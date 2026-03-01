package domain

import (
	"time"

	"github.com/google/uuid"
)

// AccountType represents the type of trading account.
type AccountType string

const (
	AccountTypeLive  AccountType = "live"
	AccountTypePaper AccountType = "paper"
)

// Side represents the trade/position direction.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// PositionSide represents the direction of a position.
type PositionSide string

const (
	PositionSideLong  PositionSide = "long"
	PositionSideShort PositionSide = "short"
)

// MarketType represents the market type of a trade.
type MarketType string

const (
	MarketTypeSpot    MarketType = "spot"
	MarketTypeFutures MarketType = "futures"
)

// PositionStatus represents the status of a position.
type PositionStatus string

const (
	PositionStatusOpen   PositionStatus = "open"
	PositionStatusClosed PositionStatus = "closed"
)

// OrderType represents the type of order.
type OrderType string

const (
	OrderTypeMarket OrderType = "market"
	OrderTypeLimit  OrderType = "limit"
)

// OrderStatus represents the status of an order.
type OrderStatus string

const (
	OrderStatusOpen            OrderStatus = "open"
	OrderStatusFilled          OrderStatus = "filled"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusCancelled       OrderStatus = "cancelled"
)

// Account represents a trading account.
type Account struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      AccountType `json:"type"`
	CreatedAt time.Time   `json:"created_at"`
}

// Trade represents a single trade execution.
type Trade struct {
	TenantID    uuid.UUID  `json:"tenant_id"`
	TradeID     string     `json:"trade_id"`
	AccountID   string     `json:"account_id"`
	Symbol      string     `json:"symbol"`
	Side        Side       `json:"side"`
	Quantity    float64    `json:"quantity"`
	Price       float64    `json:"price"`
	Fee         float64    `json:"fee"`
	FeeCurrency string     `json:"fee_currency"`
	MarketType  MarketType `json:"market_type"`
	Timestamp   time.Time  `json:"timestamp"`
	IngestedAt  time.Time  `json:"ingested_at"`
	CostBasis   float64    `json:"cost_basis"`
	RealizedPnL float64    `json:"realized_pnl"`

	// Futures-specific fields (nullable)
	Leverage         *int     `json:"leverage,omitempty"`
	Margin           *float64 `json:"margin,omitempty"`
	LiquidationPrice *float64 `json:"liquidation_price,omitempty"`
	FundingFee       *float64 `json:"funding_fee,omitempty"`

	// Strategy metadata fields (nullable)
	Strategy    *string  `json:"strategy,omitempty"`
	EntryReason *string  `json:"entry_reason,omitempty"`
	ExitReason  *string  `json:"exit_reason,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
	StopLoss    *float64 `json:"stop_loss,omitempty"`
	TakeProfit  *float64 `json:"take_profit,omitempty"`
}

// Position represents a tracked position for a symbol.
type Position struct {
	ID               string         `json:"id"`
	AccountID        string         `json:"account_id"`
	Symbol           string         `json:"symbol"`
	MarketType       MarketType     `json:"market_type"`
	Side             PositionSide   `json:"side"`
	Quantity         float64        `json:"quantity"`
	AvgEntryPrice    float64        `json:"avg_entry_price"`
	CostBasis        float64        `json:"cost_basis"`
	RealizedPnL      float64        `json:"realized_pnl"`
	Leverage         *int           `json:"leverage,omitempty"`
	Margin           *float64       `json:"margin,omitempty"`
	LiquidationPrice *float64       `json:"liquidation_price,omitempty"`
	Status           PositionStatus `json:"status"`
	OpenedAt         time.Time      `json:"opened_at"`
	ClosedAt         *time.Time     `json:"closed_at,omitempty"`

	// Strategy metadata fields (nullable)
	ExitPrice  *float64 `json:"exit_price,omitempty"`
	ExitReason *string  `json:"exit_reason,omitempty"`
	StopLoss   *float64 `json:"stop_loss,omitempty"`
	TakeProfit *float64 `json:"take_profit,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// InferAccountType returns the account type based on the account ID.
func InferAccountType(accountID string) AccountType {
	if accountID == "paper" {
		return AccountTypePaper
	}
	return AccountTypeLive
}

// AccountBalance represents the current cash balance for a trading account.
type AccountBalance struct {
	AccountID string  `json:"account_id"`
	Currency  string  `json:"currency"`
	Amount    float64 `json:"amount"`
}

// Order represents a trading order.
type Order struct {
	OrderID      string      `json:"order_id"`
	AccountID    string      `json:"account_id"`
	Symbol       string      `json:"symbol"`
	Side         Side        `json:"side"`
	OrderType    OrderType   `json:"order_type"`
	RequestedQty float64     `json:"requested_qty"`
	FilledQty    float64     `json:"filled_qty"`
	AvgFillPrice float64     `json:"avg_fill_price"`
	Status       OrderStatus `json:"status"`
	MarketType   MarketType  `json:"market_type"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}
