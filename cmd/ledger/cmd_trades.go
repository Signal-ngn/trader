package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

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

var (
	tradesSymbol     string
	tradesSide       string
	tradesMarketType string
	tradesStart      string
	tradesEnd        string
	tradesLimit      int
)

var tradesCmd = &cobra.Command{
	Use:   "trades <account-id>",
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

			// Check limit
			if tradesLimit > 0 && len(allTrades) >= tradesLimit {
				allTrades = allTrades[:tradesLimit]
				break
			}

			// No more pages
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

func init() {
	tradesCmd.Flags().StringVar(&tradesSymbol, "symbol", "", "Filter by symbol (e.g. BTC-USD)")
	tradesCmd.Flags().StringVar(&tradesSide, "side", "", "Filter by side: buy, sell")
	tradesCmd.Flags().StringVar(&tradesMarketType, "market-type", "", "Filter by market type: spot, futures")
	tradesCmd.Flags().StringVar(&tradesStart, "start", "", "Filter from timestamp (RFC3339)")
	tradesCmd.Flags().StringVar(&tradesEnd, "end", "", "Filter to timestamp (RFC3339)")
	tradesCmd.Flags().IntVar(&tradesLimit, "limit", 50, "Max results to return (0 = all pages)")
	rootCmd.AddCommand(tradesCmd)
}
