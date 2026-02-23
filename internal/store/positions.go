package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// UpsertPosition creates or updates a position based on the trade.
// Must be called within a transaction.
func (r *Repository) UpsertPosition(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
	if trade.MarketType == domain.MarketTypeSpot {
		return r.upsertSpotPosition(ctx, tx, trade)
	}
	return r.upsertFuturesPosition(ctx, tx, trade)
}

func (r *Repository) upsertSpotPosition(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
	// Look for existing open position scoped to tenant
	var pos domain.Position
	var side, status string
	err := tx.QueryRow(ctx, `
		SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
			cost_basis, realized_pnl, status, opened_at
		FROM ledger_positions
		WHERE tenant_id = $1 AND account_id = $2 AND symbol = $3 AND market_type = 'spot' AND status = 'open'
	`, trade.TenantID, trade.AccountID, trade.Symbol).Scan(
		&pos.ID, &pos.AccountID, &pos.Symbol, &pos.MarketType, &side,
		&pos.Quantity, &pos.AvgEntryPrice, &pos.CostBasis, &pos.RealizedPnL,
		&status, &pos.OpenedAt,
	)

	if err == pgx.ErrNoRows {
		// No existing position — create new one
		if trade.Side == domain.SideBuy {
			costBasis := trade.Quantity*trade.Price + trade.Fee
			posID := fmt.Sprintf("%s-%s-spot-%d", trade.AccountID, trade.Symbol, trade.Timestamp.Unix())
			_, err := tx.Exec(ctx, `
				INSERT INTO ledger_positions (tenant_id, id, account_id, symbol, market_type, side,
					quantity, avg_entry_price, cost_basis, realized_pnl, status, opened_at,
					stop_loss, take_profit, confidence)
				VALUES ($1, $2, $3, $4, 'spot', 'long', $5, $6, $7, 0, 'open', $8, $9, $10, $11)
			`, trade.TenantID, posID, trade.AccountID, trade.Symbol,
				trade.Quantity, trade.Price, costBasis, trade.Timestamp,
				trade.StopLoss, trade.TakeProfit, trade.Confidence)
			return err
		}
		// Sell without a position — skip (no position to close)
		return nil
	}
	if err != nil {
		return fmt.Errorf("query position: %w", err)
	}

	pos.Side = domain.PositionSide(side)
	pos.Status = domain.PositionStatus(status)

	if trade.Side == domain.SideBuy {
		// Add to position — recalculate weighted average
		newCostBasis := trade.Quantity*trade.Price + trade.Fee
		totalQuantity := pos.Quantity + trade.Quantity
		totalCost := pos.CostBasis + newCostBasis
		avgEntry := totalCost / totalQuantity

		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = $1, avg_entry_price = $2, cost_basis = $3,
				stop_loss = COALESCE($5, stop_loss),
				take_profit = COALESCE($6, take_profit)
			WHERE id = $4
		`, totalQuantity, avgEntry, totalCost, pos.ID, trade.StopLoss, trade.TakeProfit)
		return err
	}

	// Sell — reduce position
	realizedPnL := (trade.Price-pos.AvgEntryPrice)*trade.Quantity - trade.Fee
	newQuantity := pos.Quantity - trade.Quantity

	if newQuantity <= 0 {
		// Position fully closed
		exitPrice := trade.Price
		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = 0, realized_pnl = realized_pnl + $1, status = 'closed', closed_at = $2,
				exit_price = $4, exit_reason = $5
			WHERE id = $3
		`, realizedPnL, trade.Timestamp, pos.ID, exitPrice, trade.ExitReason)
		return err
	}

	// Partial close — reduce quantity, keep proportional cost basis
	remainingCostBasis := pos.AvgEntryPrice * newQuantity
	_, err = tx.Exec(ctx, `
		UPDATE ledger_positions
		SET quantity = $1, cost_basis = $2, realized_pnl = realized_pnl + $3
		WHERE id = $4
	`, newQuantity, remainingCostBasis, realizedPnL, pos.ID)
	return err
}

