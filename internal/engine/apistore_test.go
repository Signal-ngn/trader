package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Signal-ngn/trader/internal/domain"
	"github.com/Signal-ngn/trader/internal/engine"
)

// mockEngineStore is a test double for EngineStore.
type mockEngineStore struct {
	submitTradeErr    error
	submitTradeDup    bool
	balance           *float64
	adjustBalanceErr  error
	insertPosErr      error
	loadPosStates     []engine.EnginePositionState
	loadPosErr        error
	deletePosErr      error
	updatePosErr      error
	countOpenPos      int
	avgEntryPrice     float64
	openPositions     []domain.Position
	listAccountsItems []domain.Account
	dailyPnL          float64
	dailyPnLErr       error

	// Captured calls
	lastSubmittedTrade  *domain.Trade
	adjustBalanceDelta  float64
	adjustBalanceCalled bool
}

func (m *mockEngineStore) InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	m.lastSubmittedTrade = trade
	if m.submitTradeDup {
		return false, nil
	}
	if m.submitTradeErr != nil {
		return false, m.submitTradeErr
	}
	return true, nil
}

func (m *mockEngineStore) GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error) {
	return m.balance, nil
}

func (m *mockEngineStore) AdjustBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, delta float64) error {
	m.adjustBalanceCalled = true
	m.adjustBalanceDelta = delta
	return m.adjustBalanceErr
}

func (m *mockEngineStore) GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error) {
	return m.avgEntryPrice, nil
}

func (m *mockEngineStore) CountOpenPositionStates(ctx context.Context, accountID string) (int, error) {
	return m.countOpenPos, nil
}

func (m *mockEngineStore) ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error) {
	return m.openPositions, nil
}

func (m *mockEngineStore) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error) {
	return m.listAccountsItems, nil
}

func (m *mockEngineStore) LoadPositionStates(ctx context.Context, accountID string) ([]engine.EnginePositionState, error) {
	return m.loadPosStates, m.loadPosErr
}

func (m *mockEngineStore) InsertPositionState(ctx context.Context, tenantID uuid.UUID, s *engine.EnginePositionState) error {
	return m.insertPosErr
}

func (m *mockEngineStore) UpdatePositionState(ctx context.Context, tenantID uuid.UUID, s *engine.EnginePositionState) error {
	return m.updatePosErr
}

func (m *mockEngineStore) DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error {
	return m.deletePosErr
}

func (m *mockEngineStore) DailyRealizedPnL(ctx context.Context, accountID string) (float64, error) {
	return m.dailyPnL, m.dailyPnLErr
}

// compile-time assertion that mockEngineStore satisfies EngineStore.
var _ engine.EngineStore = (*mockEngineStore)(nil)

// --- InsertTradeAndUpdatePosition tests (task 11.1) ---

func TestMockStore_InsertTrade_Success(t *testing.T) {
	m := &mockEngineStore{}
	ctx := context.Background()
	tenantID := uuid.New()
	trade := &domain.Trade{
		TradeID:   "test-1",
		AccountID: "paper",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Quantity:  0.1,
		Price:     50000,
		MarketType: domain.MarketTypeSpot,
		Timestamp: time.Now(),
	}

	inserted, err := m.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !inserted {
		t.Fatal("expected inserted=true")
	}
	if m.lastSubmittedTrade != trade {
		t.Fatal("expected trade to be captured")
	}
}

func TestMockStore_InsertTrade_Duplicate(t *testing.T) {
	m := &mockEngineStore{submitTradeDup: true}
	ctx := context.Background()

	inserted, err := m.InsertTradeAndUpdatePosition(ctx, uuid.New(), &domain.Trade{TradeID: "dup"})
	if err != nil {
		t.Fatalf("expected no error on duplicate, got %v", err)
	}
	if inserted {
		t.Fatal("expected inserted=false on duplicate")
	}
}

func TestMockStore_InsertTrade_Error(t *testing.T) {
	errPlatform := errors.New("platform unavailable")
	m := &mockEngineStore{submitTradeErr: errPlatform}
	ctx := context.Background()

	inserted, err := m.InsertTradeAndUpdatePosition(ctx, uuid.New(), &domain.Trade{TradeID: "err"})
	if err == nil {
		t.Fatal("expected error")
	}
	if inserted {
		t.Fatal("expected inserted=false on error")
	}
}

