package ingest

import (
	"testing"
)

func TestTradeEventValidation_Valid(t *testing.T) {
	event := TradeEvent{
		TradeID:     "t-001",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid event, got error: %v", err)
	}
}

func TestTradeEventValidation_ValidFutures(t *testing.T) {
	leverage := 10
	margin := 5000.0
	liqPrice := 45000.0
	event := TradeEvent{
		TradeID:          "t-002",
		AccountID:        "live",
		Symbol:           "BTC-USD",
		Side:             "buy",
		Quantity:         1.0,
		Price:            50000,
		Fee:              50,
		FeeCurrency:      "USD",
		MarketType:       "futures",
		Timestamp:        "2025-01-15T10:00:00Z",
		Leverage:         &leverage,
		Margin:           &margin,
		LiquidationPrice: &liqPrice,
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid futures event, got error: %v", err)
	}
}

func TestTradeEventValidation_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		event TradeEvent
		want  string
	}{
		{
			name:  "missing trade_id",
			event: TradeEvent{AccountID: "live", Symbol: "BTC-USD", Side: "buy", Quantity: 1, Price: 50000, FeeCurrency: "USD", MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "missing required field: trade_id",
		},
		{
			name:  "missing account_id",
			event: TradeEvent{TradeID: "t-1", Symbol: "BTC-USD", Side: "buy", Quantity: 1, Price: 50000, FeeCurrency: "USD", MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "missing required field: account_id",
		},
		{
			name:  "missing symbol",
			event: TradeEvent{TradeID: "t-1", AccountID: "live", Side: "buy", Quantity: 1, Price: 50000, FeeCurrency: "USD", MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "missing required field: symbol",
		},
		{
			name:  "missing fee_currency",
			event: TradeEvent{TradeID: "t-1", AccountID: "live", Symbol: "BTC-USD", Side: "buy", Quantity: 1, Price: 50000, MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "missing required field: fee_currency",
		},
		{
			name:  "missing timestamp",
			event: TradeEvent{TradeID: "t-1", AccountID: "live", Symbol: "BTC-USD", Side: "buy", Quantity: 1, Price: 50000, FeeCurrency: "USD", MarketType: "spot"},
			want:  "missing required field: timestamp",
		},
		{
			name:  "zero quantity",
			event: TradeEvent{TradeID: "t-1", AccountID: "live", Symbol: "BTC-USD", Side: "buy", Quantity: 0, Price: 50000, FeeCurrency: "USD", MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "quantity must be positive",
		},
		{
			name:  "zero price",
			event: TradeEvent{TradeID: "t-1", AccountID: "live", Symbol: "BTC-USD", Side: "buy", Quantity: 1, Price: 0, FeeCurrency: "USD", MarketType: "spot", Timestamp: "2025-01-15T10:00:00Z"},
			want:  "price must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.want && !contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestTradeEventValidation_InvalidMarketType(t *testing.T) {
	event := TradeEvent{
		TradeID:     "t-001",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "options",
		Timestamp:   "2025-01-15T10:00:00Z",
	}

	err := event.Validate()
	if err == nil {
		t.Fatal("expected error for invalid market type")
	}
	if !contains(err.Error(), "invalid market_type") {
		t.Fatalf("expected market_type error, got: %v", err)
	}
}

func TestTradeEventValidation_InvalidSide(t *testing.T) {
	event := TradeEvent{
		TradeID:     "t-001",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "hold",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
	}

	err := event.Validate()
	if err == nil {
		t.Fatal("expected error for invalid side")
	}
	if !contains(err.Error(), "invalid side") {
		t.Fatalf("expected side error, got: %v", err)
	}
}

func TestTradeEventToDomain(t *testing.T) {
	event := TradeEvent{
		TradeID:     "t-001",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
	}

	trade, err := event.ToDomain()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.TradeID != "t-001" {
		t.Errorf("expected trade_id t-001, got %s", trade.TradeID)
	}
	if trade.CostBasis != 25025 { // 0.5 * 50000 + 25
		t.Errorf("expected cost_basis 25025, got %f", trade.CostBasis)
	}
}

func TestTradeEventToDomain_WithMetadata(t *testing.T) {
	strategy := "macd-rsi-v2"
	entryReason := "MACD bullish crossover, RSI 42"
	confidence := 0.85
	stopLoss := 48000.0
	takeProfit := 55000.0

	event := TradeEvent{
		TradeID:     "t-003",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
		Strategy:    &strategy,
		EntryReason: &entryReason,
		Confidence:  &confidence,
		StopLoss:    &stopLoss,
		TakeProfit:  &takeProfit,
	}

	trade, err := event.ToDomain()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Strategy == nil || *trade.Strategy != "macd-rsi-v2" {
		t.Errorf("expected strategy macd-rsi-v2, got %v", trade.Strategy)
	}
	if trade.EntryReason == nil || *trade.EntryReason != "MACD bullish crossover, RSI 42" {
		t.Errorf("expected entry_reason, got %v", trade.EntryReason)
	}
	if trade.Confidence == nil || *trade.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %v", trade.Confidence)
	}
	if trade.StopLoss == nil || *trade.StopLoss != 48000.0 {
		t.Errorf("expected stop_loss 48000, got %v", trade.StopLoss)
	}
	if trade.TakeProfit == nil || *trade.TakeProfit != 55000.0 {
		t.Errorf("expected take_profit 55000, got %v", trade.TakeProfit)
	}
}

func TestTradeEventToDomain_WithoutMetadata(t *testing.T) {
	event := TradeEvent{
		TradeID:     "t-004",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
	}

	trade, err := event.ToDomain()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.Strategy != nil {
		t.Errorf("expected nil strategy, got %v", trade.Strategy)
	}
	if trade.EntryReason != nil {
		t.Errorf("expected nil entry_reason, got %v", trade.EntryReason)
	}
	if trade.ExitReason != nil {
		t.Errorf("expected nil exit_reason, got %v", trade.ExitReason)
	}
	if trade.Confidence != nil {
		t.Errorf("expected nil confidence, got %v", trade.Confidence)
	}
	if trade.StopLoss != nil {
		t.Errorf("expected nil stop_loss, got %v", trade.StopLoss)
	}
	if trade.TakeProfit != nil {
		t.Errorf("expected nil take_profit, got %v", trade.TakeProfit)
	}
}

func TestTradeEventValidation_WithMetadata(t *testing.T) {
	strategy := "macd-rsi-v2"
	confidence := 0.85
	event := TradeEvent{
		TradeID:     "t-005",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Quantity:    0.5,
		Price:       50000,
		Fee:         25,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T10:00:00Z",
		Strategy:    &strategy,
		Confidence:  &confidence,
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid event with metadata, got error: %v", err)
	}
}

func TestTradeEventToDomain_ExitReason(t *testing.T) {
	exitReason := "stop loss hit"
	event := TradeEvent{
		TradeID:     "t-006",
		AccountID:   "live",
		Symbol:      "BTC-USD",
		Side:        "sell",
		Quantity:    0.5,
		Price:       48000,
		Fee:         24,
		FeeCurrency: "USD",
		MarketType:  "spot",
		Timestamp:   "2025-01-15T12:00:00Z",
		ExitReason:  &exitReason,
	}

	trade, err := event.ToDomain()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if trade.ExitReason == nil || *trade.ExitReason != "stop loss hit" {
		t.Errorf("expected exit_reason 'stop loss hit', got %v", trade.ExitReason)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
