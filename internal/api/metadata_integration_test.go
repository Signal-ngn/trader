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

	"github.com/Spot-Canvas/ledger/internal/api"
	"github.com/Spot-Canvas/ledger/internal/domain"
	"github.com/Spot-Canvas/ledger/internal/store"
)

// Integration tests for trade metadata fields.
// Requires PostgreSQL running on DATABASE_URL.
//
// Run with: go test -tags=integration ./internal/api/ -v -run TestMetadata

func setupMetadataTest(t *testing.T) (*store.Repository, *httptest.Server, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable"
	}

	repo, err := store.NewRepository(ctx, dbURL)
	if err != nil {
		cancel()
		t.Fatalf("connect to db: %v", err)
	}

	if err := store.RunMigrations(ctx, repo.Pool()); err != nil {
		repo.Close()
		cancel()
		t.Fatalf("run migrations: %v", err)
	}

	// ENFORCE_AUTH=false → default tenant used for all requests
	srv := api.NewServer(repo, nil, nil, false, defaultTenantID)
	ts := httptest.NewServer(srv.Router())

	cleanup := func() {
		ts.Close()
		repo.Close()
		cancel()
	}
	return repo, ts, cleanup
}

func importTrades(t *testing.T, ts *httptest.Server, body string) api.ImportResponse {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/v1/import", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("import request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import: expected 200, got %d", resp.StatusCode)
	}
	var importResp api.ImportResponse
	json.NewDecoder(resp.Body).Decode(&importResp)
	return importResp
}

func TestMetadata_TradeWithFullMetadata(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-full-" + suffix

	body := fmt.Sprintf(`{"trades": [{
		"trade_id": "meta-buy-%s",
		"account_id": "%s",
		"symbol": "BTC-USD",
		"side": "buy",
		"quantity": 1.0,
		"price": 50000,
		"fee": 25,
		"fee_currency": "USD",
		"market_type": "spot",
		"timestamp": "2025-01-15T10:00:00Z",
		"strategy": "macd-rsi-v2",
		"entry_reason": "MACD bullish crossover, RSI 42",
		"confidence": 0.85,
		"stop_loss": 48000,
		"take_profit": 55000
	}]}`, suffix, accountID)

	importResp := importTrades(t, ts, body)
	if importResp.Inserted != 1 {
		t.Fatalf("expected 1 inserted, got %d", importResp.Inserted)
	}

	// Verify trade has metadata
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/trades", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	defer resp.Body.Close()

	var result store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}
	tr := result.Trades[0]

	if tr.Strategy == nil || *tr.Strategy != "macd-rsi-v2" {
		t.Errorf("expected strategy macd-rsi-v2, got %v", tr.Strategy)
	}
	if tr.EntryReason == nil || *tr.EntryReason != "MACD bullish crossover, RSI 42" {
		t.Errorf("expected entry_reason, got %v", tr.EntryReason)
	}
	if tr.Confidence == nil || *tr.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %v", tr.Confidence)
	}
	if tr.StopLoss == nil || *tr.StopLoss != 48000 {
		t.Errorf("expected stop_loss 48000, got %v", tr.StopLoss)
	}
	if tr.TakeProfit == nil || *tr.TakeProfit != 55000 {
		t.Errorf("expected take_profit 55000, got %v", tr.TakeProfit)
	}
}

