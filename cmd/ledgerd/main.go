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

	"github.com/Spot-Canvas/ledger/internal/api"
	"github.com/Spot-Canvas/ledger/internal/api/middleware"
	"github.com/Spot-Canvas/ledger/internal/config"
	"github.com/Spot-Canvas/ledger/internal/ingest"
	"github.com/Spot-Canvas/ledger/internal/store"
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
		Msg("starting ledger service")

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
	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}
	log.Info().Msg("migrations complete")

	// Initialise UserRepository (shares the same pool — same DB as spot-canvas-app)
	userRepo := store.NewUserRepository(repo.Pool())

	// Connect to NATS
	nc, err := ingest.ConnectNATS(cfg.NATSURLs, cfg.NATSCredsFile, cfg.NATSCreds)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to NATS")
	}
	defer nc.Close()
	log.Info().Str("url", nc.ConnectedUrl()).Msg("connected to NATS")

	// Start NATS consumer
	consumer := ingest.NewConsumer(nc, repo)
	go func() {
		if err := consumer.Start(ctx); err != nil {
			log.Error().Err(err).Msg("NATS consumer error")
		}
	}()

	// Start HTTP server
	defaultTenantID := uuid.MustParse(middleware.DefaultTenantID.String())
	srv := api.NewServer(repo, userRepo, nc, cfg.EnforceAuth, defaultTenantID)
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
