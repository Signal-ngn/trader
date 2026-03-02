package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Signal-ngn/trader/internal/domain"
)

// EnginePositionState holds risk metadata for a single open position tracked by the engine.
type EnginePositionState struct {
	ID           int64
	AccountID    string
	Symbol       string
	MarketType   string
	Side         string  // "long" or "short"
	EntryPrice   float64
	StopLoss     float64
	TakeProfit   float64
	Leverage     int
	Strategy     string
	OpenedAt     time.Time
	PeakPrice    float64
	TrailingStop float64
}

// InsertPositionState inserts a new engine_position_state row.
// Uses ON CONFLICT (account_id, symbol, market_type, tenant_id) DO UPDATE
// so that a restart after an unexpected crash doesn't fail on a duplicate.
func (r *Repository) InsertPositionState(ctx context.Context, tenantID uuid.UUID, s *EnginePositionState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO engine_position_state (
			account_id, symbol, market_type, side, entry_price,
			stop_loss, take_profit, leverage, strategy, opened_at,
			peak_price, trailing_stop, tenant_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (account_id, symbol, market_type, tenant_id) DO UPDATE SET
			side          = EXCLUDED.side,
			entry_price   = EXCLUDED.entry_price,
			stop_loss     = EXCLUDED.stop_loss,
			take_profit   = EXCLUDED.take_profit,
			leverage      = EXCLUDED.leverage,
			strategy      = EXCLUDED.strategy,
			opened_at     = EXCLUDED.opened_at,
			peak_price    = EXCLUDED.peak_price,
			trailing_stop = EXCLUDED.trailing_stop
	`,
		s.AccountID, s.Symbol, s.MarketType, s.Side, s.EntryPrice,
		nullFloat(s.StopLoss), nullFloat(s.TakeProfit),
		nullInt(s.Leverage), nullString(s.Strategy), s.OpenedAt,
		nullFloat(s.PeakPrice), nullFloat(s.TrailingStop),
		tenantID,
	)
	if err != nil {
		return fmt.Errorf("insert position state: %w", err)
	}
	return nil
}

// LoadPositionStates loads all engine_position_state rows for the given account.
func (r *Repository) LoadPositionStates(ctx context.Context, accountID string) ([]EnginePositionState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, account_id, symbol, market_type, side, entry_price,
			COALESCE(stop_loss, 0), COALESCE(take_profit, 0), COALESCE(leverage, 0),
			COALESCE(strategy, ''), opened_at,
			COALESCE(peak_price, 0), COALESCE(trailing_stop, 0)
		FROM engine_position_state
		WHERE account_id = $1
		ORDER BY opened_at ASC
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("load position states: %w", err)
	}
	defer rows.Close()

	var states []EnginePositionState
	for rows.Next() {
		var s EnginePositionState
		err := rows.Scan(
			&s.ID, &s.AccountID, &s.Symbol, &s.MarketType, &s.Side, &s.EntryPrice,
			&s.StopLoss, &s.TakeProfit, &s.Leverage,
			&s.Strategy, &s.OpenedAt,
			&s.PeakPrice, &s.TrailingStop,
		)
		if err != nil {
			return nil, fmt.Errorf("scan position state: %w", err)
		}
		states = append(states, s)
	}
	return states, nil
}

// UpdatePositionState updates the mutable risk fields (peak_price, trailing_stop).
func (r *Repository) UpdatePositionState(ctx context.Context, tenantID uuid.UUID, s *EnginePositionState) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE engine_position_state
		SET peak_price = $1, trailing_stop = $2, stop_loss = $3, take_profit = $4
		WHERE account_id = $5 AND symbol = $6 AND market_type = $7 AND tenant_id = $8
	`,
		nullFloat(s.PeakPrice), nullFloat(s.TrailingStop),
		nullFloat(s.StopLoss), nullFloat(s.TakeProfit),
		s.AccountID, s.Symbol, s.MarketType, tenantID,
	)
	if err != nil {
		return fmt.Errorf("update position state: %w", err)
	}
	return nil
}

// DeletePositionState removes the engine_position_state row for a given position.
func (r *Repository) DeletePositionState(ctx context.Context, tenantID uuid.UUID, symbol, marketType, accountID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM engine_position_state
		WHERE account_id = $1 AND symbol = $2 AND market_type = $3 AND tenant_id = $4
	`, accountID, symbol, marketType, tenantID)
	if err != nil {
		return fmt.Errorf("delete position state: %w", err)
	}
	return nil
}

// CountOpenPositionStates returns the number of open position state rows for the account.
func (r *Repository) CountOpenPositionStates(ctx context.Context, accountID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM engine_position_state WHERE account_id = $1",
		accountID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count open position states: %w", err)
	}
	return count, nil
}

// ListOpenPositionsForAccount returns all open ledger positions for the given accountID
// across all tenants. Used by the engine to seed the conflict guard on startup.
func (r *Repository) ListOpenPositionsForAccount(ctx context.Context, accountID string) ([]domain.Position, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
			cost_basis, realized_pnl, status, opened_at
		FROM ledger_positions
		WHERE account_id = $1 AND status = 'open'
		ORDER BY opened_at ASC
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list open positions: %w", err)
	}
	defer rows.Close()

	var positions []domain.Position
	for rows.Next() {
		var p domain.Position
		var side, marketType, status string
		err := rows.Scan(
			&p.ID, &p.AccountID, &p.Symbol, &marketType, &side,
			&p.Quantity, &p.AvgEntryPrice, &p.CostBasis, &p.RealizedPnL,
			&status, &p.OpenedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		p.Side = domain.PositionSide(side)
		p.MarketType = domain.MarketType(marketType)
		p.Status = domain.PositionStatus(status)
		positions = append(positions, p)
	}
	return positions, nil
}

// DailyRealizedPnL returns the sum of realized P&L for trades recorded since midnight UTC today.
// Used by the engine to enforce the daily loss limit.
func (r *Repository) DailyRealizedPnL(ctx context.Context, accountID string) (float64, error) {
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	var pnl float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(realized_pnl), 0)
		FROM ledger_trades
		WHERE account_id = $1 AND ingested_at >= $2
	`, accountID, midnight).Scan(&pnl)
	if err != nil && err != pgx.ErrNoRows {
		return 0, fmt.Errorf("daily realized pnl: %w", err)
	}
	return pnl, nil
}

// Helper: convert zero float to nil (for nullable DB columns).
func nullFloat(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

// Helper: convert zero int to nil.
func nullInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

// Helper: convert empty string to nil.
func nullString(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}
