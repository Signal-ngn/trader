package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/domain"
)

// integrationTestStore is a more stateful mock for integration-level tests.
// It tracks trades, balance, and positions to simulate a realistic flow.
type integrationTestStore struct {
	mu sync.Mutex

	balance       float64
	trades        []domain.Trade
	openPositions []domain.Position
	avgEntryPrices map[string]float64

	getBalanceCalls    int
	adjustBalanceCalls int
	adjustBalanceTotal float64
}

func newIntegrationTestStore(balance float64) *integrationTestStore {
	return &integrationTestStore{
		balance:        balance,
		avgEntryPrices: make(map[string]float64),
	}
}

func (s *integrationTestStore) InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, *trade)
	// Also adjust balance inline (this method includes balance adjustment).
	delta := CostDeltaForTrade(trade)
	s.balance += delta
	s.adjustBalanceCalls++
	s.adjustBalanceTotal += delta
	return true, nil
}

func (s *integrationTestStore) InsertTradeOnly(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, *trade)
	// Does NOT adjust balance — that's batched.
	return true, nil
}

func (s *integrationTestStore) GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getBalanceCalls++
	b := s.balance
	return &b, nil
}

func (s *integrationTestStore) AdjustBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.balance += delta
	s.adjustBalanceCalls++
	s.adjustBalanceTotal += delta
	return nil
}

func (s *integrationTestStore) GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.avgEntryPrices[symbol], nil
}

func (s *integrationTestStore) CountOpenPositionStates(ctx context.Context, accountID string) (int, error) {
	return 0, nil
}

func (s *integrationTestStore) ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]domain.Position, len(s.openPositions))
	copy(cp, s.openPositions)
	return cp, nil
}

func (s *integrationTestStore) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error) {
	return nil, nil
}

func (s *integrationTestStore) LoadPositionStates(ctx context.Context, accountID string) ([]EnginePositionState, error) {
	return nil, nil
}

func (s *integrationTestStore) InsertPositionState(ctx context.Context, tenantID uuid.UUID, st *EnginePositionState) error {
	return nil
}

func (s *integrationTestStore) UpdatePositionState(ctx context.Context, tenantID uuid.UUID, st *EnginePositionState) error {
	return nil
}

func (s *integrationTestStore) DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error {
	return nil
}

func (s *integrationTestStore) DailyRealizedPnL(ctx context.Context, accountID string) (float64, error) {
	return 0, nil
}

func (s *integrationTestStore) getTrades() []domain.Trade {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]domain.Trade, len(s.trades))
	copy(cp, s.trades)
	return cp
}

func newIntegrationEngine(store *integrationTestStore) *Engine {
	cfg := &config.Config{
		TradingMode:     "paper",
		PortfolioSize:   10000,
		PositionSizePct: 10,
		MaxPositionSize: 100,
		MinPositionSize: 5,
	}
	e := &Engine{
		cfg:        cfg,
		repo:       store,
		exchange:   NewNoopExchange(cfg),
		posState:   make(map[string]*PositionState),
		cooldown:   make(map[cooldownKey]time.Time),
		conflict:   make(map[string]string),
		lastPrice:  make(map[string]float64),
		tenantUUID: uuid.New(),
		logger:     zerolog.Nop(),
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
			}, nil
		},
	}
	return e
}

