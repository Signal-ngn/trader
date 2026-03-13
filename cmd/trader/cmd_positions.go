package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var positionsStatus string

var positionsCmd = &cobra.Command{
	Use:   "positions <account-id>",
	Short: "List positions for an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		c := newPlatformClient()

		q := url.Values{}
		q.Set("status", positionsStatus)
		endpoint := c.apiURL("/api/v1/accounts/"+accountID+"/positions", q)
		useJSON, _ := cmd.Flags().GetBool("json")

		if useJSON {
			_, raw, err := c.GetRaw(endpoint)
			if err != nil {
				return err
			}
			fmt.Print(string(raw))
			return nil
		}

		var resp struct {
			Positions []position `json:"positions"`
		}
		if err := c.Get(endpoint, &resp); err != nil {
			return err
		}
		positions := resp.Positions

		rows := make([][]string, len(positions))
		for i, p := range positions {
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
		return nil
	},
}

func init() {
	positionsCmd.Flags().StringVar(&positionsStatus, "status", "open", "Filter by status: open, closed, all")
	rootCmd.AddCommand(positionsCmd)
}
