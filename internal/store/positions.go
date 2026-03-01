package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// PositionFilter defines filters for listing positions.
type PositionFilter struct {
	Status string // "open", "closed", "all" (default: "open")
	Limit  int    // 0 means use default (50); max 200
	Cursor string
}

// PositionListResult contains paginated position results.
type PositionListResult struct {
	Positions  []domain.Position `json:"positions"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// UpsertPosition creates or updates a position based on the trade, and adjusts
// the account balance when a balance row exists. Must be called within a transaction.
func (r *Repository) UpsertPosition(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
	if trade.MarketType == domain.MarketTypeSpot {
		return r.upsertSpotPosition(ctx, tx, trade)
	}
	return r.upsertFuturesPosition(ctx, tx, trade)
}

// upsertPositionForRebuild creates or updates a position based on the trade
// WITHOUT adjusting the account balance. Used exclusively by RebuildPositions
// so that position reconstruction does not disturb the current balance.
func (r *Repository) upsertPositionForRebuild(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
	if trade.MarketType == domain.MarketTypeSpot {
		return r.upsertSpotPositionNoBalance(ctx, tx, trade)
	}
	return r.upsertFuturesPositionNoBalance(ctx, tx, trade)
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
			if err != nil {
				return err
			}
			// Deduct cost from balance (no-op if no balance row exists)
			return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", -costBasis)
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
		if err != nil {
			return err
		}
		// Deduct additional cost from balance (no-op if no balance row exists)
		return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", -newCostBasis)
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
		if err != nil {
			return err
		}
		// Credit realised P&L to balance (no-op if no balance row exists)
		return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", realizedPnL)
	}

	// Partial close — reduce quantity, keep proportional cost basis
	remainingCostBasis := pos.AvgEntryPrice * newQuantity
	_, err = tx.Exec(ctx, `
		UPDATE ledger_positions
		SET quantity = $1, cost_basis = $2, realized_pnl = realized_pnl + $3
		WHERE id = $4
	`, newQuantity, remainingCostBasis, realizedPnL, pos.ID)
	if err != nil {
		return err
	}
	// Credit realised P&L to balance (no-op if no balance row exists)
	return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", realizedPnL)
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
		if err != nil {
			return err
		}
		// Deduct margin from balance.
		// Priority: trade.Margin → costBasis/leverage → skip if neither available.
		var marginDelta float64
		if trade.Margin != nil {
			marginDelta = *trade.Margin
		} else if trade.Leverage != nil && *trade.Leverage > 0 {
			marginDelta = costBasis / float64(*trade.Leverage)
		} else {
			return nil // cannot determine margin — skip balance adjustment
		}
		return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", -marginDelta)
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
		if err != nil {
			return err
		}
		// Deduct additional margin for the added quantity (no-op if no balance row)
		var marginDelta float64
		if trade.Margin != nil {
			marginDelta = *trade.Margin
		} else {
			lev := pos.Leverage
			if lev == nil {
				lev = trade.Leverage
			}
			if lev != nil && *lev > 0 {
				marginDelta = newCost / float64(*lev)
			} else {
				return nil
			}
		}
		return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", -marginDelta)
	}

	// Closing (partially or fully)
	// P&L is in margin (account-impact) terms, not full notional.
	// scale = 1/leverage.
	// Priority: stored position leverage → closing trade leverage → default 2x.
	// Default 2x because all current trading configs use 2x leverage and the
	// bot inconsistently omits the leverage field on some symbols (e.g. AVAX).
	const defaultLeverage = 2
	levVal := defaultLeverage
	lev := pos.Leverage
	if lev == nil {
		lev = trade.Leverage
	}
	if lev != nil && *lev > 0 {
		levVal = *lev
	}
	scale := 1.0 / float64(levVal)

	var realizedPnL float64
	if pos.Side == domain.PositionSideLong {
		realizedPnL = (trade.Price - pos.AvgEntryPrice) * trade.Quantity * scale
	} else {
		realizedPnL = (pos.AvgEntryPrice - trade.Price) * trade.Quantity * scale
	}
	// Subtract fees and funding fees (already in account terms — not notional)
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
		if err != nil {
			return err
		}
		// Credit realised P&L to balance (no-op if no balance row exists)
		return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", realizedPnL)
	}

	// Partially closed
	remainingCost := pos.AvgEntryPrice * newQuantity
	_, err = tx.Exec(ctx, `
		UPDATE ledger_positions
		SET quantity = $1, cost_basis = $2, realized_pnl = realized_pnl + $3
		WHERE id = $4
	`, newQuantity, remainingCost, realizedPnL, pos.ID)
	if err != nil {
		return err
	}
	// Credit realised P&L to balance (no-op if no balance row exists)
	return r.AdjustBalance(ctx, tx, trade.TenantID, trade.AccountID, "USD", realizedPnL)
}

// upsertSpotPositionNoBalance is identical to upsertSpotPosition but skips balance adjustment.
// Used by position rebuild so that replaying trades does not alter the current balance.
func (r *Repository) upsertSpotPositionNoBalance(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
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
		return nil
	}
	if err != nil {
		return fmt.Errorf("query position: %w", err)
	}
	pos.Side = domain.PositionSide(side)
	pos.Status = domain.PositionStatus(status)
	if trade.Side == domain.SideBuy {
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
	realizedPnL := (trade.Price-pos.AvgEntryPrice)*trade.Quantity - trade.Fee
	newQuantity := pos.Quantity - trade.Quantity
	if newQuantity <= 0 {
		exitPrice := trade.Price
		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = 0, realized_pnl = realized_pnl + $1, status = 'closed', closed_at = $2,
				exit_price = $4, exit_reason = $5
			WHERE id = $3
		`, realizedPnL, trade.Timestamp, pos.ID, exitPrice, trade.ExitReason)
		return err
	}
	remainingCostBasis := pos.AvgEntryPrice * newQuantity
	_, err = tx.Exec(ctx, `
		UPDATE ledger_positions
		SET quantity = $1, cost_basis = $2, realized_pnl = realized_pnl + $3
		WHERE id = $4
	`, newQuantity, remainingCostBasis, realizedPnL, pos.ID)
	return err
}

