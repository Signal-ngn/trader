package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/Spot-Canvas/ledger/internal/store"
)

// tenantIDKey is the typed context key for the tenant ID.
type tenantIDKey struct{}

// DefaultTenantID is the fallback tenant used when ENFORCE_AUTH=false.
var DefaultTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// TenantIDFromContext retrieves the tenant ID stored in the context by AuthMiddleware.
// Returns uuid.Nil if not present.
func TenantIDFromContext(ctx context.Context) uuid.UUID {
	if id, ok := ctx.Value(tenantIDKey{}).(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

// NewAuthMiddleware returns an HTTP middleware that authenticates Bearer API keys.
//
//   - Parses "Authorization: Bearer <uuid>" from the request header.
//   - Resolves the UUID to a tenant ID via userRepo.GetByAPIKey.
//   - Stores the tenant ID in the request context.
//   - When enforceAuth=true: returns 401 on missing/invalid/unknown keys.
//   - When enforceAuth=false: logs a warning and falls back to defaultTenantID.
func NewAuthMiddleware(userRepo *store.UserRepository, enforceAuth bool, defaultTenantID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			tenantID, ok := resolveAuth(w, r, authHeader, userRepo, enforceAuth, defaultTenantID)
			if !ok {
				return
			}

			ctx := context.WithValue(r.Context(), tenantIDKey{}, tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveAuth parses and validates the Bearer token, returning the resolved tenant ID.
// Returns (uuid.Nil, false) and writes a 401 if auth fails (when enforceAuth=true).
func resolveAuth(w http.ResponseWriter, r *http.Request, authHeader string, userRepo *store.UserRepository, enforceAuth bool, defaultTenantID uuid.UUID) (uuid.UUID, bool) {
	if authHeader == "" {
		return fallbackOrReject(w, enforceAuth, defaultTenantID, "missing Authorization header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return fallbackOrReject(w, enforceAuth, defaultTenantID, "Authorization header must use Bearer scheme")
	}

	rawKey := strings.TrimPrefix(authHeader, "Bearer ")
	apiKey, err := uuid.Parse(rawKey)
	if err != nil || apiKey == uuid.Nil {
		return fallbackOrReject(w, enforceAuth, defaultTenantID, "invalid API key format")
	}

	user, err := userRepo.GetByAPIKey(r.Context(), apiKey)
	if err != nil {
		log.Error().Err(err).Msg("auth: database error resolving API key")
		if enforceAuth {
			writeUnauthorized(w, "authentication service unavailable")
			return uuid.Nil, false
		}
		log.Warn().Msg("auth: DB error resolving key, falling back to default tenant (ENFORCE_AUTH=false)")
		return defaultTenantID, true
	}

	if user == nil {
		return fallbackOrReject(w, enforceAuth, defaultTenantID, "unknown API key")
	}

	return user.TenantID, true
}

// fallbackOrReject returns the default tenant when enforceAuth=false, or writes 401.
func fallbackOrReject(w http.ResponseWriter, enforceAuth bool, defaultTenantID uuid.UUID, reason string) (uuid.UUID, bool) {
	if enforceAuth {
		writeUnauthorized(w, reason)
		return uuid.Nil, false
	}
	log.Warn().Str("reason", reason).Msg("auth: falling back to default tenant (ENFORCE_AUTH=false)")
	return defaultTenantID, true
}

// writeUnauthorized sends a JSON 401 response.
func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
