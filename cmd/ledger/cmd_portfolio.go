package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type position struct {
	ID            string     `json:"id"`
	AccountID     string     `json:"account_id"`
	Symbol        string     `json:"symbol"`
	MarketType    string     `json:"market_type"`
	Side          string     `json:"side"`
	Quantity      float64    `json:"quantity"`
	AvgEntryPrice float64    `json:"avg_entry_price"`
	CostBasis     float64    `json:"cost_basis"`
	RealizedPnL   float64    `json:"realized_pnl"`
	Status        string     `json:"status"`
	OpenedAt      time.Time  `json:"opened_at"`
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
}

type portfolioSummary struct {
	Positions        []position `json:"positions"`
	TotalRealizedPnL float64    `json:"total_realized_pnl"`
}

var portfolioCmd = &cobra.Command{
	Use:   "portfolio <account-id>",
	Short: "Show portfolio summary (open positions + total realized P&L)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		c := newClient()
		endpoint := c.ledgerURL("/api/v1/accounts/" + accountID + "/portfolio")
		useJSON, _ := cmd.Flags().GetBool("json")

		if useJSON {
			status, raw, err := c.GetRaw(endpoint)
			if err != nil {
				return err
			}
			if status == 404 {
				fmt.Fprintln(os.Stderr, "account not found")
				os.Exit(1)
			}
			fmt.Print(string(raw))
			return nil
		}

		var summary portfolioSummary
		if err := c.Get(endpoint, &summary); err != nil {
			if isNotFound(err) {
				fmt.Fprintln(os.Stderr, "account not found")
				os.Exit(1)
			}
			return err
		}

		rows := make([][]string, len(summary.Positions))
		for i, p := range summary.Positions {
			rows[i] = []string{
				p.Symbol,
				p.Side,
				p.MarketType,
				fmtFloat(p.Quantity),
				fmtFloat(p.AvgEntryPrice),
				fmtFloat2(p.CostBasis),
				fmtFloat2(p.RealizedPnL),
				p.Status,
			}
		}
		PrintTable(
			[]string{"SYMBOL", "SIDE", "MARKET", "QTY", "AVG-ENTRY", "COST-BASIS", "REALIZED-PNL", "STATUS"},
			rows,
		)
		fmt.Printf("\nTotal Realized P&L: %s\n", fmtFloat2(summary.TotalRealizedPnL))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(portfolioCmd)
}
