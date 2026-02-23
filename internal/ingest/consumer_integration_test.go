//go:build integration

package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Spot-Canvas/ledger/internal/ingest"
	"github.com/Spot-Canvas/ledger/internal/store"
)

// Integration test requires:
// - PostgreSQL running on DATABASE_URL (default: postgres://spot:spot@localhost:5432/spot_canvas?sslmode=disable)
// - NATS running on NATS_URLS (default: nats://localhost:4222)
//
// Run with: go test -tags=integration ./internal/ingest/ -v

// defaultTestTenantID is the tenant used for integration tests.
var defaultTestTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func TestIngestionFlow(t *testing.T) {
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
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("connect to nats: %v", err)
	}
	defer nc.Close()

	// Start consumer
	consumer := ingest.NewConsumer(nc, repo)
	consumerCtx, consumerCancel := context.WithCancel(ctx)
	defer consumerCancel()

	go func() {
		consumer.Start(consumerCtx)
	}()

	// Wait a moment for consumer to be ready
	time.Sleep(time.Second)

	// Publish a trade event (must include tenant_id)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("create jetstream: %v", err)
	}

	event := ingest.TradeEvent{
		TenantID:    defaultTestTenantID.String(),
		TradeID:     "integration-test-" + time.Now().Format("20060102150405"),
		AccountID:   "test-account",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.1,
		Price:       50000,
		Fee:         5,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	_, err = js.Publish(ctx, "ledger.trades.test-account.spot", data)
	if err != nil {
		t.Fatalf("publish trade: %v", err)
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Verify trade in DB (scoped to default test tenant)
	result, err := repo.ListTrades(ctx, defaultTestTenantID, "test-account", store.TradeFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}

	found := false
	for _, trade := range result.Trades {
		if trade.TradeID == event.TradeID {
			found = true
			if trade.Symbol != "BTC-USD" {
				t.Errorf("expected symbol BTC-USD, got %s", trade.Symbol)
			}
			if trade.Quantity != 0.1 {
				t.Errorf("expected quantity 0.1, got %f", trade.Quantity)
			}
			break
		}
	}
	if !found {
		t.Error("trade not found in database after ingestion")
	}

	// Verify position was created (scoped to default test tenant)
	positions, err := repo.ListPositions(ctx, defaultTestTenantID, "test-account", "open")
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}

	posFound := false
	for _, pos := range positions {
		if pos.Symbol == "BTC-USD" {
			posFound = true
			if pos.Quantity < 0.1 {
				t.Errorf("expected position quantity >= 0.1, got %f", pos.Quantity)
			}
			break
		}
	}
	if !posFound {
		t.Error("position not found after trade ingestion")
	}
}