func (r *Repository) upsertFuturesPosition(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
	// Look for existing open futures position scoped to tenant
	var pos domain.Position
	var side, status string
	err := tx.QueryRow(ctx, `
		SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
			cost_basis, realized_pnl, leverage, margin, liquidation_price, status, opened_at
		FROM ledger_positions
		WHERE tenant_id = $1 AND account_id = $2 AND symbol = $3 AND market_type = 'futures' AND status = 'open'
	`, trade.TenantID, trade.AccountID, trade.Symbol).Scan(
		&pos.ID, &pos.AccountID, &pos.Symbol, &pos.MarketType, &side,
		&pos.Quantity, &pos.AvgEntryPrice, &pos.CostBasis, &pos.RealizedPnL,
		&pos.Leverage, &pos.Margin, &pos.LiquidationPrice, &status, &pos.OpenedAt,
	)

	if err == pgx.ErrNoRows {
		// No existing position — open new futures position
		var posSide domain.PositionSide
		if trade.Side == domain.SideBuy {
			posSide = domain.PositionSideLong
		} else {
			posSide = domain.PositionSideShort
		}

		costBasis := trade.Quantity * trade.Price
		posID := fmt.Sprintf("%s-%s-futures-%d", trade.AccountID, trade.Symbol, trade.Timestamp.Unix())
		_, err := tx.Exec(ctx, `
			INSERT INTO ledger_positions (tenant_id, id, account_id, symbol, market_type, side,
				quantity, avg_entry_price, cost_basis, realized_pnl,
				leverage, margin, liquidation_price, status, opened_at,
				stop_loss, take_profit, confidence)
			VALUES ($1, $2, $3, $4, 'futures', $5, $6, $7, $8, 0, $9, $10, $11, 'open', $12, $13, $14, $15)
		`, trade.TenantID, posID, trade.AccountID, trade.Symbol, string(posSide),
			trade.Quantity, trade.Price, costBasis,
			trade.Leverage, trade.Margin, trade.LiquidationPrice, trade.Timestamp,
			trade.StopLoss, trade.TakeProfit, trade.Confidence)
		return err
	}
	if err != nil {
		return fmt.Errorf("query futures position: %w", err)
	}

	pos.Side = domain.PositionSide(side)
	pos.Status = domain.PositionStatus(status)

	// Determine if this trade increases or decreases the position
	isClosing := (pos.Side == domain.PositionSideLong && trade.Side == domain.SideSell) ||
		(pos.Side == domain.PositionSideShort && trade.Side == domain.SideBuy)

	if !isClosing {
		// Increasing position
		newCost := trade.Quantity * trade.Price
		totalQuantity := pos.Quantity + trade.Quantity
		totalCost := pos.CostBasis + newCost
		avgEntry := totalCost / totalQuantity

		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = $1, avg_entry_price = $2, cost_basis = $3,
				leverage = COALESCE($4, leverage),
				margin = COALESCE($5, margin),
				liquidation_price = COALESCE($6, liquidation_price),
				stop_loss = COALESCE($8, stop_loss),
				take_profit = COALESCE($9, take_profit)
			WHERE id = $7
		`, totalQuantity, avgEntry, totalCost,
			trade.Leverage, trade.Margin, trade.LiquidationPrice, pos.ID,
			trade.StopLoss, trade.TakeProfit)
		return err
	}

	// Closing (partially or fully)
	var realizedPnL float64
	if pos.Side == domain.PositionSideLong {
		realizedPnL = (trade.Price - pos.AvgEntryPrice) * trade.Quantity
	} else {
		realizedPnL = (pos.AvgEntryPrice - trade.Price) * trade.Quantity
	}
	// Subtract fees and funding fees
	realizedPnL -= trade.Fee
	if trade.FundingFee != nil {
		realizedPnL -= *trade.FundingFee
	}

	newQuantity := pos.Quantity - trade.Quantity
	if newQuantity <= 0 {
		// Fully closed
		exitPrice := trade.Price
		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = 0, realized_pnl = realized_pnl + $1, status = 'closed', closed_at = $2,
				exit_price = $4, exit_reason = $5
			WHERE id = $3
		`, realizedPnL, trade.Timestamp, pos.ID, exitPrice, trade.ExitReason)
		return err
	}

	// Partially closed
	remainingCost := pos.AvgEntryPrice * newQuantity
	_, err = tx.Exec(ctx, `
		UPDATE ledger_positions
		SET quantity = $1, cost_basis = $2, realized_pnl = realized_pnl + $3
		WHERE id = $4
	`, newQuantity, remainingCost, realizedPnL, pos.ID)
	return err
}

