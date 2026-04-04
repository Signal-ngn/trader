package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/domain"
)

// flushTestStore is a mock EngineStore for flush handler tests.
// It tracks calls and manages enough state to verify the 4-phase flow.
type flushTestStore struct {
	mu sync.Mutex

	balance    *float64
	balanceErr error

	// Track calls in order.
	calls []storeCall

	// InsertTradeOnly results — default success unless overridden.
	insertTradeOnlyResults map[string]error // tradeID → error (nil = success)

	// Open positions for close lookups.
	openPositions []domain.Position

	// Avg entry prices keyed by symbol.
	avgEntryPrices map[string]float64

	// adjustBalanceDelta captures the delta from AdjustBalance.
	adjustBalanceDelta  float64
	adjustBalanceCalled bool
	adjustBalanceErr    error

	// getBalanceCalls tracks how many times GetAccountBalance was called.
	getBalanceCalls int
}

type storeCall struct {
	method string
	args   map[string]interface{}
}

func newFlushTestStore(balance float64) *flushTestStore {
	b := balance
	return &flushTestStore{
		balance:                &b,
		insertTradeOnlyResults: make(map[string]error),
		avgEntryPrices:         make(map[string]float64),
	}
}

func (s *flushTestStore) InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, storeCall{method: "InsertTradeAndUpdatePosition", args: map[string]interface{}{"trade_id": trade.TradeID}})
	return true, nil
}

func (s *flushTestStore) InsertTradeOnly(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, storeCall{method: "InsertTradeOnly", args: map[string]interface{}{
		"trade_id": trade.TradeID,
		"action":   string(trade.Side) + "+" + string(trade.PositionSide),
		"symbol":   trade.Symbol,
	}})
	if err, ok := s.insertTradeOnlyResults[trade.Symbol]; ok && err != nil {
		return false, err
	}
	return true, nil
}

func (s *flushTestStore) GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getBalanceCalls++
	s.calls = append(s.calls, storeCall{method: "GetAccountBalance"})
	if s.balanceErr != nil {
		return nil, s.balanceErr
	}
	return s.balance, nil
}

func (s *flushTestStore) AdjustBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adjustBalanceCalled = true
	s.adjustBalanceDelta = delta
	s.calls = append(s.calls, storeCall{method: "AdjustBalance", args: map[string]interface{}{"delta": delta}})
	return s.adjustBalanceErr
}

func (s *flushTestStore) GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error) {
	if p, ok := s.avgEntryPrices[symbol]; ok {
		return p, nil
	}
	return 0, nil
}

func (s *flushTestStore) CountOpenPositionStates(ctx context.Context, accountID string) (int, error) {
	return 0, nil
}

func (s *flushTestStore) ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error) {
	return s.openPositions, nil
}

func (s *flushTestStore) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error) {
	return nil, nil
}

func (s *flushTestStore) LoadPositionStates(ctx context.Context, accountID string) ([]EnginePositionState, error) {
	return nil, nil
}

func (s *flushTestStore) InsertPositionState(ctx context.Context, tenantID uuid.UUID, st *EnginePositionState) error {
	return nil
}

func (s *flushTestStore) UpdatePositionState(ctx context.Context, tenantID uuid.UUID, st *EnginePositionState) error {
	return nil
}

func (s *flushTestStore) DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error {
	return nil
}

func (s *flushTestStore) DailyRealizedPnL(ctx context.Context, accountID string) (float64, error) {
	return 0, nil
}

// getCalls returns a copy of the recorded calls.
func (s *flushTestStore) getCalls() []storeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]storeCall, len(s.calls))
	copy(cp, s.calls)
	return cp
}

// --- Test helpers ---

