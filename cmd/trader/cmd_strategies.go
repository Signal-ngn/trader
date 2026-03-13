package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

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
	User    []UserStrategy    `json:"user"`
}

// UserStrategy mirrors the server model.
type UserStrategy struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	Source      string    `json:"source,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

var strategiesCmd = &cobra.Command{
	Use:   "strategies",
	Short: "Manage built-in and user-defined strategies",
}

// ---- list ----

var strategiesListActive bool

var strategiesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all built-in and user strategies",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		q := url.Values{}
		if strategiesListActive {
			q.Set("active", "true")
		}
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
			rows = append(rows, []string{"builtin", s.Name, s.Description, "-"})
		}
		for _, s := range resp.User {
			active := "no"
			if s.IsActive {
				active = "yes"
			}
			rows = append(rows, []string{"user", s.Name, s.Description, active})
		}
		PrintTable([]string{"TYPE", "NAME", "DESCRIPTION", "ACTIVE"}, rows)
		return nil
	},
}

// ---- get ----

var strategiesGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a user strategy by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/user-strategies/" + args[0]))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		var s UserStrategy
		if err := c.Get(c.apiURL("/user-strategies/"+args[0]), &s); err != nil {
			return err
		}
		PrintTable([]string{"FIELD", "VALUE"}, [][]string{
			{"ID", strconv.Itoa(s.ID)},
			{"Name", s.Name},
			{"Description", s.Description},
			{"Active", fmtBool(s.IsActive)},
			{"Created", s.CreatedAt.Format(time.RFC3339)},
		})
		if s.Source != "" {
			fmt.Println("\n--- Source ---")
			fmt.Println(s.Source)
		}
		return nil
	},
}

// ---- validate ----

var (
	stratValidateName string
	stratValidateFile string
)

var strategiesValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a strategy source file",
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.ReadFile(stratValidateFile)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		c := newPlatformClient()
		body := map[string]any{
			"name":   stratValidateName,
			"source": string(src),
		}
		var resp map[string]any
		if err := c.Post(c.ingestionURL("/user-strategies/validate"), body, &resp); err != nil {
			if valid, _ := resp["valid"].(bool); !valid {
				errMsg, _ := resp["error"].(string)
				fmt.Fprintf(os.Stderr, "✗ Validation failed: %s\n", errMsg)
				os.Exit(1)
			}
			return err
		}
		if valid, ok := resp["valid"].(bool); ok && valid {
			fmt.Println("✓ Strategy is valid")
		} else {
			errMsg, _ := resp["error"].(string)
			fmt.Fprintf(os.Stderr, "✗ Validation failed: %s\n", errMsg)
			os.Exit(1)
		}
		return nil
	},
}

// ---- create ----

var (
	stratCreateName        string
	stratCreateFile        string
	stratCreateDescription string
	stratCreateParams      string
)

var strategiesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a user strategy from a source file",
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.ReadFile(stratCreateFile)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		c := newPlatformClient()
		body := map[string]any{
			"name":        stratCreateName,
			"source":      string(src),
			"description": stratCreateDescription,
		}
		if stratCreateParams != "" {
			var params map[string]float64
			if err := json.Unmarshal([]byte(stratCreateParams), &params); err != nil {
				return fmt.Errorf("parse --params JSON: %w", err)
			}
			body["parameters"] = params
		}
		var s UserStrategy
		if err := c.Post(c.apiURL("/user-strategies"), body, &s); err != nil {
			return err
		}
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			return PrintJSON(s)
		}
		fmt.Printf("Created strategy %q (ID: %d)\n", s.Name, s.ID)
		return nil
	},
}

// ---- update ----

var (
	stratUpdateFile        string
	stratUpdateDescription string
	stratUpdateParams      string
)

var strategiesUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a user strategy from a source file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.ReadFile(stratUpdateFile)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		c := newPlatformClient()
		body := map[string]any{
			"source":      string(src),
			"description": stratUpdateDescription,
		}
		if stratUpdateParams != "" {
			var params map[string]float64
			if err := json.Unmarshal([]byte(stratUpdateParams), &params); err != nil {
				return fmt.Errorf("parse --params JSON: %w", err)
			}
			body["parameters"] = params
		}
		useJSON, _ := cmd.Flags().GetBool("json")
		var s UserStrategy
		if err := c.Put(c.apiURL("/user-strategies/"+args[0]), body, &s); err != nil {
			return err
		}
		if useJSON {
			return PrintJSON(s)
		}
		fmt.Printf("Updated strategy ID %s\n", args[0])
		return nil
	},
}

// ---- activate ----

var strategiesActivateCmd = &cobra.Command{
	Use:   "activate <id>",
	Short: "Activate a user strategy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		var resp map[string]any
		if err := c.Post(c.apiURL("/user-strategies/"+args[0]+"/activate"), nil, &resp); err != nil {
			return err
		}
		fmt.Printf("Activated strategy ID %s\n", args[0])
		return nil
	},
}

// ---- deactivate ----

var strategiesDeactivateCmd = &cobra.Command{
	Use:   "deactivate <id>",
	Short: "Deactivate a user strategy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		var resp map[string]any
		if err := c.Post(c.apiURL("/user-strategies/"+args[0]+"/deactivate"), nil, &resp); err != nil {
			return err
		}
		fmt.Printf("Deactivated strategy ID %s\n", args[0])
		return nil
	},
}

// ---- delete ----

var strategiesDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a user strategy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		if err := c.Delete(c.apiURL("/user-strategies/" + args[0])); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("strategy %s not found", args[0])
			}
			return err
		}
		fmt.Printf("Deleted strategy ID %s\n", args[0])
		return nil
	},
}

// ---- backtest ----

var (
	sbtExchange    string
	sbtProduct     string
	sbtGranularity string
	sbtMode        string
	sbtStart       string
	sbtEnd         string
	sbtLeverage    int
)

var strategiesBacktestCmd = &cobra.Command{
	Use:   "backtest <id>",
	Short: "Run a backtest for a user strategy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		body := map[string]any{
			"exchange":    sbtExchange,
			"product_id":  sbtProduct,
			"granularity": sbtGranularity,
			"market_mode": sbtMode,
		}
		if sbtStart != "" {
			body["start_date"] = sbtStart
		}
		if sbtEnd != "" {
			body["end_date"] = sbtEnd
		}
		if sbtLeverage > 0 {
			body["leverage"] = sbtLeverage
		}
		fmt.Printf("Running backtest for user strategy %s...\n", args[0])
		useJSON, _ := cmd.Flags().GetBool("json")
		var result BacktestResult
		if err := c.Post(c.ingestionURL("/user-strategies/"+args[0]+"/backtest"), body, &result); err != nil {
			return err
		}
		if useJSON {
			return PrintJSON(result)
		}
		printBacktestResult(result)
		return nil
	},
}

func init() {
	strategiesListCmd.Flags().BoolVar(&strategiesListActive, "active", false, "Only show active user strategies")

	strategiesValidateCmd.Flags().StringVar(&stratValidateName, "name", "", "Strategy name (required)")
	strategiesValidateCmd.Flags().StringVar(&stratValidateFile, "file", "", "Path to .star source file (required)")
	_ = strategiesValidateCmd.MarkFlagRequired("name")
	_ = strategiesValidateCmd.MarkFlagRequired("file")

	strategiesCreateCmd.Flags().StringVar(&stratCreateName, "name", "", "Strategy name (required)")
	strategiesCreateCmd.Flags().StringVar(&stratCreateFile, "file", "", "Path to .star source file (required)")
	strategiesCreateCmd.Flags().StringVar(&stratCreateDescription, "description", "", "Strategy description")
	strategiesCreateCmd.Flags().StringVar(&stratCreateParams, "params", "", "Parameters JSON object, e.g. '{\"THRESHOLD\": 2.0}'")
	_ = strategiesCreateCmd.MarkFlagRequired("name")
	_ = strategiesCreateCmd.MarkFlagRequired("file")

	strategiesUpdateCmd.Flags().StringVar(&stratUpdateFile, "file", "", "Path to .star source file (required)")
	strategiesUpdateCmd.Flags().StringVar(&stratUpdateDescription, "description", "", "Strategy description")
	strategiesUpdateCmd.Flags().StringVar(&stratUpdateParams, "params", "", "Parameters JSON object")
	_ = strategiesUpdateCmd.MarkFlagRequired("file")

	strategiesBacktestCmd.Flags().StringVar(&sbtExchange, "exchange", "", "Exchange (required)")
	strategiesBacktestCmd.Flags().StringVar(&sbtProduct, "product", "", "Product ID (required)")
	strategiesBacktestCmd.Flags().StringVar(&sbtGranularity, "granularity", "", "Granularity (required)")
	strategiesBacktestCmd.Flags().StringVar(&sbtMode, "mode", "spot", "Market mode: spot, futures-long, futures-short")
	strategiesBacktestCmd.Flags().StringVar(&sbtStart, "start", "", "Start date YYYY-MM-DD")
	strategiesBacktestCmd.Flags().StringVar(&sbtEnd, "end", "", "End date YYYY-MM-DD")
	strategiesBacktestCmd.Flags().IntVar(&sbtLeverage, "leverage", 0, "Leverage (futures modes)")
	_ = strategiesBacktestCmd.MarkFlagRequired("exchange")
	_ = strategiesBacktestCmd.MarkFlagRequired("product")
	_ = strategiesBacktestCmd.MarkFlagRequired("granularity")

	strategiesCmd.AddCommand(
		strategiesListCmd,
		strategiesGetCmd,
		strategiesValidateCmd,
		strategiesCreateCmd,
		strategiesUpdateCmd,
		strategiesActivateCmd,
		strategiesDeactivateCmd,
		strategiesDeleteCmd,
		strategiesBacktestCmd,
	)
	rootCmd.AddCommand(strategiesCmd)
}