// --- AdjustBalance tests (task 11.2) ---

func TestMockStore_AdjustBalance_Open(t *testing.T) {
	m := &mockEngineStore{}
	ctx := context.Background()
	tenantID := uuid.New()

	err := m.AdjustBalance(ctx, tenantID, "paper", "USD", -500.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.adjustBalanceCalled {
		t.Fatal("expected AdjustBalance to be called")
	}
	if m.adjustBalanceDelta != -500.0 {
		t.Fatalf("expected delta -500, got %v", m.adjustBalanceDelta)
	}
}

func TestMockStore_AdjustBalance_Close(t *testing.T) {
	m := &mockEngineStore{}
	ctx := context.Background()

	err := m.AdjustBalance(ctx, uuid.New(), "paper", "USD", 600.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.adjustBalanceDelta != 600.0 {
		t.Fatalf("expected delta 600, got %v", m.adjustBalanceDelta)
	}
}

func TestMockStore_AdjustBalance_Error(t *testing.T) {
	errBal := errors.New("balance service down")
	m := &mockEngineStore{adjustBalanceErr: errBal}
	ctx := context.Background()

	err := m.AdjustBalance(ctx, uuid.New(), "paper", "USD", 100.0)
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- LoadPositionStates / round-trip tests (task 11.3) ---

func TestMockStore_LoadPositionStates_Empty(t *testing.T) {
	m := &mockEngineStore{}
	ctx := context.Background()

	states, err := m.LoadPositionStates(ctx, "paper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("expected empty states, got %d", len(states))
	}
}

func TestMockStore_LoadPositionStates_WithData(t *testing.T) {
	now := time.Now().UTC()
	expected := []engine.EnginePositionState{
		{
			AccountID:  "paper",
			Symbol:     "BTC-USD",
			MarketType: "futures",
			Side:       "long",
			EntryPrice: 48000,
			StopLoss:   45000,
			TakeProfit: 55000,
			HardStop:   40000,
			Leverage:   10,
			Strategy:   "ml_transformer",
			Granularity: "1h",
			OpenedAt:   now,
		},
	}
	m := &mockEngineStore{loadPosStates: expected}
	ctx := context.Background()

	states, err := m.LoadPositionStates(ctx, "paper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if states[0].Symbol != "BTC-USD" {
		t.Fatalf("wrong symbol: %s", states[0].Symbol)
	}
	if states[0].StopLoss != 45000 {
		t.Fatalf("wrong stop loss: %v", states[0].StopLoss)
	}
}

func TestMockStore_InsertAndDeletePositionState(t *testing.T) {
	m := &mockEngineStore{}
	ctx := context.Background()
	tenantID := uuid.New()

	state := &engine.EnginePositionState{
		AccountID:  "paper",
		Symbol:     "ETH-USD",
		MarketType: "spot",
		Side:       "long",
		EntryPrice: 3000,
	}

	if err := m.InsertPositionState(ctx, tenantID, state); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := m.DeletePositionState(ctx, tenantID, state.Symbol, state.MarketType, state.AccountID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// --- DailyRealizedPnL tests (task 11.4) ---

func TestMockStore_DailyPnL_Zero(t *testing.T) {
	m := &mockEngineStore{dailyPnL: 0}
	ctx := context.Background()

	pnl, err := m.DailyRealizedPnL(ctx, "paper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pnl != 0 {
		t.Fatalf("expected 0, got %v", pnl)
	}
}

func TestMockStore_DailyPnL_Negative(t *testing.T) {
	m := &mockEngineStore{dailyPnL: -350.5}
	ctx := context.Background()

	pnl, err := m.DailyRealizedPnL(ctx, "paper")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pnl != -350.5 {
		t.Fatalf("expected -350.5, got %v", pnl)
	}
}

func TestMockStore_DailyPnL_Error(t *testing.T) {
	m := &mockEngineStore{dailyPnLErr: errors.New("firestore error")}
	ctx := context.Background()

	_, err := m.DailyRealizedPnL(ctx, "paper")
	if err == nil {
		t.Fatal("expected error")
	}
}
