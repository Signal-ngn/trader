package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// InsertTrade inserts a trade with ON CONFLICT DO NOTHING. Returns true if inserted.
func (r *Repository) InsertTrade(ctx context.Context, tx pgx.Tx, trade *domain.Trade) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO ledger_trades (
			tenant_id, trade_id, account_id, symbol, side, quantity, price, fee, fee_currency,
			market_type, timestamp, ingested_at, cost_basis, realized_pnl,
			leverage, margin, liquidation_price, funding_fee,
			strategy, entry_reason, exit_reason, confidence, stop_loss, take_profit
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		ON CONFLICT (trade_id) DO NOTHING
	`,
		trade.TenantID, trade.TradeID, trade.AccountID, trade.Symbol, string(trade.Side),
		trade.Quantity, trade.Price, trade.Fee, trade.FeeCurrency,
		string(trade.MarketType), trade.Timestamp, trade.IngestedAt,
		trade.CostBasis, trade.RealizedPnL,
		trade.Leverage, trade.Margin, trade.LiquidationPrice, trade.FundingFee,
		trade.Strategy, trade.EntryReason, trade.ExitReason, trade.Confidence, trade.StopLoss, trade.TakeProfit,
	)
	if err != nil {
		return false, fmt.Errorf("insert trade: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// TradeFilter defines filters for listing trades.
type TradeFilter struct {
	Symbol     string
	Side       string
	MarketType string
	Start      *time.Time
	End        *time.Time
	Cursor     string
	Limit      int
}

// TradeListResult contains paginated trade results.
type TradeListResult struct {
	Trades     []domain.Trade `json:"trades"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// ListTrades returns trades for a tenant/account with filters and cursor-based pagination.
func (r *Repository) ListTrades(ctx context.Context, tenantID uuid.UUID, accountID string, filter TradeFilter) (*TradeListResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
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

	if filter.Symbol != "" {
		conditions = append(conditions, fmt.Sprintf("symbol = $%d", argIdx))
		args = append(args, filter.Symbol)
		argIdx++
	}
	if filter.Side != "" {
		conditions = append(conditions, fmt.Sprintf("side = $%d", argIdx))
		args = append(args, filter.Side)
		argIdx++
	}
	if filter.MarketType != "" {
		conditions = append(conditions, fmt.Sprintf("market_type = $%d", argIdx))
		args = append(args, filter.MarketType)
		argIdx++
	}
	if filter.Start != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argIdx))
		args = append(args, *filter.Start)
		argIdx++
	}
	if filter.End != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argIdx))
		args = append(args, *filter.End)
		argIdx++
	}

	// Cursor-based pagination: cursor is base64-encoded "timestamp|trade_id"
	if filter.Cursor != "" {
		cursorTS, cursorID, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf(
			"(timestamp, trade_id) < ($%d, $%d)", argIdx, argIdx+1,
		))
		args = append(args, cursorTS, cursorID)
		argIdx += 2
	}

	where := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT trade_id, account_id, symbol, side, quantity, price, fee, fee_currency,
			market_type, timestamp, ingested_at, cost_basis, realized_pnl,
			leverage, margin, liquidation_price, funding_fee,
			strategy, entry_reason, exit_reason, confidence, stop_loss, take_profit
		FROM ledger_trades
		WHERE %s
		ORDER BY timestamp DESC, trade_id DESC
		LIMIT $%d
	`, where, argIdx)
	args = append(args, filter.Limit+1) // fetch one extra to check if there's a next page

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list trades: %w", err)
	}
	defer rows.Close()

	var trades []domain.Trade
	for rows.Next() {
		var t domain.Trade
		var side, marketType string
		err := rows.Scan(
			&t.TradeID, &t.AccountID, &t.Symbol, &side, &t.Quantity, &t.Price,
			&t.Fee, &t.FeeCurrency, &marketType, &t.Timestamp, &t.IngestedAt,
			&t.CostBasis, &t.RealizedPnL,
			&t.Leverage, &t.Margin, &t.LiquidationPrice, &t.FundingFee,
			&t.Strategy, &t.EntryReason, &t.ExitReason, &t.Confidence, &t.StopLoss, &t.TakeProfit,
		)
		if err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		t.Side = domain.Side(side)
		t.MarketType = domain.MarketType(marketType)
		t.TenantID = tenantID
		trades = append(trades, t)
	}

	result := &TradeListResult{}
	if len(trades) > filter.Limit {
		trades = trades[:filter.Limit]
		last := trades[len(trades)-1]
		result.NextCursor = encodeCursor(last.Timestamp, last.TradeID)
	}
	result.Trades = trades
	if result.Trades == nil {
		result.Trades = []domain.Trade{}
	}

	return result, nil
}

func encodeCursor(ts time.Time, id string) string {
	raw := fmt.Sprintf("%s|%s", ts.Format(time.RFC3339Nano), id)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode base64: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse timestamp: %w", err)
	}
	return ts, parts[1], nil
}