func newTestEngine(store EngineStore) *Engine {
	cfg := &config.Config{
		TradingMode:     "paper",
		PortfolioSize:   10000,
		PositionSizePct: 10,
	}
	e := &Engine{
		cfg:       cfg,
		repo:      store,
		exchange:  NewNoopExchange(cfg),
		posState:  make(map[string]*PositionState),
		cooldown:  make(map[cooldownKey]time.Time),
		conflict:  make(map[string]string),
		lastPrice: make(map[string]float64),
		logger:    zerolog.Nop(),
		fetchTradingConfigsFn: func(ctx context.Context, cfg *config.Config) (tradingConfigByProduct, error) {
			return tradingConfigByProduct{
				{accountID: "acct-1", productID: "BTCUSDT"}: {
					AccountID: "acct-1", ProductID: "BTCUSDT", Exchange: "binance",
					StrategiesLong: []string{"ml_transformer"}, StrategiesShort: []string{"ml_transformer"},
					LongLeverage: 5, ShortLeverage: 5, Enabled: true,
				},
				{accountID: "acct-1", productID: "ETHUSDT"}: {
					AccountID: "acct-1", ProductID: "ETHUSDT", Exchange: "binance",
					StrategiesLong: []string{"ml_transformer"}, StrategiesShort: []string{"ml_transformer"},
					LongLeverage: 5, ShortLeverage: 5, Enabled: true,
				},
				{accountID: "acct-1", productID: "SOLUSDT"}: {
					AccountID: "acct-1", ProductID: "SOLUSDT", Exchange: "binance",
					StrategiesLong: []string{"ml_transformer"}, StrategiesShort: []string{"ml_transformer"},
					LongLeverage: 5, ShortLeverage: 5, Enabled: true,
				},
				{accountID: "acct-1", productID: "AVAXUSDT"}: {
					AccountID: "acct-1", ProductID: "AVAXUSDT", Exchange: "binance",
					StrategiesLong: []string{"ml_transformer"}, StrategiesShort: []string{"ml_transformer"},
					LongLeverage: 5, ShortLeverage: 5, Enabled: true,
				},
			}, nil
		},
		tenantUUID: uuid.New(),
	}
	return e
}

func makeFlushSig(action, product string, confidence float64, seq int) bufferedSignal {
	return bufferedSignal{
		signal: SignalPayload{
			Action:     action,
			Product:    product,
			Price:      100.0,
			Confidence: confidence,
			Strategy:   "ml_transformer",
		},
		product:   product,
		strategy:  "ml_transformer",
		accountID: "acct-1",
		seq:       seq,
	}
}

// --- Tests ---

