package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type order struct {
	OrderID      string    `json:"order_id"`
	AccountID    string    `json:"account_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	OrderType    string    `json:"order_type"`
	RequestedQty float64   `json:"requested_qty"`
	FilledQty    float64   `json:"filled_qty"`
	AvgFillPrice float64   `json:"avg_fill_price"`
	Status       string    `json:"status"`
	MarketType   string    `json:"market_type"`
	CreatedAt    time.Time `json:"created_at"`
}

type orderListResult struct {
	Orders     []order `json:"orders"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

var (
	ordersStatus string
	ordersSymbol string
	ordersLimit  int
)

var ordersCmd = &cobra.Command{
	Use:   "orders <account-id>",
	Short: "List orders for an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		c := newClient()
		useJSON, _ := cmd.Flags().GetBool("json")

		var allOrders []order
		cursor := ""
		pageSize := 50
		if ordersLimit > 0 && ordersLimit < pageSize {
			pageSize = ordersLimit
		}

		for {
			q := url.Values{}
			q.Set("limit", fmt.Sprintf("%d", pageSize))
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			if ordersStatus != "" {
				q.Set("status", ordersStatus)
			}
			if ordersSymbol != "" {
				q.Set("symbol", ordersSymbol)
			}

			endpoint := c.ledgerURL("/api/v1/accounts/"+accountID+"/orders", q)
			var result orderListResult
			if err := c.Get(endpoint, &result); err != nil {
				return err
			}

			allOrders = append(allOrders, result.Orders...)

			if ordersLimit > 0 && len(allOrders) >= ordersLimit {
				allOrders = allOrders[:ordersLimit]
				break
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}

		if useJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(allOrders)
		}

		rows := make([][]string, len(allOrders))
		for i, o := range allOrders {
			rows[i] = []string{
				o.OrderID,
				o.Symbol,
				o.Side,
				o.OrderType,
				fmtFloat(o.RequestedQty),
				fmtFloat(o.FilledQty),
				fmtFloat(o.AvgFillPrice),
				o.Status,
				fmtTime(o.CreatedAt),
			}
		}
		PrintTable(
			[]string{"ORDER-ID", "SYMBOL", "SIDE", "TYPE", "REQ-QTY", "FILLED-QTY", "AVG-FILL", "STATUS", "CREATED"},
			rows,
		)
		return nil
	},
}

func init() {
	ordersCmd.Flags().StringVar(&ordersStatus, "status", "", "Filter by status: open, filled, partially_filled, cancelled")
	ordersCmd.Flags().StringVar(&ordersSymbol, "symbol", "", "Filter by symbol")
	ordersCmd.Flags().IntVar(&ordersLimit, "limit", 50, "Max results to return (0 = all pages)")
	rootCmd.AddCommand(ordersCmd)
}
