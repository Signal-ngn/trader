package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type importTradeResult struct {
	TradeID string `json:"trade_id"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type importResult struct {
	Total      int                 `json:"total"`
	Inserted   int                 `json:"inserted"`
	Duplicates int                 `json:"duplicates"`
	Errors     int                 `json:"errors"`
	Results    []importTradeResult `json:"results,omitempty"`
}

var importCmd = &cobra.Command{
	Use:   "import <file.json>",
	Short: "Import a batch of historic trades from a JSON file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]
		useJSON, _ := cmd.Flags().GetBool("json")

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}

		c := newPlatformClient()
		endpoint := c.apiURL("/api/v1/import")

		if useJSON {
			status, raw, err := c.PostRaw(endpoint, data)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return fmt.Errorf("API error %d: %s", status, string(raw))
			}
			cmd.Print(string(raw))
			return nil
		}

		status, raw, err := c.PostRaw(endpoint, data)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("API error %d: %s", status, string(raw))
		}

		var result importResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		fmt.Printf("Total: %d  Inserted: %d  Duplicates: %d  Errors: %d\n",
			result.Total, result.Inserted, result.Duplicates, result.Errors)

		for _, r := range result.Results {
			if r.Status == "error" {
				fmt.Printf("  error: %s — %s\n", r.TradeID, r.Error)
			}
		}

		if result.Errors > 0 {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
}
