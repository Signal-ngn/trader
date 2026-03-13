package engine

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Signal-ngn/trader/internal/domain"
)

// EnginePositionState holds risk metadata for a single open position tracked
// by the engine. Previously defined in internal/store — now owned by the engine
// package so the engine has no dependency on internal/store.
type EnginePositionState struct {
	ID           int64
	AccountID    string
	Symbol       string
	MarketType   string
	Side         string  // "long" or "short"
	EntryPrice   float64
	StopLoss     float64
	TakeProfit   float64
	HardStop     float64 // leverage-scaled circuit-breaker price; 0 = not yet set
	Leverage     int
	Strategy     string
	Granularity  string    // candle granularity from trading config; "" = unknown
	OpenedAt     time.Time
	PeakPrice    float64
	TrailingStop float64
}

// EngineStore is the narrow storage interface used by the trading engine.
// It is satisfied by APIEngineStore (backed by the platform API + Firestore)
// and may be mocked for tests. The engine package has no direct dependency on
// internal/store or any database driver.
type EngineStore interface {
	// InsertTradeAndUpdatePosition records a trade via the platform API and
	// updates the associated position. Returns (true, nil) on success,
	// (false, nil) on duplicate (idempotent), (false, err) on failure.
	InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error)

	// GetAccountBalance returns the current USD balance for the account, or nil
	// when no balance record exists.
	GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error)

	// AdjustBalance applies a signed delta to the account balance.
	AdjustBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, delta float64) error

	// GetAvgEntryPrice returns the average entry price for an open position, or
	// 0 if no matching open position exists.
	GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error)

	// CountOpenPositionStates returns the number of open position state entries
	// for the account (used for the max-positions guard).
	CountOpenPositionStates(ctx context.Context, accountID string) (int, error)

	// ListOpenPositionsForAccount returns all open positions for the account,
	// used to seed the conflict guard on startup and during close execution.
	ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error)

	// ListAccounts returns all accounts for the tenant. Used on startup to
	// determine which accounts the engine should manage.
	ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error)

	// LoadPositionStates loads all engine position state entries for the account.
	// Called on startup to restore the in-memory risk map.
	LoadPositionStates(ctx context.Context, accountID string) ([]EnginePositionState, error)

	// InsertPositionState persists a new position's risk state.
	InsertPositionState(ctx context.Context, tenantID uuid.UUID, s *EnginePositionState) error

	// UpdatePositionState updates the mutable risk fields (trailing stop, peak
	// price, stop loss, take profit) for an existing position state entry.
	UpdatePositionState(ctx context.Context, tenantID uuid.UUID, s *EnginePositionState) error

	// DeletePositionState removes the position state entry for a closed position.
	DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error

	// DailyRealizedPnL returns the sum of realised P&L for closed trades since
	// midnight UTC today, for the given account. Returns 0 when no trades have
	// been closed today.
	DailyRealizedPnL(ctx context.Context, accountID string) (float64, error)
}
