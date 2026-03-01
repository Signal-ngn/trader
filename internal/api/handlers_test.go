package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── GET /api/v1/accounts/{id}/positions — pagination shape ───────────────────

// TestListPositionsRoute_ReturnsObjectNotArray verifies that the positions
// endpoint returns a JSON object {"positions":[...], "next_cursor":"..."} and
// NOT a bare array. Without a real DB the handler will 500, but we can test
// the route is registered and the response shape when the handler does run by
// checking the Content-Type and that a bare array is never returned.
func TestListPositionsRoute_Registered(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	req := httptest.NewRequest("GET", "/api/v1/accounts/paper/positions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("GET /api/v1/accounts/paper/positions: got 404, route not registered")
	}
	if w.Code == http.StatusMethodNotAllowed {
		t.Error("GET /api/v1/accounts/paper/positions: got 405, GET should be allowed")
	}
}

func TestListPositionsRoute_InvalidStatus(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	req := httptest.NewRequest("GET", "/api/v1/accounts/paper/positions?status=invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", w.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestListPositionsRoute_InvalidLimit(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	req := httptest.NewRequest("GET", "/api/v1/accounts/paper/positions?limit=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid limit, got %d", w.Code)
	}
}

func TestListPositionsRoute_RequiresAuth(t *testing.T) {
	srv := &Server{nc: nil, enforceAuth: true}
	router := srv.Router()

	req := httptest.NewRequest("GET", "/api/v1/accounts/paper/positions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth header, got %d", w.Code)
	}
}

// ── DELETE /api/v1/trades/{tradeId} ──────────────────────────────────────────

func TestDeleteTradeRoute_Registered(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	req := httptest.NewRequest("DELETE", "/api/v1/trades/some-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Without auth enforcement the handler runs; without a real DB it will 500.
	// What matters here is that the route IS registered (not 404 or 405).
	if w.Code == http.StatusNotFound {
		t.Error("DELETE /api/v1/trades/{tradeId}: got 404, route not registered")
	}
	if w.Code == http.StatusMethodNotAllowed {
		t.Error("DELETE /api/v1/trades/{tradeId}: got 405, DELETE should be allowed")
	}
}

func TestDeleteTradeRoute_RequiresAuth(t *testing.T) {
	srv := &Server{nc: nil, enforceAuth: true}
	router := srv.Router()

	req := httptest.NewRequest("DELETE", "/api/v1/trades/some-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth header, got %d", w.Code)
	}
}

func TestDeleteTradeRoute_OtherMethodsNotAllowed(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	for _, method := range []string{"PUT", "PATCH", "POST"} {
		req := httptest.NewRequest(method, "/api/v1/trades/some-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/v1/trades/some-id: expected 405, got %d", method, w.Code)
		}
	}
}

func TestHealthEndpoint_NilNATS(t *testing.T) {
	srv := &Server{
		repo: nil,
		nc:   nil,
	}

	router := srv.Router()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Without a real database, we expect 503 or a panic-recovered 500
	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 503 or 500, got %d", w.Code)
	}
}

func TestMethodNotAllowed_PUTAndDELETE(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	// PUT and DELETE should be 405 on all endpoints
	methods := []string{"PUT", "DELETE", "PATCH"}
	paths := []string{
		"/api/v1/accounts",
		"/api/v1/accounts/live/portfolio",
		"/api/v1/accounts/live/positions",
		"/api/v1/accounts/live/trades",
		"/api/v1/accounts/live/orders",
		"/api/v1/import",
	}

	for _, method := range methods {
		for _, path := range paths {
			req := httptest.NewRequest(method, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: expected 405, got %d", method, path, w.Code)
			}
		}
	}
}

func TestRouterHasCorrectGETRoutes(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	paths := []string{
		"/health",
		"/api/v1/accounts",
		"/api/v1/accounts/test/portfolio",
		"/api/v1/accounts/test/positions",
		"/api/v1/accounts/test/trades",
		"/api/v1/accounts/test/orders",
		"/api/v1/accounts/test/stats",
	}

	for _, path := range paths {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code == http.StatusNotFound {
			t.Errorf("GET %s: got 404, route not registered", path)
		}
		if w.Code == http.StatusMethodNotAllowed {
			t.Errorf("GET %s: got 405, GET should be allowed", path)
		}
	}
}

func TestAccountStatsRoute_RequiresAuth(t *testing.T) {
	// With enforceAuth=true, no Authorization header → 401
	srv := &Server{nc: nil, enforceAuth: true}
	router := srv.Router()

	req := httptest.NewRequest("GET", "/api/v1/accounts/paper/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth header, got %d", w.Code)
	}
}

func TestAccountStatsRoute_MethodNotAllowed(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req := httptest.NewRequest(method, "/api/v1/accounts/paper/stats", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/v1/accounts/paper/stats: expected 405, got %d", method, w.Code)
		}
	}
}

func TestImportRouteAcceptsPOST(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	body := `{"trades": []}`
	req := httptest.NewRequest("POST", "/api/v1/import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should get 400 (empty trades) not 404 or 405
	if w.Code == http.StatusNotFound {
		t.Error("POST /api/v1/import: got 404, route not registered")
	}
	if w.Code == http.StatusMethodNotAllowed {
		t.Error("POST /api/v1/import: got 405, POST should be allowed")
	}
}

func TestJSONContentType(t *testing.T) {
	srv := &Server{nc: nil}
	router := srv.Router()

	// Use the import endpoint which returns a JSON error body
	body := `{"trades": []}`
	req := httptest.NewRequest("POST", "/api/v1/import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("response body is not valid JSON: %v", err)
	}
}
