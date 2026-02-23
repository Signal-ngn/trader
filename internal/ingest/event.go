package ingest

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// TradeEvent is the JSON structure for trade events received via NATS.
type TradeEvent struct {
	TenantID    string  `json:"tenant_id"`
	TradeID     string  `json:"trade_id"`
	AccountID   string  `json:"account_id"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Quantity    float64 `json:"quantity"`
	Price       float64 `json:"price"`
	Fee         float64 `json:"fee"`
	FeeCurrency string  `json:"fee_currency"`
	MarketType  string  `json:"market_type"`
	Timestamp   string  `json:"timestamp"`

	// Futures-specific fields (optional)
	Leverage         *int     `json:"leverage,omitempty"`
	Margin           *float64 `json:"margin,omitempty"`
	LiquidationPrice *float64 `json:"liquidation_price,omitempty"`
	FundingFee       *float64 `json:"funding_fee,omitempty"`

	// Strategy metadata fields (optional)
	Strategy    *string  `json:"strategy,omitempty"`
	EntryReason *string  `json:"entry_reason,omitempty"`
	ExitReason  *string  `json:"exit_reason,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
	StopLoss    *float64 `json:"stop_loss,omitempty"`
	TakeProfit  *float64 `json:"take_profit,omitempty"`
}

// Validate checks that the trade event has all required fields and valid values.
func (e *TradeEvent) Validate() error {
	if e.TenantID == "" {
		return fmt.Errorf("missing required field: tenant_id")
	}
	if _, err := uuid.Parse(e.TenantID); err != nil {
		return fmt.Errorf("invalid tenant_id: must be a valid UUID, got %q", e.TenantID)
	}
	if e.TradeID == "" {
		return fmt.Errorf("missing required field: trade_id")
	}
	if e.AccountID == "" {
		return fmt.Errorf("missing required field: account_id")
	}
	if e.Symbol == "" {
		return fmt.Errorf("missing required field: symbol")
	}
	if e.Side != "buy" && e.Side != "sell" {
		return fmt.Errorf("invalid side: %q (must be buy or sell)", e.Side)
	}
	if e.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive, got %f", e.Quantity)
	}
	if e.Price <= 0 {
		return fmt.Errorf("price must be positive, got %f", e.Price)
	}
	if e.FeeCurrency == "" {
		return fmt.Errorf("missing required field: fee_currency")
	}
	if e.Timestamp == "" {
		return fmt.Errorf("missing required field: timestamp")
	}
	if e.MarketType != "spot" && e.MarketType != "futures" {
		return fmt.Errorf("invalid market_type: %q (must be spot or futures)", e.MarketType)
	}

	// Validate timestamp is parseable
	if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	return nil
}

// ToDomain converts a TradeEvent to a domain Trade.
func (e *TradeEvent) ToDomain() (*domain.Trade, error) {
	tenantID, err := uuid.Parse(e.TenantID)
	if err != nil {
		return nil, fmt.Errorf("parse tenant_id: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("parse timestamp: %w", err)
	}

	trade := &domain.Trade{
		TenantID:         tenantID,
		TradeID:          e.TradeID,
		AccountID:        e.AccountID,
		Symbol:           e.Symbol,
		Side:             domain.Side(e.Side),
		Quantity:         e.Quantity,
		Price:            e.Price,
		Fee:              e.Fee,
		FeeCurrency:      e.FeeCurrency,
		MarketType:       domain.MarketType(e.MarketType),
		Timestamp:        ts,
		IngestedAt:       time.Now(),
		Leverage:         e.Leverage,
		Margin:           e.Margin,
		LiquidationPrice: e.LiquidationPrice,
		FundingFee:       e.FundingFee,
		Strategy:         e.Strategy,
		EntryReason:      e.EntryReason,
		ExitReason:       e.ExitReason,
		Confidence:       e.Confidence,
		StopLoss:         e.StopLoss,
		TakeProfit:       e.TakeProfit,
	}

	// Calculate cost basis
	if trade.Side == domain.SideBuy {
		trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
	}
	// For sells, cost basis is set during position update (needs avg entry price)

	return trade, nil
}
