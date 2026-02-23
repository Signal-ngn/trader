//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Spot-Canvas/ledger/internal/api"
	"github.com/Spot-Canvas/ledger/internal/ingest"
	"github.com/Spot-Canvas/ledger/internal/store"
)

// Integration test requires:
// - PostgreSQL running on DATABASE_URL
// - NATS running on NATS_URLS
//
// Run with: go test -tags=integration ./internal/api/ -v

// defaultTenantID is used in integration tests (matches the migration backfill value).
var defaultTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// newTestServer creates an API server with ENFORCE_AUTH=false (default tenant fallback).
func newTestServer(repo *store.Repository, nc *natsgo.Conn) *api.Server {
	return api.NewServer(repo, nil, nc, false, defaultTenantID)
}

func TestAPIIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}
	natsURL := os.Getenv("NATS_URLS")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	// Set up database
	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	defer repo.Close()

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	// Connect to NATS
	nc, err := natsgo.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect to nats: %v", err)
	}
	defer nc.Close()

	// Start consumer
	consumer := ingest.NewConsumer(nc, repo)
	consumerCtx, consumerCancel := context.WithCancel(ctx)
	defer consumerCancel()
	go consumer.Start(consumerCtx)
	time.Sleep(time.Second)

	// Publish test trades (must include tenant_id)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("create jetstream: %v", err)
	}

	tradeID := fmt.Sprintf("api-test-%d", time.Now().UnixNano())
	event := ingest.TradeEvent{
		TenantID:    defaultTenantID.String(),
		TradeID:     tradeID,
		AccountID:   "api-test-account",
		Symbol:      "ETH-USD",
		Side:        "buy",
		Quantity:    2.0,
		Price:       3000,
		Fee:         6,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(event)
	if _, err := js.Publish(ctx, "ledger.trades.api-test-account.spot", data); err != nil {
		t.Fatalf("publish trade: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Set up API server (ENFORCE_AUTH=false → default tenant fallback)
	srv := newTestServer(repo, nc)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Test GET /health
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("health: expected 200, got %d", resp.StatusCode)
	}

	// Test GET /api/v1/accounts (no auth header → default tenant, ENFORCE_AUTH=false)
	resp, err = http.Get(ts.URL + "/api/v1/accounts")
	if err != nil {
		t.Fatalf("accounts request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("accounts: expected 200, got %d", resp.StatusCode)
	}

	// Test GET /api/v1/accounts/{accountId}/trades
	resp, err = http.Get(ts.URL + "/api/v1/accounts/api-test-account/trades?symbol=ETH-USD")
	if err != nil {
		t.Fatalf("trades request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("trades: expected 200, got %d", resp.StatusCode)
	}

	var tradeResult store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&tradeResult)
	resp.Body.Close()

	found := false
	for _, tr := range tradeResult.Trades {
		if tr.TradeID == tradeID {
			found = true
			break
		}
	}
	if !found {
		t.Error("ingested trade not found in API response")
	}

	// Test GET /api/v1/accounts/{accountId}/positions
	resp, err = http.Get(ts.URL + "/api/v1/accounts/api-test-account/positions?status=open")
	if err != nil {
		t.Fatalf("positions request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("positions: expected 200, got %d", resp.StatusCode)
	}

	// Test GET /api/v1/accounts/{accountId}/portfolio
	resp, err = http.Get(ts.URL + "/api/v1/accounts/api-test-account/portfolio")
	if err != nil {
		t.Fatalf("portfolio request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("portfolio: expected 200, got %d", resp.StatusCode)
	}

	// Test 404 for non-existent account portfolio
	resp, err = http.Get(ts.URL + "/api/v1/accounts/nonexistent-account-xyz/portfolio")
	if err != nil {
		t.Fatalf("portfolio 404 request: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("portfolio 404: expected 404, got %d", resp.StatusCode)
	}
}

func TestImportTradesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}

	// Set up database
	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	defer repo.Close()

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	srv := newTestServer(repo, nil)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Import a batch of historic trades
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "import-test-" + suffix

	// The import handler injects tenantID from context (defaultTenantID when ENFORCE_AUTH=false)
	// so the import payload does not need tenant_id
	importBody := fmt.Sprintf(`{"trades": [
		{"trade_id":"imp-buy1-%s","account_id":"%s","symbol":"BTC-USD","side":"buy","quantity":1.0,"price":40000,"fee":20,"fee_currency":"USD","market_type":"spot","timestamp":"2024-06-01T10:00:00Z"},
		{"trade_id":"imp-buy2-%s","account_id":"%s","symbol":"BTC-USD","side":"buy","quantity":0.5,"price":42000,"fee":10.50,"fee_currency":"USD","market_type":"spot","timestamp":"2024-06-15T10:00:00Z"},
		{"trade_id":"imp-sell1-%s","account_id":"%s","symbol":"BTC-USD","side":"sell","quantity":0.5,"price":45000,"fee":11.25,"fee_currency":"USD","market_type":"spot","timestamp":"2024-07-01T10:00:00Z"}
	]}`, suffix, accountID, suffix, accountID, suffix, accountID)

	resp, err := http.Post(ts.URL+"/api/v1/import", "application/json", bytes.NewBufferString(importBody))
	if err != nil {
		t.Fatalf("import request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("import: expected 200, got %d", resp.StatusCode)
	}

	var importResp api.ImportResponse
	json.NewDecoder(resp.Body).Decode(&importResp)
	resp.Body.Close()

	if importResp.Total != 3 {
		t.Errorf("expected total 3, got %d", importResp.Total)
	}
	if importResp.Inserted != 3 {
		t.Errorf("expected inserted 3, got %d", importResp.Inserted)
	}
	if importResp.Duplicates != 0 {
		t.Errorf("expected 0 duplicates, got %d", importResp.Duplicates)
	}
	if importResp.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", importResp.Errors)
	}

	// Verify trades via GET
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/trades", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	var tradeResult store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&tradeResult)
	resp.Body.Close()

	if len(tradeResult.Trades) != 3 {
		t.Errorf("expected 3 trades, got %d", len(tradeResult.Trades))
	}

	// Verify position was built correctly: bought 1.5 BTC, sold 0.5 → 1.0 remaining
	resp, err = http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=open", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	var positions []struct {
		Symbol   string  `json:"symbol"`
		Quantity float64 `json:"quantity"`
	}
	json.NewDecoder(resp.Body).Decode(&positions)
	resp.Body.Close()

	if len(positions) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(positions))
	}
	if positions[0].Quantity != 1.0 {
		t.Errorf("expected position quantity 1.0, got %f", positions[0].Quantity)
	}

	// Re-import same trades — all should be duplicates
	resp, err = http.Post(ts.URL+"/api/v1/import", "application/json", bytes.NewBufferString(importBody))
	if err != nil {
		t.Fatalf("re-import request: %v", err)
	}

	var reimportResp api.ImportResponse
	json.NewDecoder(resp.Body).Decode(&reimportResp)
	resp.Body.Close()

	if reimportResp.Inserted != 0 {
		t.Errorf("re-import: expected 0 inserted, got %d", reimportResp.Inserted)
	}
	if reimportResp.Duplicates != 3 {
		t.Errorf("re-import: expected 3 duplicates, got %d", reimportResp.Duplicates)
	}
}

