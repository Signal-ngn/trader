package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

type account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Manage ledger accounts",
}

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all accounts for the authenticated tenant",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		useJSON, _ := cmd.Flags().GetBool("json")

		if useJSON {
			_, raw, err := c.GetRaw(c.ledgerURL("/api/v1/accounts"))
			if err != nil {
				return err
			}
			fmt.Print(string(raw))
			return nil
		}

		var accounts []account
		if err := c.Get(c.ledgerURL("/api/v1/accounts"), &accounts); err != nil {
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

func init() {
	accountsCmd.AddCommand(accountsListCmd)
	rootCmd.AddCommand(accountsCmd)
}
