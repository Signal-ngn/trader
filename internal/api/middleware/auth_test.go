package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Spot-Canvas/ledger/internal/api/middleware"
	"github.com/Spot-Canvas/ledger/internal/store"
)

// stubUserRepo is a minimal in-memory stub that satisfies the GetByAPIKey call.
// We use a function field so each test can configure the behaviour.
type stubUserRepo struct {
	fn func(ctx context.Context, apiKey uuid.UUID) (*store.AuthUser, error)
}

func (s *stubUserRepo) GetByAPIKey(ctx context.Context, apiKey uuid.UUID) (*store.AuthUser, error) {
	return s.fn(ctx, apiKey)
}

// Because middleware.NewAuthMiddleware accepts *store.UserRepository (concrete type),
// we build a thin shim: embed a real *store.UserRepository value with a nil pool,
// but override the method via a different approach.
//
// Since we cannot inject an interface without changing the middleware signature,
// the cleanest unit-test approach is to test via NewAuthMiddleware with a
// *store.UserRepository that has its pool set to nil, relying on the early-return
// for invalid/unknown keys that never hit the DB.
//
// For the "valid key" path we write an integration-tagged test instead.

func newHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.TenantIDFromContext(r.Context())
		w.Header().Set("X-Tenant-ID", id.String())
		w.WriteHeader(http.StatusOK)
	})
}

// TestAuthMiddleware_MissingHeader checks that a missing Authorization header → 401.
func TestAuthMiddleware_MissingHeader(t *testing.T) {
	mw := middleware.NewAuthMiddleware(store.NewUserRepository(nil), true, middleware.DefaultTenantID)
	handler := mw(newHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing header, got %d", rr.Code)
	}
}

// TestAuthMiddleware_NonBearerScheme checks that a non-Bearer scheme → 401.
func TestAuthMiddleware_NonBearerScheme(t *testing.T) {
	mw := middleware.NewAuthMiddleware(store.NewUserRepository(nil), true, middleware.DefaultTenantID)
	handler := mw(newHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-Bearer scheme, got %d", rr.Code)
	}
}

// TestAuthMiddleware_InvalidKeyFormat checks that a malformed UUID → 401.
func TestAuthMiddleware_InvalidKeyFormat(t *testing.T) {
	mw := middleware.NewAuthMiddleware(store.NewUserRepository(nil), true, middleware.DefaultTenantID)
	handler := mw(newHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-uuid")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid key format, got %d", rr.Code)
	}
}

// TestAuthMiddleware_EnforceAuthFalse_MissingHeader checks that ENFORCE_AUTH=false
// with a missing header falls back to the default tenant and returns 200.
func TestAuthMiddleware_EnforceAuthFalse_MissingHeader(t *testing.T) {
	defaultTenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	mw := middleware.NewAuthMiddleware(store.NewUserRepository(nil), false, defaultTenant)
	handler := mw(newHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for missing header with ENFORCE_AUTH=false, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Tenant-ID"); got != defaultTenant.String() {
		t.Errorf("expected default tenant ID %s, got %s", defaultTenant, got)
	}
}

// TestAuthMiddleware_EnforceAuthFalse_InvalidFormat checks that ENFORCE_AUTH=false
// with an invalid Bearer value falls back to the default tenant.
func TestAuthMiddleware_EnforceAuthFalse_InvalidFormat(t *testing.T) {
	defaultTenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	mw := middleware.NewAuthMiddleware(store.NewUserRepository(nil), false, defaultTenant)
	handler := mw(newHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with ENFORCE_AUTH=false, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Tenant-ID"); got != defaultTenant.String() {
		t.Errorf("expected default tenant ID %s, got %s", defaultTenant, got)
	}
}

// TestTenantIDFromContext checks that TenantIDFromContext returns uuid.Nil on empty context.
func TestTenantIDFromContext_Empty(t *testing.T) {
	id := middleware.TenantIDFromContext(context.Background())
	if id != uuid.Nil {
		t.Errorf("expected uuid.Nil for empty context, got %s", id)
	}
}