// TestAuthResolveIntegration tests the /auth/resolve endpoint.
func TestAuthResolveIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}

	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	defer repo.Close()

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	// ENFORCE_AUTH=false → /auth/resolve with no header returns default tenant
	srv := newTestServer(repo, nil)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// No auth header → default tenant (ENFORCE_AUTH=false)
	resp, err := http.Get(ts.URL + "/auth/resolve")
	if err != nil {
		t.Fatalf("auth/resolve request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("auth/resolve: expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	if body["tenant_id"] != defaultTenantID.String() {
		t.Errorf("expected tenant_id %s, got %s", defaultTenantID, body["tenant_id"])
	}
}

// TestAuthResolveUnauthorized tests that /auth/resolve returns 401 when ENFORCE_AUTH=true.
func TestAuthResolveUnauthorized(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}

	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	defer repo.Close()

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	// ENFORCE_AUTH=true, userRepo backed by real pool (users table must exist)
	userRepo := store.NewUserRepository(repo.Pool())
	srv := api.NewServer(repo, userRepo, nil, true, defaultTenantID)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// No auth header → 401
	resp, err := http.Get(ts.URL + "/auth/resolve")
	if err != nil {
		t.Fatalf("auth/resolve request: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("auth/resolve with no key: expected 401, got %d", resp.StatusCode)
	}

	// Unknown API key → 401
	req, _ := http.NewRequest("GET", ts.URL+"/auth/resolve", nil)
	req.Header.Set("Authorization", "Bearer "+uuid.New().String())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auth/resolve with unknown key: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("auth/resolve with unknown key: expected 401, got %d", resp.StatusCode)
	}
}

