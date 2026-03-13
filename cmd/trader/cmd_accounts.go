package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

// AccountStats mirrors the JSON returned by GET /api/v1/accounts/{accountId}/stats.
type AccountStats struct {
	TotalTrades      int      `json:"total_trades"`
	ClosedTrades     int      `json:"closed_trades"`
	WinCount         int      `json:"win_count"`
	LossCount        int      `json:"loss_count"`
	WinRate          float64  `json:"win_rate"`
	TotalRealizedPnL float64  `json:"total_realized_pnl"`
	OpenPositions    int      `json:"open_positions"`
	Balance          *float64 `json:"balance,omitempty"`
}

// accountBalance mirrors the JSON returned by GET/PUT /api/v1/accounts/{accountId}/balance.
type accountBalance struct {
	AccountID string  `json:"account_id"`
	Currency  string  `json:"currency"`
	Amount    float64 `json:"amount"`
}

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Manage ledger accounts",
}

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all accounts for the authenticated tenant",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		useJSON, _ := cmd.Flags().GetBool("json")

		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/api/v1/accounts"))
			if err != nil {
				return err
			}
			fmt.Print(string(raw))
			return nil
		}

		var accounts []account
		if err := c.Get(c.apiURL("/api/v1/accounts"), &accounts); err != nil {
			return err
		}

		rows := make([][]string, len(accounts))
		for i, a := range accounts {
			rows[i] = []string{a.ID, a.Name, a.Type, fmtTime(a.CreatedAt)}
		}
		PrintTable([]string{"ID", "NAME", "TYPE", "CREATED"}, rows)
		return nil
	},
}

var accountsShowCmd = &cobra.Command{
	Use:   "show <account-id>",
	Short: "Show all-time aggregate statistics for an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		c := newPlatformClient()
		useJSON, _ := cmd.Flags().GetBool("json")

		statusCode, raw, err := c.GetRaw(c.apiURL("/api/v1/accounts/" + accountID + "/stats"))
		if err != nil {
			return err
		}

		if statusCode == 404 {
			fmt.Fprintln(os.Stderr, "account not found")
			os.Exit(1)
		}
		if statusCode < 200 || statusCode >= 300 {
			return fmt.Errorf("API error %d: %s", statusCode, string(raw))
		}

		if useJSON {
			fmt.Print(string(raw))
			return nil
		}

		var stats AccountStats
		if err := json.Unmarshal(raw, &stats); err != nil {
			return fmt.Errorf("decode stats: %w", err)
		}

		winRatePct := stats.WinRate * 100
		rows := [][]string{
			{"Account", accountID},
			{"Total Trades", fmt.Sprintf("%d", stats.TotalTrades)},
			{"Closed Trades", fmt.Sprintf("%d", stats.ClosedTrades)},
			{"Wins", fmt.Sprintf("%d", stats.WinCount)},
			{"Losses", fmt.Sprintf("%d", stats.LossCount)},
			{"Win Rate", fmt.Sprintf("%.2f%%", winRatePct)},
			{"Realized P&L", fmtFloat(stats.TotalRealizedPnL)},
			{"Open Positions", fmt.Sprintf("%d", stats.OpenPositions)},
		}
		if stats.Balance != nil {
			rows = append(rows, []string{"Balance (USD)", fmtFloat2(*stats.Balance)})
		} else {
			rows = append(rows, []string{"Balance (USD)", "not set"})
		}
		PrintTable([]string{"FIELD", "VALUE"}, rows)
		return nil
	},
}

// accountsBalanceCmd is the parent command group for balance subcommands.
var accountsBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Manage account cash balance",
}

var accountsBalanceSetCmd = &cobra.Command{
	Use:   "set <account-id> <amount>",
	Short: "Set the cash balance for an account (overwrites any existing value)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		amount, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			return fmt.Errorf("invalid amount %q: must be a number", args[1])
		}
		currency, _ := cmd.Flags().GetString("currency")
		useJSON, _ := cmd.Flags().GetBool("json")

		c := newPlatformClient()
		body, _ := json.Marshal(map[string]interface{}{
			"amount":   amount,
			"currency": currency,
		})

		statusCode, raw, err := c.PutRaw(c.apiURL("/api/v1/accounts/"+accountID+"/balance"), bytes.NewReader(body))
		if err != nil {
			return err
		}
		if statusCode < 200 || statusCode >= 300 {
			return fmt.Errorf("API error %d: %s", statusCode, string(raw))
		}

		if useJSON {
			fmt.Print(string(raw))
			return nil
		}

		var bal accountBalance
		if err := json.Unmarshal(raw, &bal); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		PrintTable([]string{"FIELD", "VALUE"}, [][]string{
			{"Account", bal.AccountID},
			{"Currency", bal.Currency},
			{"Balance", fmtFloat2(bal.Amount)},
		})
		return nil
	},
}

var accountsBalanceGetCmd = &cobra.Command{
	Use:   "get <account-id>",
	Short: "Get the current cash balance for an account",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountID := args[0]
		currency, _ := cmd.Flags().GetString("currency")
		useJSON, _ := cmd.Flags().GetBool("json")

		c := newPlatformClient()
		url := c.apiURL("/api/v1/accounts/" + accountID + "/balance?currency=" + currency)
		statusCode, raw, err := c.GetRaw(url)
		if err != nil {
			return err
		}

		if statusCode == 404 {
			fmt.Fprintf(os.Stderr, "no balance set for %s\n", accountID)
			os.Exit(1)
		}
		if statusCode < 200 || statusCode >= 300 {
			return fmt.Errorf("API error %d: %s", statusCode, string(raw))
		}

		if useJSON {
			fmt.Print(string(raw))
			return nil
		}

		var bal accountBalance
		if err := json.Unmarshal(raw, &bal); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		PrintTable([]string{"FIELD", "VALUE"}, [][]string{
			{"Account", bal.AccountID},
			{"Currency", bal.Currency},
			{"Balance", fmtFloat2(bal.Amount)},
		})
		return nil
	},
}

func init() {
	accountsListCmd.Flags().Bool("json", false, "Output raw JSON")

	accountsShowCmd.Flags().Bool("json", false, "Output raw JSON")

	accountsBalanceSetCmd.Flags().Bool("json", false, "Output raw JSON")
	accountsBalanceSetCmd.Flags().String("currency", "USD", "Currency code (default USD)")

	accountsBalanceGetCmd.Flags().Bool("json", false, "Output raw JSON")
	accountsBalanceGetCmd.Flags().String("currency", "USD", "Currency code (default USD)")

	accountsBalanceCmd.AddCommand(accountsBalanceSetCmd)
	accountsBalanceCmd.AddCommand(accountsBalanceGetCmd)

	accountsCmd.AddCommand(accountsListCmd)
	accountsCmd.AddCommand(accountsShowCmd)
	accountsCmd.AddCommand(accountsBalanceCmd)

	rootCmd.AddCommand(accountsCmd)
}
