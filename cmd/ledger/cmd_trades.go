package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type trade struct {
	TradeID     string    `json:"trade_id"`
	AccountID   string    `json:"account_id"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`
	Quantity    float64   `json:"quantity"`
	Price       float64   `json:"price"`
	Fee         float64   `json:"fee"`
	FeeCurrency string    `json:"fee_currency"`
	MarketType  string    `json:"market_type"`
	Timestamp   time.Time `json:"timestamp"`
}

type tradeListResult struct {
	Trades     []trade `json:"trades"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// addTradeRequest mirrors the import payload for a single trade.
// tenant_id is injected server-side from the auth context.
type addTradeRequest struct {
	Trades []addTradeEvent `json:"trades"`
}

type addTradeEvent struct {
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

	// Futures-specific (optional)
	Leverage         *int     `json:"leverage,omitempty"`
	Margin           *float64 `json:"margin,omitempty"`
	LiquidationPrice *float64 `json:"liquidation_price,omitempty"`
	FundingFee       *float64 `json:"funding_fee,omitempty"`

	// Strategy metadata (optional)
	Strategy    *string  `json:"strategy,omitempty"`
	EntryReason *string  `json:"entry_reason,omitempty"`
	ExitReason  *string  `json:"exit_reason,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
	StopLoss    *float64 `json:"stop_loss,omitempty"`
	TakeProfit  *float64 `json:"take_profit,omitempty"`
}

// ── list flags ────────────────────────────────────────────────────────────────

var (
	tradesSymbol     string
	tradesSide       string
	tradesMarketType string
	tradesStart      string
	tradesEnd        string
	tradesLimit      int
)

// ── add flags ─────────────────────────────────────────────────────────────────

var (
	addTradeID          string
	addSymbol           string
	addSide             string
	addQuantity         float64
	addPrice            float64
	addFee              float64
	addFeeCurrency      string
	addMarketType       string
	addTimestamp        string
	addStrategy         string
	addEntryReason      string
	addExitReason       string
	addConfidence       float64
	addStopLoss         float64
	addTakeProfit       float64
	addLeverage         int
	addMargin           float64
	addLiquidationPrice float64
	addFundingFee       float64
)

// ── root trades command ───────────────────────────────────────────────────────

var tradesCmd = &cobra.Command{
	Use:   "trades",
	Short: "Manage trades",
}

// ── trades list ───────────────────────────────────────────────────────────────

var tradesListCmd = &cobra.Command{
	Use:   "list <account-id>",
	Short: "List trades for an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		c := newClient()
		useJSON, _ := cmd.Flags().GetBool("json")

		var allTrades []trade
		cursor := ""
		pageSize := 50
		if tradesLimit > 0 && tradesLimit < pageSize {
			pageSize = tradesLimit
		}

		for {
			q := url.Values{}
			q.Set("limit", fmt.Sprintf("%d", pageSize))
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			if tradesSymbol != "" {
				q.Set("symbol", tradesSymbol)
			}
			if tradesSide != "" {
				q.Set("side", tradesSide)
			}
			if tradesMarketType != "" {
				q.Set("market_type", tradesMarketType)
			}
			if tradesStart != "" {
				q.Set("start", tradesStart)
			}
			if tradesEnd != "" {
				q.Set("end", tradesEnd)
			}

			endpoint := c.ledgerURL("/api/v1/accounts/"+accountID+"/trades", q)
			var result tradeListResult
			if err := c.Get(endpoint, &result); err != nil {
				return err
			}

			allTrades = append(allTrades, result.Trades...)

			if tradesLimit > 0 && len(allTrades) >= tradesLimit {
				allTrades = allTrades[:tradesLimit]
				break
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}

		if useJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(allTrades)
		}

		rows := make([][]string, len(allTrades))
		for i, t := range allTrades {
			rows[i] = []string{
				t.TradeID,
				t.Symbol,
				t.Side,
				fmtFloat(t.Quantity),
				fmtFloat(t.Price),
				fmtFloat(t.Fee),
				t.MarketType,
				fmtTime(t.Timestamp),
			}
		}
		PrintTable(
			[]string{"TRADE-ID", "SYMBOL", "SIDE", "QTY", "PRICE", "FEE", "MARKET", "TIMESTAMP"},
			rows,
		)
		return nil
	},
}

// ── trades add ────────────────────────────────────────────────────────────────

