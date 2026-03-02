package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/trader/internal/api/middleware"
	"github.com/Signal-ngn/trader/internal/store"
)

// Server holds the HTTP server dependencies.
type Server struct {
	repo           *store.Repository
	userRepo       *store.UserRepository
	nc             *nats.Conn
	enforceAuth    bool
	defaultTID     uuid.UUID
	streamRegistry *StreamRegistry
}

// NewServer creates a new API server.
func NewServer(repo *store.Repository, userRepo *store.UserRepository, nc *nats.Conn, enforceAuth bool, defaultTenantID uuid.UUID) *Server {
	return &Server{
		repo:           repo,
		userRepo:       userRepo,
		nc:             nc,
		enforceAuth:    enforceAuth,
		defaultTID:     defaultTenantID,
		streamRegistry: NewStreamRegistry(),
	}
}

// StreamRegistry returns the server's trade stream registry.
func (s *Server) StreamRegistry() *StreamRegistry {
	return s.streamRegistry
}

// Router returns the configured chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(requestLogger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type", "Authorization"},
		MaxAge:         300,
	}))

	// Health check — no auth required
	r.Get("/health", s.handleHealth)

	// Auth resolve endpoint — protected by AuthMiddleware
	authMW := middleware.NewAuthMiddleware(s.userRepo, s.enforceAuth, s.defaultTID)
	r.With(authMW).Get("/auth/resolve", s.handleAuthResolve)

	// API v1 routes — all protected by AuthMiddleware
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authMW)

		// Import endpoint (POST)
		r.Post("/import", s.handleImportTrades)

		// Trade deletion endpoint (DELETE)
		r.Delete("/trades/{tradeId}", s.handleDeleteTrade)

		// Read-only query endpoints (GET)
		r.Get("/accounts", s.handleListAccounts)
		r.Get("/accounts/{accountId}/stats", s.handleAccountStats)
		r.Get("/accounts/{accountId}/portfolio", s.handlePortfolioSummary)
		r.Get("/accounts/{accountId}/positions", s.handleListPositions)
		r.Get("/accounts/{accountId}/trades", s.handleListTrades)
		r.Get("/accounts/{accountId}/trades/stream", s.handleTradeStream)
		r.Get("/accounts/{accountId}/orders", s.handleListOrders)
		r.Put("/accounts/{accountId}/balance", s.handleSetBalance)
		r.Get("/accounts/{accountId}/balance", s.handleGetBalance)
	})

	return r
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Dur("duration", time.Since(start)).
			Msg("request")
	})
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"error": "Method Not Allowed",
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
