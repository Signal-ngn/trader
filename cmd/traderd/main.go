package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/api"
	"github.com/Signal-ngn/trader/internal/api/middleware"
	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/engine"
	"github.com/Signal-ngn/trader/internal/ingest"
	"github.com/Signal-ngn/trader/internal/store"
)

func main() {
	// Configure zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Set log level
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	log.Info().
		Str("port", cfg.HTTPPort).
		Str("environment", cfg.Environment).
		Bool("enforce_auth", cfg.EnforceAuth).
		Msg("starting trader service")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize database
	repo, err := store.NewRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer repo.Close()

	if err := repo.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ping database")
	}
	log.Info().Msg("connected to PostgreSQL")

	// Run migrations
	migrated, err := store.RunMigrationsWithReport(ctx, repo.Pool())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}
	log.Info().Msg("migrations complete")

	// If a positions-rebuild migration was just applied, replay all trades
	// through the updated P&L logic.
	rebuildMigrations := map[string]bool{
		"004_rebuild_positions_margin_pnl":          true,
		"005_rebuild_positions_margin_scale":         true,
		"006_rebuild_positions_default_leverage":     true,
	}
	for _, v := range migrated {
		if rebuildMigrations[v] {
			log.Info().Str("migration", v).Msg("rebuild migration applied — rebuilding all positions")
			if err := repo.RebuildAllPositions(ctx); err != nil {
				log.Fatal().Err(err).Msg("failed to rebuild positions")
			}
			log.Info().Msg("position rebuild complete")
			break
		}
	}

	// Initialise UserRepository (shares the same pool)
	userRepo := store.NewUserRepository(repo.Pool())

	// Start HTTP server immediately so Cloud Run health checks pass while NATS
	// is still connecting. The server runs without NATS initially; the consumer
	// is wired in once NATS connects (below).
	defaultTenantID := uuid.MustParse(middleware.DefaultTenantID.String())
	srv := api.NewServer(repo, userRepo, nil, cfg.EnforceAuth, defaultTenantID)
	httpServer := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: srv.Router(),
	}

	go func() {
		log.Info().Str("port", cfg.HTTPPort).Msg("starting HTTP server")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	// Start trading engine when enabled — runs after DB is ready.
	if cfg.TradingEnabled {
		eng := engine.New(cfg, repo)
		go func() {
			if err := eng.Start(ctx); err != nil {
				log.Error().Err(err).Msg("trading engine error")
			}
		}()
		log.Info().Str("account", cfg.TraderAccount).Str("mode", cfg.TradingMode).Msg("trading engine starting")
	}

	// Connect to NATS in the background so startup doesn't block.
	// The consumer starts automatically once connected.
	go func() {
		nc, err := ingest.ConnectNATS(cfg.NATSURLs, cfg.NATSCredsFile, cfg.NATSCreds)
		if err != nil {
			// ConnectNATS retries forever; this path is unreachable in practice.
			log.Error().Err(err).Msg("failed to connect to NATS")
			return
		}
		defer nc.Close()
		log.Info().Str("url", nc.ConnectedUrl()).Msg("connected to NATS")

		consumer := ingest.NewConsumer(nc, repo)
		consumer.WithPublisher(srv.StreamRegistry())
		if err := consumer.Start(ctx); err != nil {
			log.Error().Err(err).Msg("NATS consumer error")
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Info().Msg("shutting down...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	log.Info().Msg("shutdown complete")
}