var tradesAddCmd = &cobra.Command{
	Use:   "add <account-id>",
	Short: "Record a single trade",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		useJSON, _ := cmd.Flags().GetBool("json")

		// Generate a trade ID if not provided
		tradeID := addTradeID
		if tradeID == "" {
			tradeID = uuid.New().String()
		}

		// Default timestamp to now
		ts := addTimestamp
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339)
		} else {
			// Validate provided timestamp
			if _, err := time.Parse(time.RFC3339, ts); err != nil {
				return fmt.Errorf("invalid --timestamp: must be RFC3339, e.g. 2025-01-01T10:00:00Z")
			}
		}

		// Default fee currency
		feeCurrency := addFeeCurrency
		if feeCurrency == "" {
			feeCurrency = "USD"
		}

		event := addTradeEvent{
			TradeID:     tradeID,
			AccountID:   accountID,
			Symbol:      addSymbol,
			Side:        addSide,
			Quantity:    addQuantity,
			Price:       addPrice,
			Fee:         addFee,
			FeeCurrency: feeCurrency,
			MarketType:  addMarketType,
			Timestamp:   ts,
		}

		// Optional strategy metadata
		if cmd.Flags().Changed("strategy") {
			event.Strategy = &addStrategy
		}
		if cmd.Flags().Changed("entry-reason") {
			event.EntryReason = &addEntryReason
		}
		if cmd.Flags().Changed("exit-reason") {
			event.ExitReason = &addExitReason
		}
		if cmd.Flags().Changed("confidence") {
			event.Confidence = &addConfidence
		}
		if cmd.Flags().Changed("stop-loss") {
			event.StopLoss = &addStopLoss
		}
		if cmd.Flags().Changed("take-profit") {
			event.TakeProfit = &addTakeProfit
		}

		// Optional futures fields
		if cmd.Flags().Changed("leverage") {
			event.Leverage = &addLeverage
		}
		if cmd.Flags().Changed("margin") {
			event.Margin = &addMargin
		}
		if cmd.Flags().Changed("liquidation-price") {
			event.LiquidationPrice = &addLiquidationPrice
		}
		if cmd.Flags().Changed("funding-fee") {
			event.FundingFee = &addFundingFee
		}

		c := newClient()
		endpoint := c.ledgerURL("/api/v1/import")
		req := addTradeRequest{Trades: []addTradeEvent{event}}

		var result importResult
		if err := c.Post(endpoint, req, &result); err != nil {
			return err
		}

		if useJSON {
			return PrintJSON(result)
		}

		if result.Errors > 0 {
			for _, r := range result.Results {
				if r.Status == "error" {
					return fmt.Errorf("trade rejected: %s", r.Error)
				}
			}
		}

		switch {
		case result.Inserted > 0:
			fmt.Printf("recorded trade %s\n", tradeID)
		case result.Duplicates > 0:
			fmt.Printf("duplicate: trade %s already exists\n", tradeID)
		}
		return nil
	},
}

func init() {
	// trades list flags
	tradesListCmd.Flags().StringVar(&tradesSymbol, "symbol", "", "Filter by symbol (e.g. BTC-USD)")
	tradesListCmd.Flags().StringVar(&tradesSide, "side", "", "Filter by side: buy, sell")
	tradesListCmd.Flags().StringVar(&tradesMarketType, "market-type", "", "Filter by market type: spot, futures")
	tradesListCmd.Flags().StringVar(&tradesStart, "start", "", "Filter from timestamp (RFC3339)")
	tradesListCmd.Flags().StringVar(&tradesEnd, "end", "", "Filter to timestamp (RFC3339)")
	tradesListCmd.Flags().IntVar(&tradesLimit, "limit", 50, "Max results to return (0 = all pages)")

	// trades add required flags
	tradesAddCmd.Flags().StringVar(&addTradeID, "trade-id", "", "Trade ID (default: auto-generated UUID)")
	tradesAddCmd.Flags().StringVar(&addSymbol, "symbol", "", "Trading pair, e.g. BTC-USD")
	tradesAddCmd.Flags().StringVar(&addSide, "side", "", "Trade side: buy or sell")
	tradesAddCmd.Flags().Float64Var(&addQuantity, "quantity", 0, "Trade size")
	tradesAddCmd.Flags().Float64Var(&addPrice, "price", 0, "Fill price")
	tradesAddCmd.Flags().Float64Var(&addFee, "fee", 0, "Fee paid")
	tradesAddCmd.Flags().StringVar(&addFeeCurrency, "fee-currency", "", "Fee currency (default: USD)")
	tradesAddCmd.Flags().StringVar(&addMarketType, "market-type", "spot", "Market type: spot or futures")
	tradesAddCmd.Flags().StringVar(&addTimestamp, "timestamp", "", "Execution time in RFC3339 (default: now)")
	_ = tradesAddCmd.MarkFlagRequired("symbol")
	_ = tradesAddCmd.MarkFlagRequired("side")
	_ = tradesAddCmd.MarkFlagRequired("quantity")
	_ = tradesAddCmd.MarkFlagRequired("price")

	// trades add optional strategy metadata flags
	tradesAddCmd.Flags().StringVar(&addStrategy, "strategy", "", "Strategy name")
	tradesAddCmd.Flags().StringVar(&addEntryReason, "entry-reason", "", "Entry reason")
	tradesAddCmd.Flags().StringVar(&addExitReason, "exit-reason", "", "Exit reason")
	tradesAddCmd.Flags().Float64Var(&addConfidence, "confidence", 0, "Signal confidence (0–1)")
	tradesAddCmd.Flags().Float64Var(&addStopLoss, "stop-loss", 0, "Stop-loss price")
	tradesAddCmd.Flags().Float64Var(&addTakeProfit, "take-profit", 0, "Take-profit price")

	// trades add optional futures flags
	tradesAddCmd.Flags().IntVar(&addLeverage, "leverage", 0, "Leverage (futures)")
	tradesAddCmd.Flags().Float64Var(&addMargin, "margin", 0, "Margin used (futures)")
	tradesAddCmd.Flags().Float64Var(&addLiquidationPrice, "liquidation-price", 0, "Liquidation price (futures)")
	tradesAddCmd.Flags().Float64Var(&addFundingFee, "funding-fee", 0, "Funding fee (futures)")

	tradesCmd.AddCommand(tradesListCmd)
	tradesCmd.AddCommand(tradesAddCmd)
	rootCmd.AddCommand(tradesCmd)
}