func TestMetadata_TradeWithoutMetadata(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-none-" + suffix

	body := fmt.Sprintf(`{"trades": [{
		"trade_id": "nometa-buy-%s",
		"account_id": "%s",
		"symbol": "ETH-USD",
		"side": "buy",
		"quantity": 2.0,
		"price": 3000,
		"fee": 6,
		"fee_currency": "USD",
		"market_type": "spot",
		"timestamp": "2025-01-15T10:00:00Z"
	}]}`, suffix, accountID)

	importResp := importTrades(t, ts, body)
	if importResp.Inserted != 1 {
		t.Fatalf("expected 1 inserted, got %d", importResp.Inserted)
	}

	// Verify trade has NULL metadata
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/trades", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	defer resp.Body.Close()

	var result store.TradeListResult
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}
	tr := result.Trades[0]

	if tr.Strategy != nil {
		t.Errorf("expected nil strategy, got %v", tr.Strategy)
	}
	if tr.EntryReason != nil {
		t.Errorf("expected nil entry_reason, got %v", tr.EntryReason)
	}
	if tr.Confidence != nil {
		t.Errorf("expected nil confidence, got %v", tr.Confidence)
	}
	if tr.StopLoss != nil {
		t.Errorf("expected nil stop_loss, got %v", tr.StopLoss)
	}
	if tr.TakeProfit != nil {
		t.Errorf("expected nil take_profit, got %v", tr.TakeProfit)
	}
}

