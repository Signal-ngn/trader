package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/api"
	"github.com/Signal-ngn/trader/internal/api/middleware"
	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/engine"
	"github.com/Signal-ngn/trader/internal/platform"
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

	// Start HTTP server immediately so Cloud Run health checks pass.
	defaultTenantID := uuid.MustParse(middleware.DefaultTenantID.String())
	srv := api.NewServer(cfg.EnforceAuth, defaultTenantID)
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

	// Start trading engine when enabled — uses platform API + Firestore, no DB.
	if cfg.TradingEnabled {
		if cfg.SNAPIKey == "" {
			log.Fatal().Msg("SN_API_KEY is required when TRADING_ENABLED=true")
		}
		if cfg.FirestoreProjectID == "" {
			log.Fatal().Msg("FIRESTORE_PROJECT_ID is required when TRADING_ENABLED=true")
		}

		// Construct platform client.
		platformClient := platform.New(cfg.TraderAPIURL, cfg.SNAPIKey)

		// Construct Firestore client using Application Default Credentials (ADC).
		firestoreClient, err := firestore.NewClient(ctx, cfg.FirestoreProjectID)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create Firestore client")
		}
		defer firestoreClient.Close()

		// Construct the API-backed engine store.
		apiStore := engine.NewAPIEngineStore(platformClient, firestoreClient, cfg)

		eng := engine.New(cfg, apiStore, srv.StreamRegistry())
		go func() {
			if err := eng.Start(ctx); err != nil {
				log.Error().Err(err).Msg("trading engine error")
			}
		}()
		log.Info().Strs("accounts", cfg.TraderAccounts).Str("mode", cfg.TradingMode).Msg("trading engine starting")
	}

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