// upsertFuturesPositionNoBalance is identical to upsertFuturesPosition but skips balance adjustment.
func (r *Repository) upsertFuturesPositionNoBalance(ctx context.Context, tx pgx.Tx, trade *domain.Trade) error {
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
	isClosing := (pos.Side == domain.PositionSideLong && trade.Side == domain.SideSell) ||
		(pos.Side == domain.PositionSideShort && trade.Side == domain.SideBuy)
	if !isClosing {
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
	const defaultLeverage = 2
	levVal := defaultLeverage
	lev := pos.Leverage
	if lev == nil {
		lev = trade.Leverage
	}
	if lev != nil && *lev > 0 {
		levVal = *lev
	}
	scale := 1.0 / float64(levVal)
	var realizedPnL float64
	if pos.Side == domain.PositionSideLong {
		realizedPnL = (trade.Price - pos.AvgEntryPrice) * trade.Quantity * scale
	} else {
		realizedPnL = (pos.AvgEntryPrice - trade.Price) * trade.Quantity * scale
	}
	realizedPnL -= trade.Fee
	if trade.FundingFee != nil {
		realizedPnL -= *trade.FundingFee
	}
	newQuantity := pos.Quantity - trade.Quantity
	if newQuantity <= 0 {
		exitPrice := trade.Price
		_, err = tx.Exec(ctx, `
			UPDATE ledger_positions
			SET quantity = 0, realized_pnl = realized_pnl + $1, status = 'closed', closed_at = $2,
				exit_price = $4, exit_reason = $5
			WHERE id = $3
		`, realizedPnL, trade.Timestamp, pos.ID, exitPrice, trade.ExitReason)
		return err
	}
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

// ListPositions returns positions for a tenant/account with optional status filter and cursor pagination.
func (r *Repository) ListPositions(ctx context.Context, tenantID uuid.UUID, accountID string, filter PositionFilter) (*PositionListResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	status := filter.Status
	if status == "" {
		status = "open"
	}

	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
	args = append(args, tenantID)
	argIdx++

	conditions = append(conditions, fmt.Sprintf("account_id = $%d", argIdx))
	args = append(args, accountID)
	argIdx++

	if status != "all" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}

	// Cursor-based pagination: cursor encodes (opened_at, id)
	if filter.Cursor != "" {
		cursorTS, cursorID, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf(
			"(opened_at, id) < ($%d, $%d)", argIdx, argIdx+1,
		))
		args = append(args, cursorTS, cursorID)
		argIdx += 2
	}

	where := strings.Join(conditions, " AND ")
	query := fmt.Sprintf(`
		SELECT id, account_id, symbol, market_type, side, quantity, avg_entry_price,
			cost_basis, realized_pnl, leverage, margin, liquidation_price,
			status, opened_at, closed_at,
			exit_price, exit_reason, stop_loss, take_profit, confidence
		FROM ledger_positions
		WHERE %s
		ORDER BY opened_at DESC, id DESC
		LIMIT $%d
	`, where, argIdx)
	args = append(args, filter.Limit+1) // fetch one extra to detect next page

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

	result := &PositionListResult{}
	if len(positions) > filter.Limit {
		positions = positions[:filter.Limit]
		last := positions[len(positions)-1]
		result.NextCursor = encodeCursor(last.OpenedAt, last.ID)
	}
	result.Positions = positions
	if result.Positions == nil {
		result.Positions = []domain.Position{}
	}
	return result, nil
}