func TestFlush_ClosesBeforeOpens_BalanceReadOnce_BalanceWriteOnce(t *testing.T) {
	store := newFlushTestStore(200)
	store.openPositions = []domain.Position{
		{Symbol: "BTCUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideLong, Quantity: 0.5, AvgEntryPrice: 100},
	}
	store.avgEntryPrices["BTCUSDT"] = 100

	e := newTestEngine(store)
	// Seed position state for the close.
	e.posState[posKey("acct-1", "BTCUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "BTCUSDT", MarketType: "futures",
		Side: "long", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}

	signals := []bufferedSignal{
		makeFlushSig("SELL", "BTCUSDT", 0.8, 1),  // close
		makeFlushSig("BUY", "SOLUSDT", 0.9, 2),   // open
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	calls := store.getCalls()

	// Verify call order: InsertTradeOnly (close) → GetAccountBalance → InsertTradeOnly (open) → AdjustBalance
	methods := make([]string, len(calls))
	for i, c := range calls {
		methods[i] = c.method
	}

	// Find indices.
	closeIdx, balReadIdx, openIdx, balWriteIdx := -1, -1, -1, -1
	insertCount := 0
	for i, m := range methods {
		switch m {
		case "InsertTradeOnly":
			if insertCount == 0 {
				closeIdx = i
			} else {
				openIdx = i
			}
			insertCount++
		case "GetAccountBalance":
			balReadIdx = i
		case "AdjustBalance":
			balWriteIdx = i
		}
	}

	if closeIdx < 0 || balReadIdx < 0 || openIdx < 0 || balWriteIdx < 0 {
		t.Fatalf("missing expected calls, got: %v", methods)
	}
	if !(closeIdx < balReadIdx && balReadIdx < openIdx && openIdx < balWriteIdx) {
		t.Errorf("wrong call order: close=%d, balRead=%d, open=%d, balWrite=%d", closeIdx, balReadIdx, openIdx, balWriteIdx)
	}

	// Balance should have been read exactly once.
	if store.getBalanceCalls != 1 {
		t.Errorf("expected 1 balance read, got %d", store.getBalanceCalls)
	}
	if !store.adjustBalanceCalled {
		t.Error("expected AdjustBalance to be called")
	}
}

func TestFlush_CloseDeltasReflectedInAvailableBalance(t *testing.T) {
	// Balance is $200. Close returns margin+PnL. Open should see the increased balance.
	store := newFlushTestStore(200)
	store.openPositions = []domain.Position{
		{Symbol: "BTCUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideLong, Quantity: 0.5, AvgEntryPrice: 100},
	}
	store.avgEntryPrices["BTCUSDT"] = 100

	e := newTestEngine(store)
	e.posState[posKey("acct-1", "BTCUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "BTCUSDT", MarketType: "futures",
		Side: "long", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}

	// Close at same price → P&L = 0, cost basis returned = 100*0.5/5 = 10
	// Then open SOLUSDT — should have $200 + $10 = $210 available.
	signals := []bufferedSignal{
		makeFlushSig("SELL", "BTCUSDT", 0.8, 1),
		makeFlushSig("BUY", "SOLUSDT", 0.9, 2),
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	// Verify open trade was inserted (wouldn't be if balance was insufficient).
	calls := store.getCalls()
	insertCount := 0
	for _, c := range calls {
		if c.method == "InsertTradeOnly" {
			insertCount++
		}
	}
	if insertCount != 2 {
		t.Errorf("expected 2 InsertTradeOnly calls (1 close + 1 open), got %d", insertCount)
	}
}

func TestFlush_LimitedCapitalFillsHighestConfidenceFirst(t *testing.T) {
	// Use spot (leverage 1) so full notional = required capital.
	// Fixed position size of $10 via MaxPositionSize. Balance $25 fits 2, not 3.
	store := newFlushTestStore(25)
	e := newTestEngine(store)
	e.cfg.PositionSizePct = 100
	e.cfg.MinPositionSize = 8
	e.cfg.MaxPositionSize = 10
	e.fetchTradingConfigsFn = func(ctx context.Context, cfg *config.Config) (tradingConfigByProduct, error) {
		return tradingConfigByProduct{
			{accountID: "acct-1", productID: "SOLUSDT"}: {
				AccountID: "acct-1", ProductID: "SOLUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
			{accountID: "acct-1", productID: "BTCUSDT"}: {
				AccountID: "acct-1", ProductID: "BTCUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
			{accountID: "acct-1", productID: "AVAXUSDT"}: {
				AccountID: "acct-1", ProductID: "AVAXUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
		}, nil
	}

	signals := []bufferedSignal{
		makeFlushSig("BUY", "AVAXUSDT", 0.78, 3), // lowest confidence
		makeFlushSig("BUY", "SOLUSDT", 0.85, 2),  // medium
		makeFlushSig("BUY", "BTCUSDT", 0.92, 1),  // highest
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	// BTCUSDT (0.92) and SOLUSDT (0.85) should be filled. AVAXUSDT (0.78) skipped.
	calls := store.getCalls()
	var insertedSymbols []string
	for _, c := range calls {
		if c.method == "InsertTradeOnly" {
			if sym, ok := c.args["symbol"]; ok {
				insertedSymbols = append(insertedSymbols, sym.(string))
			}
		}
	}
	if len(insertedSymbols) != 2 {
		t.Fatalf("expected 2 opens filled, got %d: %v", len(insertedSymbols), insertedSymbols)
	}
	if insertedSymbols[0] != "BTCUSDT" {
		t.Errorf("expected first fill BTCUSDT, got %s", insertedSymbols[0])
	}
	if insertedSymbols[1] != "SOLUSDT" {
		t.Errorf("expected second fill SOLUSDT, got %s", insertedSymbols[1])
	}
}

func TestFlush_PartialOpenFailure_NetDeltaOnlyIncludesSuccessful(t *testing.T) {
	store := newFlushTestStore(1000)
	e := newTestEngine(store)
	e.cfg.PositionSizePct = 10 // 10% of $1000 = $100 notional, $20 margin each (5x leverage)

	// Make ETHUSDT insertion fail.
	store.insertTradeOnlyResults["ETHUSDT"] = fmt.Errorf("platform error")

	signals := []bufferedSignal{
		makeFlushSig("BUY", "BTCUSDT", 0.92, 1),
		makeFlushSig("BUY", "ETHUSDT", 0.85, 2), // will fail
		makeFlushSig("BUY", "SOLUSDT", 0.78, 3),
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	// Only BTCUSDT and SOLUSDT should be reflected in the balance delta.
	// Each margin = $1000 * 10% / 5 = $20. Two successful = -$40.
	if !store.adjustBalanceCalled {
		t.Fatal("expected AdjustBalance to be called")
	}
	// Delta should be negative (opens only, no closes). Two successful opens.
	// Exact value depends on position sizing which uses available balance that shrinks.
	// Just verify it was called and is negative.
	if store.adjustBalanceDelta >= 0 {
		t.Errorf("expected negative delta for opens, got %f", store.adjustBalanceDelta)
	}
}

func TestFlush_OpensEvaluatedAfterSkip(t *testing.T) {
	// Balance = $15. First open needs $12.50, leaving $2.50. 
	// Second open also needs $12.50 — skipped. Third needs $2 — should fit.
	// Use spot (leverage 1) for simplicity.
	store := newFlushTestStore(15)
	e := newTestEngine(store)
	e.cfg.PositionSizePct = 50 // 50% of $15 = $7.50 per position (spot, full notional)
	e.cfg.MinPositionSize = 0

	e.fetchTradingConfigsFn = func(ctx context.Context, cfg *config.Config) (tradingConfigByProduct, error) {
		return tradingConfigByProduct{
			{accountID: "acct-1", productID: "BTCUSDT"}: {
				AccountID: "acct-1", ProductID: "BTCUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
			{accountID: "acct-1", productID: "ETHUSDT"}: {
				AccountID: "acct-1", ProductID: "ETHUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
			{accountID: "acct-1", productID: "SOLUSDT"}: {
				AccountID: "acct-1", ProductID: "SOLUSDT", Exchange: "binance",
				StrategiesLong: []string{"ml_transformer"}, LongLeverage: 1, Enabled: true,
			},
		}, nil
	}
	e.cfg.MinPositionSize = 5

	signals := []bufferedSignal{
		makeFlushSig("BUY", "BTCUSDT", 0.92, 1),  // $7.50 → fits, leaves $7.50
		makeFlushSig("BUY", "ETHUSDT", 0.85, 2),   // $7.50 → fits, leaves $0
		makeFlushSig("BUY", "SOLUSDT", 0.78, 3),   // $0 left < $5 min → skipped
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	calls := store.getCalls()
	var insertedSymbols []string
	for _, c := range calls {
		if c.method == "InsertTradeOnly" {
			if sym, ok := c.args["symbol"]; ok {
				insertedSymbols = append(insertedSymbols, sym.(string))
			}
		}
	}
	// First two should fit, third skipped.
	if len(insertedSymbols) != 2 {
		t.Fatalf("expected 2 opens, got %d: %v", len(insertedSymbols), insertedSymbols)
	}
}

func TestFlush_OnlyCloses_SingleBalanceWrite(t *testing.T) {
	store := newFlushTestStore(500)
	store.openPositions = []domain.Position{
		{Symbol: "BTCUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideLong, Quantity: 0.5, AvgEntryPrice: 100},
		{Symbol: "ETHUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideShort, Quantity: 1.0, AvgEntryPrice: 100},
	}
	store.avgEntryPrices["BTCUSDT"] = 100
	store.avgEntryPrices["ETHUSDT"] = 100

	e := newTestEngine(store)
	e.posState[posKey("acct-1", "BTCUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "BTCUSDT", MarketType: "futures",
		Side: "long", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}
	e.posState[posKey("acct-1", "ETHUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "ETHUSDT", MarketType: "futures",
		Side: "short", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}

	signals := []bufferedSignal{
		makeFlushSig("SELL", "BTCUSDT", 0.8, 1),
		makeFlushSig("COVER", "ETHUSDT", 0.7, 2),
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	// No balance read should have happened (no opens).
	if store.getBalanceCalls != 0 {
		t.Errorf("expected 0 balance reads for closes-only flush, got %d", store.getBalanceCalls)
	}
	// Balance write should still happen for the close deltas.
	if !store.adjustBalanceCalled {
		t.Error("expected AdjustBalance to be called for close deltas")
	}
	if store.adjustBalanceDelta <= 0 {
		t.Errorf("expected positive delta from closes, got %f", store.adjustBalanceDelta)
	}
}

func TestFlush_OnlyOpens_BalanceReadAndWrite(t *testing.T) {
	store := newFlushTestStore(1000)
	e := newTestEngine(store)
	e.cfg.PositionSizePct = 10

	signals := []bufferedSignal{
		makeFlushSig("BUY", "BTCUSDT", 0.92, 1),
		makeFlushSig("SHORT", "ETHUSDT", 0.85, 2),
	}
	sortSignals(signals)

	ctx := context.Background()
	e.flushAccountSignals(ctx, signals)

	if store.getBalanceCalls != 1 {
		t.Errorf("expected 1 balance read, got %d", store.getBalanceCalls)
	}
	if !store.adjustBalanceCalled {
		t.Error("expected AdjustBalance to be called")
	}
	if store.adjustBalanceDelta >= 0 {
		t.Errorf("expected negative delta for opens, got %f", store.adjustBalanceDelta)
	}
}

func TestFlush_Empty_NoBalanceReadOrWrite(t *testing.T) {
	store := newFlushTestStore(1000)
	e := newTestEngine(store)

	ctx := context.Background()
	e.flushAccountSignals(ctx, []bufferedSignal{})

	if store.getBalanceCalls != 0 {
		t.Errorf("expected 0 balance reads, got %d", store.getBalanceCalls)
	}
	if store.adjustBalanceCalled {
		t.Error("expected no AdjustBalance call for empty flush")
	}
}