func TestMetadata_PositionOpensWithMetadata(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-pos-" + suffix

	body := fmt.Sprintf(`{"trades": [{
		"trade_id": "pos-buy-%s",
		"account_id": "%s",
		"symbol": "BTC-USD",
		"side": "buy",
		"quantity": 1.0,
		"price": 50000,
		"fee": 25,
		"fee_currency": "USD",
		"market_type": "spot",
		"timestamp": "2025-01-15T10:00:00Z",
		"strategy": "trend-follow",
		"confidence": 0.9,
		"stop_loss": 47000,
		"take_profit": 56000
	}]}`, suffix, accountID)

	importTrades(t, ts, body)

	// Verify position has metadata
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=open", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	defer resp.Body.Close()

	var positions []domain.Position
	json.NewDecoder(resp.Body).Decode(&positions)

	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	pos := positions[0]

	if pos.Confidence == nil || *pos.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %v", pos.Confidence)
	}
	if pos.StopLoss == nil || *pos.StopLoss != 47000 {
		t.Errorf("expected stop_loss 47000, got %v", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 56000 {
		t.Errorf("expected take_profit 56000, got %v", pos.TakeProfit)
	}
	if pos.ExitPrice != nil {
		t.Errorf("expected nil exit_price on open position, got %v", pos.ExitPrice)
	}
	if pos.ExitReason != nil {
		t.Errorf("expected nil exit_reason on open position, got %v", pos.ExitReason)
	}
}

func TestMetadata_PositionClosesSetsExitFields(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-close-" + suffix

	body := fmt.Sprintf(`{"trades": [
		{
			"trade_id": "close-buy-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "buy",
			"quantity": 1.0,
			"price": 50000,
			"fee": 25,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-15T10:00:00Z",
			"strategy": "trend-follow",
			"confidence": 0.85,
			"stop_loss": 47000,
			"take_profit": 56000
		},
		{
			"trade_id": "close-sell-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "sell",
			"quantity": 1.0,
			"price": 55000,
			"fee": 27.50,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-20T10:00:00Z",
			"exit_reason": "take profit reached"
		}
	]}`, suffix, accountID, suffix, accountID)

	importTrades(t, ts, body)

	// Verify closed position has exit fields
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=closed", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	defer resp.Body.Close()

	var positions []domain.Position
	json.NewDecoder(resp.Body).Decode(&positions)

	if len(positions) != 1 {
		t.Fatalf("expected 1 closed position, got %d", len(positions))
	}
	pos := positions[0]

	if pos.ExitPrice == nil || *pos.ExitPrice != 55000 {
		t.Errorf("expected exit_price 55000, got %v", pos.ExitPrice)
	}
	if pos.ExitReason == nil || *pos.ExitReason != "take profit reached" {
		t.Errorf("expected exit_reason 'take profit reached', got %v", pos.ExitReason)
	}
	if pos.Confidence == nil || *pos.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85 from entry, got %v", pos.Confidence)
	}
	if pos.StopLoss == nil || *pos.StopLoss != 47000 {
		t.Errorf("expected stop_loss 47000 from entry, got %v", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 56000 {
		t.Errorf("expected take_profit 56000 from entry, got %v", pos.TakeProfit)
	}
}

func TestMetadata_IncreasePositionUpdatesSLTP(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-incr-" + suffix

	body := fmt.Sprintf(`{"trades": [
		{
			"trade_id": "incr-buy1-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "buy",
			"quantity": 1.0,
			"price": 50000,
			"fee": 25,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-15T10:00:00Z",
			"confidence": 0.85,
			"stop_loss": 47000,
			"take_profit": 56000
		},
		{
			"trade_id": "incr-buy2-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "buy",
			"quantity": 0.5,
			"price": 51000,
			"fee": 12.75,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-16T10:00:00Z",
			"stop_loss": 48000
		}
	]}`, suffix, accountID, suffix, accountID)

	importTrades(t, ts, body)

	// Verify position: SL updated to 48000, TP unchanged at 56000, confidence unchanged at 0.85
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=open", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	defer resp.Body.Close()

	var positions []domain.Position
	json.NewDecoder(resp.Body).Decode(&positions)

	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	pos := positions[0]

	if pos.StopLoss == nil || *pos.StopLoss != 48000 {
		t.Errorf("expected stop_loss updated to 48000, got %v", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 56000 {
		t.Errorf("expected take_profit unchanged at 56000, got %v", pos.TakeProfit)
	}
	if pos.Confidence == nil || *pos.Confidence != 0.85 {
		t.Errorf("expected confidence unchanged at 0.85, got %v", pos.Confidence)
	}
}

func TestMetadata_RebuildPreservesMetadata(t *testing.T) {
	repo, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-rebuild-" + suffix

	body := fmt.Sprintf(`{"trades": [
		{
			"trade_id": "rb-buy-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "buy",
			"quantity": 1.0,
			"price": 50000,
			"fee": 25,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-15T10:00:00Z",
			"strategy": "trend-follow",
			"confidence": 0.85,
			"stop_loss": 47000,
			"take_profit": 56000
		},
		{
			"trade_id": "rb-sell-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "sell",
			"quantity": 1.0,
			"price": 55000,
			"fee": 27.50,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-20T10:00:00Z",
			"exit_reason": "stop loss hit"
		}
	]}`, suffix, accountID, suffix, accountID)

	importTrades(t, ts, body)

	// Rebuild positions
	ctx := context.Background()
	if err := repo.RebuildPositions(ctx, defaultTenantID, accountID); err != nil {
		t.Fatalf("rebuild positions: %v", err)
	}

	// Verify metadata preserved after rebuild
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=closed", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	defer resp.Body.Close()

	var positions []domain.Position
	json.NewDecoder(resp.Body).Decode(&positions)

	if len(positions) != 1 {
		t.Fatalf("expected 1 closed position after rebuild, got %d", len(positions))
	}
	pos := positions[0]

	if pos.ExitPrice == nil || *pos.ExitPrice != 55000 {
		t.Errorf("expected exit_price 55000 after rebuild, got %v", pos.ExitPrice)
	}
	if pos.ExitReason == nil || *pos.ExitReason != "stop loss hit" {
		t.Errorf("expected exit_reason 'stop loss hit' after rebuild, got %v", pos.ExitReason)
	}
	if pos.Confidence == nil || *pos.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85 after rebuild, got %v", pos.Confidence)
	}
	if pos.StopLoss == nil || *pos.StopLoss != 47000 {
		t.Errorf("expected stop_loss 47000 after rebuild, got %v", pos.StopLoss)
	}
	if pos.TakeProfit == nil || *pos.TakeProfit != 56000 {
		t.Errorf("expected take_profit 56000 after rebuild, got %v", pos.TakeProfit)
	}
}

func TestMetadata_APITradesOmitsNullFields(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-api-tr-" + suffix

	// Import one trade with metadata and one without
	body := fmt.Sprintf(`{"trades": [
		{
			"trade_id": "api-withmeta-%s",
			"account_id": "%s",
			"symbol": "BTC-USD",
			"side": "buy",
			"quantity": 1.0,
			"price": 50000,
			"fee": 25,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-15T10:00:00Z",
			"strategy": "test-strat",
			"confidence": 0.75
		},
		{
			"trade_id": "api-nometa-%s",
			"account_id": "%s",
			"symbol": "ETH-USD",
			"side": "buy",
			"quantity": 5.0,
			"price": 3000,
			"fee": 15,
			"fee_currency": "USD",
			"market_type": "spot",
			"timestamp": "2025-01-15T11:00:00Z"
		}
	]}`, suffix, accountID, suffix, accountID)

	importTrades(t, ts, body)

	// Get raw JSON to check omitempty behavior
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/trades", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	defer resp.Body.Close()

	var raw []map[string]interface{}
	// The response has a wrapper with "trades" and "next_cursor"
	var wrapper map[string]json.RawMessage
	json.NewDecoder(resp.Body).Decode(&wrapper)
	json.Unmarshal(wrapper["trades"], &raw)

	for _, tr := range raw {
		tradeID, _ := tr["trade_id"].(string)
		if tradeID == fmt.Sprintf("api-withmeta-%s", suffix) {
			if _, ok := tr["strategy"]; !ok {
				t.Error("trade with metadata should have 'strategy' field")
			}
			if _, ok := tr["confidence"]; !ok {
				t.Error("trade with metadata should have 'confidence' field")
			}
		}
		if tradeID == fmt.Sprintf("api-nometa-%s", suffix) {
			if _, ok := tr["strategy"]; ok {
				t.Error("trade without metadata should NOT have 'strategy' field")
			}
			if _, ok := tr["confidence"]; ok {
				t.Error("trade without metadata should NOT have 'confidence' field")
			}
			if _, ok := tr["stop_loss"]; ok {
				t.Error("trade without metadata should NOT have 'stop_loss' field")
			}
		}
	}
}

func TestMetadata_APIPositionsOmitsNullFields(t *testing.T) {
	_, ts, cleanup := setupMetadataTest(t)
	defer cleanup()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	accountID := "meta-api-pos-" + suffix

	// Create open position with metadata
	body := fmt.Sprintf(`{"trades": [{
		"trade_id": "apipos-buy-%s",
		"account_id": "%s",
		"symbol": "BTC-USD",
		"side": "buy",
		"quantity": 1.0,
		"price": 50000,
		"fee": 25,
		"fee_currency": "USD",
		"market_type": "spot",
		"timestamp": "2025-01-15T10:00:00Z",
		"confidence": 0.9,
		"stop_loss": 47000,
		"take_profit": 56000
	}]}`, suffix, accountID)

	importTrades(t, ts, body)

	// Get raw JSON to check omitempty
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/accounts/%s/positions?status=open", ts.URL, accountID))
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	defer resp.Body.Close()

	var raw []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&raw)

	if len(raw) != 1 {
		t.Fatalf("expected 1 position, got %d", len(raw))
	}
	pos := raw[0]

	// Should have stop_loss, take_profit, confidence
	if _, ok := pos["stop_loss"]; !ok {
		t.Error("open position should have 'stop_loss' field")
	}
	if _, ok := pos["take_profit"]; !ok {
		t.Error("open position should have 'take_profit' field")
	}
	if _, ok := pos["confidence"]; !ok {
		t.Error("open position should have 'confidence' field")
	}

	// Should NOT have exit_price, exit_reason (position is open)
	if _, ok := pos["exit_price"]; ok {
		t.Error("open position should NOT have 'exit_price' field")
	}
	if _, ok := pos["exit_reason"]; ok {
		t.Error("open position should NOT have 'exit_reason' field")
	}
}
