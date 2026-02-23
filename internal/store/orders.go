package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// UpsertOrder inserts or updates an order.
func (r *Repository) UpsertOrder(ctx context.Context, order *domain.Order) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ledger_orders (order_id, account_id, symbol, side, order_type,
			requested_qty, filled_qty, avg_fill_price, status, market_type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (order_id) DO UPDATE SET
			filled_qty = EXCLUDED.filled_qty,
			avg_fill_price = EXCLUDED.avg_fill_price,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`,
		order.OrderID, order.AccountID, order.Symbol, string(order.Side),
		string(order.OrderType), order.RequestedQty, order.FilledQty,
		order.AvgFillPrice, string(order.Status), string(order.MarketType),
		order.CreatedAt, order.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert order: %w", err)
	}
	return nil
}

// OrderFilter defines filters for listing orders.
type OrderFilter struct {
	Status string
	Symbol string
	Cursor string
	Limit  int
}

// OrderListResult contains paginated order results.
type OrderListResult struct {
	Orders     []domain.Order `json:"orders"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// ListOrders returns orders for a tenant/account with filters and cursor-based pagination.
func (r *Repository) ListOrders(ctx context.Context, tenantID uuid.UUID, accountID string, filter OrderFilter) (*OrderListResult, error) {
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

	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Symbol != "" {
		conditions = append(conditions, fmt.Sprintf("symbol = $%d", argIdx))
		args = append(args, filter.Symbol)
		argIdx++
	}
	if filter.Cursor != "" {
		cursorTS, cursorID, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf(
			"(created_at, order_id) < ($%d, $%d)", argIdx, argIdx+1,
		))
		args = append(args, cursorTS, cursorID)
		argIdx += 2
	}

	where := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT order_id, account_id, symbol, side, order_type,
			requested_qty, filled_qty, avg_fill_price, status, market_type,
			created_at, updated_at
		FROM ledger_orders
		WHERE %s
		ORDER BY created_at DESC, order_id DESC
		LIMIT $%d
	`, where, argIdx)
	args = append(args, filter.Limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		var side, orderType, status, marketType string
		err := rows.Scan(
			&o.OrderID, &o.AccountID, &o.Symbol, &side, &orderType,
			&o.RequestedQty, &o.FilledQty, &o.AvgFillPrice, &status, &marketType,
			&o.CreatedAt, &o.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o.Side = domain.Side(side)
		o.OrderType = domain.OrderType(orderType)
		o.Status = domain.OrderStatus(status)
		o.MarketType = domain.MarketType(marketType)
		orders = append(orders, o)
	}

	result := &OrderListResult{}
	if len(orders) > filter.Limit {
		orders = orders[:filter.Limit]
		last := orders[len(orders)-1]
		result.NextCursor = encodeCursor(last.CreatedAt, last.OrderID)
	}
	result.Orders = orders
	if result.Orders == nil {
		result.Orders = []domain.Order{}
	}

	return result, nil
}