// TestTenantIsolationIntegration verifies that two tenants cannot see each other's data.
func TestTenantIsolationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}

	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	defer repo.Close()

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "iso-live-" + suffix
	tenant1 := uuid.MustParse("11111111-0000-0000-0000-000000000001")
	tenant2 := uuid.MustParse("22222222-0000-0000-0000-000000000002")

	// Use one server per tenant by setting the default tenant via ENFORCE_AUTH=false
	srv1 := api.NewServer(repo, nil, nil, false, tenant1)
	ts1 := httptest.NewServer(srv1.Router())
	defer ts1.Close()

	srv2 := api.NewServer(repo, nil, nil, false, tenant2)
	ts2 := httptest.NewServer(srv2.Router())
	defer ts2.Close()

	// Import different trades for each tenant (same account ID, different symbols)
	body1 := fmt.Sprintf(`{"trades":[{"trade_id":"iso-t1-%s","account_id":"%s","symbol":"BTC-USD","side":"buy","quantity":1,"price":50000,"fee":0,"fee_currency":"USD","market_type":"spot","timestamp":"2025-01-01T00:00:00Z"}]}`, suffix, accountID)
	body2 := fmt.Sprintf(`{"trades":[{"trade_id":"iso-t2-%s","account_id":"%s","symbol":"ETH-USD","side":"buy","quantity":2,"price":3000,"fee":0,"fee_currency":"USD","market_type":"spot","timestamp":"2025-01-01T00:00:00Z"}]}`, suffix, accountID)

	http.Post(ts1.URL+"/api/v1/import", "application/json", bytes.NewBufferString(body1))
	http.Post(ts2.URL+"/api/v1/import", "application/json", bytes.NewBufferString(body2))

	_ = ctx // used above

	// Tenant 1 sees only BTC-USD trade
	resp, err := http.Get(ts1.URL + "/api/v1/accounts/" + accountID + "/trades")
	if err != nil {
		t.Fatalf("t1 trades: %v", err)
	}
	var r1 store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&r1)
	resp.Body.Close()

	for _, tr := range r1.Trades {
		if tr.Symbol == "ETH-USD" {
			t.Errorf("tenant1 should not see tenant2's ETH-USD trade")
		}
	}

	// Tenant 2 sees only ETH-USD trade
	resp, err = http.Get(ts2.URL + "/api/v1/accounts/" + accountID + "/trades")
	if err != nil {
		t.Fatalf("t2 trades: %v", err)
	}
	var r2 store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&r2)
	resp.Body.Close()

	for _, tr := range r2.Trades {
		if tr.Symbol == "BTC-USD" {
			t.Errorf("tenant2 should not see tenant1's BTC-USD trade")
		}
	}
}
