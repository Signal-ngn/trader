package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// BuiltinStrategy mirrors the server model.
type BuiltinStrategy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// StrategiesResponse mirrors the server model for GET /strategies.
type StrategiesResponse struct {
	Builtin []BuiltinStrategy `json:"builtin"`
}

var strategiesCmd = &cobra.Command{
	Use:   "strategies",
	Short: "List built-in strategies",
}

// ---- list ----

var strategiesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all built-in strategies",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		q := url.Values{}
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/strategies", q))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		var resp StrategiesResponse
		if err := c.Get(c.apiURL("/strategies", q), &resp); err != nil {
			return err
		}
		var rows [][]string
		for _, s := range resp.Builtin {
			rows = append(rows, []string{s.Name, s.Description})
		}
		PrintTable([]string{"NAME", "DESCRIPTION"}, rows)
		return nil
	},
}

func init() {
	strategiesCmd.AddCommand(strategiesListCmd)
	rootCmd.AddCommand(strategiesCmd)
}