// PortfolioSummary holds the portfolio summary for an account.
type PortfolioSummary struct {
	Positions        []domain.Position `json:"positions"`
	TotalRealizedPnL float64           `json:"total_realized_pnl"`
	Balance          *float64          `json:"balance,omitempty"`
}

// GetPortfolioSummary returns open positions, aggregate realized P&L, and current balance for a tenant/account.
func (r *Repository) GetPortfolioSummary(ctx context.Context, tenantID uuid.UUID, accountID string) (*PortfolioSummary, error) {
	result, err := r.ListPositions(ctx, tenantID, accountID, PositionFilter{Status: "open", Limit: 200})
	if err != nil {
		return nil, err
	}
	positions := result.Positions

	// Get total realized P&L across all positions (open and closed) for this tenant/account
	var totalPnL float64
	err = r.pool.QueryRow(ctx,
		"SELECT COALESCE(SUM(realized_pnl), 0) FROM ledger_positions WHERE tenant_id = $1 AND account_id = $2",
		tenantID, accountID,
	).Scan(&totalPnL)
	if err != nil {
		return nil, fmt.Errorf("get total pnl: %w", err)
	}

	// Attach balance when set (omitted from response when nil)
	balance, err := r.GetAccountBalance(ctx, tenantID, accountID, "USD")
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	return &PortfolioSummary{
		Positions:        positions,
		TotalRealizedPnL: totalPnL,
		Balance:          balance,
	}, nil
}

// RebuildAllPositions rebuilds positions for every (tenant, account) pair that
// has trades. This is used after schema migrations that change P&L semantics
// (e.g. switching from notional to margin-adjusted P&L for futures).
func (r *Repository) RebuildAllPositions(ctx context.Context) error {
	// Collect distinct (tenant_id, account_id) pairs from trades.
	rows, err := r.pool.Query(ctx,
		"SELECT DISTINCT tenant_id, account_id FROM ledger_trades ORDER BY tenant_id, account_id",
	)
	if err != nil {
		return fmt.Errorf("list tenant/account pairs: %w", err)
	}
	type pair struct {
		tenantID  uuid.UUID
		accountID string
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.tenantID, &p.accountID); err != nil {
			rows.Close()
			return fmt.Errorf("scan pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	rows.Close()

	for _, p := range pairs {
		if err := r.RebuildPositions(ctx, p.tenantID, p.accountID); err != nil {
			return fmt.Errorf("rebuild positions for tenant=%s account=%s: %w",
				p.tenantID, p.accountID, err)
		}
	}
	return nil
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
		if err := r.upsertPositionForRebuild(ctx, tx, &trades[i]); err != nil {
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