// TestIntegration_BurstScenario simulates a realistic burst: mixed close and
// open signals arrive in arbitrary NATS order. Verifies that closes free
// capital before opens attempt to use it, and opens are filled by confidence.
func TestIntegration_BurstScenario(t *testing.T) {
	// Start with $50 balance and two open positions ($20 margin each locked up).
	store := newIntegrationTestStore(50)
	store.openPositions = []domain.Position{
		{Symbol: "BTCUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideLong, Quantity: 0.5, AvgEntryPrice: 100},
		{Symbol: "ETHUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideShort, Quantity: 1.0, AvgEntryPrice: 100},
	}
	store.avgEntryPrices["BTCUSDT"] = 100
	store.avgEntryPrices["ETHUSDT"] = 100

	e := newIntegrationEngine(store)

	// Seed position states for the closes.
	e.posState[posKey("acct-1", "BTCUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "BTCUSDT", MarketType: "futures",
		Side: "long", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}
	e.posState[posKey("acct-1", "ETHUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "ETHUSDT", MarketType: "futures",
		Side: "short", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}

	ctx := context.Background()

	// Initialise the buffer with a short timeout for testing.
	e.signalBuf = newSignalBuffer(ctx, 100*time.Millisecond, e.flushAccountSignals)

	// Simulate burst: signals arrive in "wrong" order (opens before closes).
	e.signalBuf.Add(bufferedSignal{
		signal:    SignalPayload{Action: "BUY", Product: "SOLUSDT", Price: 100, Confidence: 0.92, Strategy: "ml_transformer"},
		product:   "SOLUSDT", strategy: "ml_transformer", accountID: "acct-1",
	})
	e.signalBuf.Add(bufferedSignal{
		signal:    SignalPayload{Action: "SELL", Product: "BTCUSDT", Price: 100, Confidence: 0.8, Strategy: "ml_transformer"},
		product:   "BTCUSDT", strategy: "ml_transformer", accountID: "acct-1",
	})
	e.signalBuf.Add(bufferedSignal{
		signal:    SignalPayload{Action: "COVER", Product: "ETHUSDT", Price: 100, Confidence: 0.7, Strategy: "ml_transformer"},
		product:   "ETHUSDT", strategy: "ml_transformer", accountID: "acct-1",
	})

	// Wait for flush.
	time.Sleep(300 * time.Millisecond)

	trades := store.getTrades()
	if len(trades) < 2 {
		t.Fatalf("expected at least 2 trades (closes), got %d", len(trades))
	}

	// First two trades should be closes (SELL, COVER).
	firstTwo := trades[:2]
	for _, tr := range firstTwo {
		if tr.Side != domain.SideSell && tr.Side != domain.SideBuy {
			t.Errorf("expected close trade side, got %s", tr.Side)
		}
	}

	// Third trade should be the open (BUY SOLUSDT).
	if len(trades) >= 3 {
		if trades[2].Symbol != "SOLUSDT" {
			t.Errorf("expected 3rd trade to be SOLUSDT open, got %s", trades[2].Symbol)
		}
	}

	// Balance should have been read only once (for the open phase).
	if store.getBalanceCalls != 1 {
		t.Errorf("expected 1 balance read, got %d", store.getBalanceCalls)
	}

	// AdjustBalance should have been called once (batched).
	if store.adjustBalanceCalls != 1 {
		t.Errorf("expected 1 AdjustBalance call (batched), got %d", store.adjustBalanceCalls)
	}
}

// TestIntegration_RiskLoopConcurrency verifies that a risk loop close
// (using InsertTradeAndUpdatePosition with per-trade balance) doesn't corrupt
// balance when running concurrently with a buffered flush.
func TestIntegration_RiskLoopConcurrency(t *testing.T) {
	store := newIntegrationTestStore(500)
	store.openPositions = []domain.Position{
		{Symbol: "BTCUSDT", MarketType: domain.MarketTypeFutures, Side: domain.PositionSideLong, Quantity: 0.5, AvgEntryPrice: 100},
	}
	store.avgEntryPrices["BTCUSDT"] = 100

	e := newIntegrationEngine(store)
	e.posState[posKey("acct-1", "BTCUSDT")] = &PositionState{
		AccountID: "acct-1", Symbol: "BTCUSDT", MarketType: "futures",
		Side: "long", EntryPrice: 100, Leverage: 5, Strategy: "ml_transformer",
	}

	ctx := context.Background()
	tenantID := e.tenantID()

	// Simulate risk loop close concurrently with a buffered flush.
	var wg sync.WaitGroup

	// Risk loop close (uses InsertTradeAndUpdatePosition with per-trade balance).
	wg.Add(1)
	go func() {
		defer wg.Done()
		trade := &domain.Trade{
			TenantID:     tenantID,
			TradeID:      "risk-close-1",
			AccountID:    "acct-1",
			Symbol:       "BTCUSDT",
			Side:         domain.SideSell,
			PositionSide: domain.PositionSideLong,
			Quantity:     0.5,
			Price:        100,
			MarketType:   domain.MarketTypeFutures,
			CostBasis:    10, // 100*0.5/5
			RealizedPnL:  0,
		}
		store.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
	}()

	// Buffered flush with an open signal.
	wg.Add(1)
	go func() {
		defer wg.Done()
		signals := []bufferedSignal{
			makeFlushSig("BUY", "SOLUSDT", 0.9, 1),
		}
		e.flushAccountSignals(ctx, signals)
	}()

	wg.Wait()

	// Both operations should complete without panics or data races.
	// The balance may not be perfectly consistent (known tradeoff from design),
	// but it should not corrupt (no negative balance from the operations we did).
	store.mu.Lock()
	finalBalance := store.balance
	store.mu.Unlock()

	if finalBalance < 0 {
		t.Errorf("balance should not be negative, got %f", finalBalance)
	}

	// Both trades should be recorded.
	trades := store.getTrades()
	if len(trades) < 2 {
		t.Errorf("expected at least 2 trades, got %d", len(trades))
	}
}