// InsertTradeAndUpdatePosition wraps InsertTrade + UpsertPosition in a single transaction.
// tenantID is used for all tenant-scoped sub-calls.
func (r *Repository) InsertTradeAndUpdatePosition(ctx context.Context, tenantID uuid.UUID, trade *domain.Trade) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	inserted, err := r.InsertTrade(ctx, tx, trade)
	if err != nil {
		return false, err
	}

	if inserted {
		if err := r.UpsertPosition(ctx, tx, trade); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}

	return inserted, nil
}

// ListPositions returns positions for a tenant/account with optional status filter.
func (r *Repository) ListPositions(ctx context.Context, tenantID uuid.UUID, accountID string, status string) ([]domain.Position, error) {
	var query string
	var args []interface{}

	if status == "" || status == "all" {
		query = `
			SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
				cost_basis, realized_pnl, leverage, margin, liquidation_price,
				status, opened_at, closed_at,
				exit_price, exit_reason, stop_loss, take_profit, confidence
			FROM ledger_positions
			WHERE tenant_id = $1 AND account_id = $2
			ORDER BY opened_at DESC
		`
		args = []interface{}{tenantID, accountID}
	} else {
		query = `
			SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
				cost_basis, realized_pnl, leverage, margin, liquidation_price,
				status, opened_at, closed_at,
				exit_price, exit_reason, stop_loss, take_profit, confidence
			FROM ledger_positions
			WHERE tenant_id = $1 AND account_id = $2 AND status = $3
			ORDER BY opened_at DESC
		`
		args = []interface{}{tenantID, accountID, status}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	defer rows.Close()

	var positions []domain.Position
	for rows.Next() {
		var p domain.Position
		var side, marketType, statusStr string
		err := rows.Scan(
			&p.ID, &p.AccountID, &p.Symbol, &marketType, &side,
			&p.Quantity, &p.AvgEntryPrice, &p.CostBasis, &p.RealizedPnL,
			&p.Leverage, &p.Margin, &p.LiquidationPrice,
			&statusStr, &p.OpenedAt, &p.ClosedAt,
			&p.ExitPrice, &p.ExitReason, &p.StopLoss, &p.TakeProfit, &p.Confidence,
		)
		if err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		p.Side = domain.PositionSide(side)
		p.MarketType = domain.MarketType(marketType)
		p.Status = domain.PositionStatus(statusStr)
		positions = append(positions, p)
	}

	if positions == nil {
		positions = []domain.Position{}
	}
	return positions, nil
}

// PortfolioSummary holds the portfolio summary for an account.
type PortfolioSummary struct {
	Positions        []domain.Position `json:"positions"`
	TotalRealizedPnL float64           `json:"total_realized_pnl"`
}

// GetPortfolioSummary returns open positions and aggregate realized P&L for a tenant/account.
func (r *Repository) GetPortfolioSummary(ctx context.Context, tenantID uuid.UUID, accountID string) (*PortfolioSummary, error) {
	positions, err := r.ListPositions(ctx, tenantID, accountID, "open")
	if err != nil {
		return nil, err
	}

	// Get total realized P&L across all positions (open and closed) for this tenant/account
	var totalPnL float64
	err = r.pool.QueryRow(ctx,
		"SELECT COALESCE(SUM(realized_pnl), 0) FROM ledger_positions WHERE tenant_id = $1 AND account_id = $2",
		tenantID, accountID,
	).Scan(&totalPnL)
	if err != nil {
		return nil, fmt.Errorf("get total pnl: %w", err)
	}

	return &PortfolioSummary{
		Positions:        positions,
		TotalRealizedPnL: totalPnL,
	}, nil
}

// RebuildPositions deletes all positions for a tenant/account and replays trades chronologically.
func (r *Repository) RebuildPositions(ctx context.Context, tenantID uuid.UUID, accountID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete all positions for this tenant/account
	_, err = tx.Exec(ctx, "DELETE FROM ledger_positions WHERE tenant_id = $1 AND account_id = $2", tenantID, accountID)
	if err != nil {
		return fmt.Errorf("delete positions: %w", err)
	}

	// Collect all trades first (must close rows before using tx for upserts)
	trades, err := r.TradesForRebuild(ctx, tx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("load trades for rebuild: %w", err)
	}

	for i := range trades {
		if err := r.UpsertPosition(ctx, tx, &trades[i]); err != nil {
			return fmt.Errorf("upsert position during rebuild: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// TradesForRebuild returns all trades for a tenant/account in chronological order.
func (r *Repository) TradesForRebuild(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, accountID string) ([]domain.Trade, error) {
	rows, err := tx.Query(ctx, `
		SELECT trade_id, account_id, symbol, side, quantity, price, fee, fee_currency,
			market_type, timestamp, ingested_at, cost_basis, realized_pnl,
			leverage, margin, liquidation_price, funding_fee,
			strategy, entry_reason, exit_reason, confidence, stop_loss, take_profit
		FROM ledger_trades
		WHERE tenant_id = $1 AND account_id = $2
		ORDER BY timestamp ASC, trade_id ASC
	`, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer rows.Close()

	var trades []domain.Trade
	for rows.Next() {
		var t domain.Trade
		var sideStr, mtStr string
		err := rows.Scan(
			&t.TradeID, &t.AccountID, &t.Symbol, &sideStr, &t.Quantity, &t.Price,
			&t.Fee, &t.FeeCurrency, &mtStr, &t.Timestamp, &t.IngestedAt,
			&t.CostBasis, &t.RealizedPnL,
			&t.Leverage, &t.Margin, &t.LiquidationPrice, &t.FundingFee,
			&t.Strategy, &t.EntryReason, &t.ExitReason, &t.Confidence, &t.StopLoss, &t.TakeProfit,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		t.Side = domain.Side(sideStr)
		t.MarketType = domain.MarketType(mtStr)
		t.TenantID = tenantID
		trades = append(trades, t)
	}
	return trades, nil
}

// CostBasisForTrade calculates the appropriate cost_basis and realized_pnl for a trade.
func CostBasisForTrade(trade *domain.Trade, avgEntryPrice float64) {
	if trade.Side == domain.SideBuy {
		trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
		trade.RealizedPnL = 0
	} else {
		trade.CostBasis = avgEntryPrice * trade.Quantity
		trade.RealizedPnL = (trade.Price-avgEntryPrice)*trade.Quantity - trade.Fee
	}
}

// GetAvgEntryPrice returns the average entry price for an open position, or 0 if none exists.
func (r *Repository) GetAvgEntryPrice(ctx context.Context, tenantID uuid.UUID, accountID, symbol string, marketType domain.MarketType) (float64, error) {
	var avgPrice float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(avg_entry_price, 0) FROM ledger_positions
		WHERE tenant_id = $1 AND account_id = $2 AND symbol = $3 AND market_type = $4 AND status = 'open'
	`, tenantID, accountID, symbol, string(marketType)).Scan(&avgPrice)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return avgPrice, err
}
