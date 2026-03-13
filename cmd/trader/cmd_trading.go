package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// TradingConfig mirrors the server model.
type TradingConfig struct {
	ID              int                           `json:"id"`
	AccountID       string                        `json:"account_id"`
	Exchange        string                        `json:"exchange"`
	ProductID       string                        `json:"product_id"`
	Granularity     string                        `json:"granularity"`
	StrategiesSpot  []string                      `json:"strategies_spot"`
	StrategiesLong  []string                      `json:"strategies_long"`
	StrategiesShort []string                      `json:"strategies_short"`
	LongLeverage    int                           `json:"long_leverage"`
	ShortLeverage   int                           `json:"short_leverage"`
	TrendFilter     bool                          `json:"trend_filter"`
	StrategyParams  map[string]map[string]float64 `json:"strategy_params"`
	Enabled         bool                          `json:"enabled"`
}

var tradingCmd = &cobra.Command{
	Use:   "trading",
	Short: "Manage server-side trading configuration",
}

// ---- list ----

var tradingListEnabled bool

var tradingListCmd = &cobra.Command{
	Use:   "list [account]",
	Short: "List trading configs (optionally filtered by account)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		q := url.Values{}
		if len(args) == 1 {
			q.Set("account_id", args[0])
		}
		if tradingListEnabled {
			q.Set("enabled", "true")
		}
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/config/trading", q))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		var configs []TradingConfig
		if err := c.Get(c.apiURL("/config/trading", q), &configs); err != nil {
			return err
		}
		rows := make([][]string, len(configs))
		for i, tc := range configs {
			rows[i] = []string{
				tc.AccountID,
				tc.Exchange,
				tc.ProductID,
				tc.Granularity,
				strings.Join(tc.StrategiesLong, ","),
				strings.Join(tc.StrategiesShort, ","),
				strings.Join(tc.StrategiesSpot, ","),
				strconv.Itoa(tc.LongLeverage),
				strconv.Itoa(tc.ShortLeverage),
				fmtBool(tc.TrendFilter),
				fmtBool(tc.Enabled),
				fmtStrategyParams(tc.StrategyParams),
			}
		}
		PrintTable(
			[]string{"ACCOUNT", "EXCHANGE", "PRODUCT", "GRANULARITY", "LONG", "SHORT", "SPOT", "L-LEV", "S-LEV", "TREND", "ENABLED", "PARAMS"},
			rows,
		)
		return nil
	},
}

// ---- get ----

var tradingGetCmd = &cobra.Command{
	Use:   "get <account> <exchange> <product>",
	Short: "Get a trading config",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, exchange, product := args[0], args[1], args[2]
		c := newPlatformClient()
		q := url.Values{}
		q.Set("account_id", account)
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/config/trading/"+exchange+"/"+product, q))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		var tc TradingConfig
		if err := c.Get(c.apiURL("/config/trading/"+exchange+"/"+product, q), &tc); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("trading config not found for %s/%s (account: %s)", exchange, product, account)
			}
			return err
		}
		printTradingConfig(tc)
		return nil
	},
}

// ---- set ----

var (
	tsGranularity   string
	tsLong          string
	tsShort         string
	tsSpot          string
	tsLongLeverage  int
	tsShortLeverage int
	tsTrendFilter   bool
	tsNoTrendFilter bool
	tsEnable        bool
	tsDisable       bool
	tsParams        []string
)

var tradingSetCmd = &cobra.Command{
	Use:   "set <account> <exchange> <product>",
	Short: "Create or update a trading config (unset flags preserve existing values)",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, exchange, product := args[0], args[1], args[2]
		c := newPlatformClient()

		// Fetch existing config to merge unset flags.
		fetchQ := url.Values{}
		fetchQ.Set("account_id", account)
		existing := &TradingConfig{
			AccountID:   account,
			Exchange:    exchange,
			ProductID:   product,
			Granularity: "ONE_HOUR",
			Enabled:     true,
		}
		_ = c.Get(c.apiURL("/config/trading/"+exchange+"/"+product, fetchQ), existing)

		// Build PUT body, merging with existing.
		body := map[string]any{
			"account_id":       account,
			"granularity":      existing.Granularity,
			"strategies_long":  existing.StrategiesLong,
			"strategies_short": existing.StrategiesShort,
			"strategies_spot":  existing.StrategiesSpot,
			"long_leverage":    existing.LongLeverage,
			"short_leverage":   existing.ShortLeverage,
			"trend_filter":     existing.TrendFilter,
			"enabled":          existing.Enabled,
		}

		if cmd.Flags().Changed("granularity") {
			body["granularity"] = tsGranularity
		}
		if cmd.Flags().Changed("long") {
			body["strategies_long"] = splitCSVOrEmpty(tsLong)
		}
		if cmd.Flags().Changed("short") {
			body["strategies_short"] = splitCSVOrEmpty(tsShort)
		}
		if cmd.Flags().Changed("spot") {
			body["strategies_spot"] = splitCSVOrEmpty(tsSpot)
		}
		if cmd.Flags().Changed("long-leverage") {
			body["long_leverage"] = tsLongLeverage
		}
		if cmd.Flags().Changed("short-leverage") {
			body["short_leverage"] = tsShortLeverage
		}
		if cmd.Flags().Changed("trend-filter") {
			body["trend_filter"] = tsTrendFilter
		}
		if cmd.Flags().Changed("no-trend-filter") {
			body["trend_filter"] = false
		}
		if cmd.Flags().Changed("enable") {
			body["enabled"] = true
		}
		if cmd.Flags().Changed("disable") {
			body["enabled"] = false
		}

		// Handle --params: parse, validate, merge into existing strategy_params.
		if cmd.Flags().Changed("params") {
			merged, err := mergeStrategyParams(existing.StrategyParams, tsParams)
			if err != nil {
				return err
			}
			body["strategy_params"] = merged
		}

		var tc TradingConfig
		if err := c.Put(c.apiURL("/config/trading/"+exchange+"/"+product), body, &tc); err != nil {
			return err
		}
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			return PrintJSON(tc)
		}
		printTradingConfig(tc)
		return nil
	},
}

// ---- delete ----

var tradingDeleteCmd = &cobra.Command{
	Use:   "delete <account> <exchange> <product>",
	Short: "Delete a trading config",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		account, exchange, product := args[0], args[1], args[2]
		c := newPlatformClient()
		q := url.Values{}
		q.Set("account_id", account)
		if err := c.Delete(c.apiURL("/config/trading/"+exchange+"/"+product, q)); err != nil {
			if isNotFound(err) {
				return fmt.Errorf("trading config not found for %s/%s (account: %s)", exchange, product, account)
			}
			return err
		}
		fmt.Printf("Deleted trading config for %s/%s\n", exchange, product)
		return nil
	},
}

// ---- wipe ----

var (
	twConfirm   bool
	twExchange  string
)

var tradingWipeCmd = &cobra.Command{
	Use:   "wipe <account>",
	Short: "Delete all trading configs for an account",
	Long: `Delete every trading config for the given account, leaving it with a clean
slate ready for a fresh configuration.

Use --exchange to restrict deletion to a single exchange.
The --confirm flag is required to prevent accidental wipes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !twConfirm {
			return fmt.Errorf("use --confirm to wipe all trading configs")
		}
		account := args[0]
		c := newPlatformClient()

		// Fetch all configs for this account.
		q := url.Values{}
		q.Set("account_id", account)
		var configs []TradingConfig
		if err := c.Get(c.apiURL("/config/trading", q), &configs); err != nil {
			return err
		}

		// Filter by exchange if requested.
		if twExchange != "" {
			filtered := configs[:0]
			for _, tc := range configs {
				if tc.Exchange == twExchange {
					filtered = append(filtered, tc)
				}
			}
			configs = filtered
		}

		if len(configs) == 0 {
			fmt.Println("no trading configs found — nothing to wipe")
			return nil
		}

		// Delete each config.
		deleted, failed := 0, 0
		dq := url.Values{}
		dq.Set("account_id", account)
		for _, tc := range configs {
			err := c.Delete(c.apiURL("/config/trading/"+tc.Exchange+"/"+tc.ProductID, dq))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "  error deleting %s/%s: %v\n", tc.Exchange, tc.ProductID, err)
				failed++
			} else {
				fmt.Printf("  deleted %s/%s\n", tc.Exchange, tc.ProductID)
				deleted++
			}
		}

		fmt.Printf("wiped %d config(s)", deleted)
		if failed > 0 {
			fmt.Printf(", %d error(s)", failed)
		}
		fmt.Println()
		return nil
	},
}

// ---- helpers ----

func printTradingConfig(tc TradingConfig) {
	PrintTable(
		[]string{"FIELD", "VALUE"},
		[][]string{
			{"Account ID", tc.AccountID},
			{"Exchange", tc.Exchange},
			{"Product", tc.ProductID},
			{"Granularity", tc.Granularity},
			{"Strategies Long", strings.Join(tc.StrategiesLong, ", ")},
			{"Strategies Short", strings.Join(tc.StrategiesShort, ", ")},
			{"Strategies Spot", strings.Join(tc.StrategiesSpot, ", ")},
			{"Long Leverage", strconv.Itoa(tc.LongLeverage)},
			{"Short Leverage", strconv.Itoa(tc.ShortLeverage)},
			{"Trend Filter", fmtBool(tc.TrendFilter)},
			{"Enabled", fmtBool(tc.Enabled)},
			{"Params", fmtStrategyParams(tc.StrategyParams)},
		},
	)
}

// mergeStrategyParams parses --params values and merges them into the existing
// strategy_params map.
//
// Each entry in rawParams must be one of:
//   - "<strategy>:<key>=<float>"  — set a single parameter
//   - "<strategy>:clear"          — remove all params for that strategy (set to {})
func mergeStrategyParams(existing map[string]map[string]float64, rawParams []string) (map[string]map[string]float64, error) {
	// Deep-copy existing to avoid mutation.
	merged := make(map[string]map[string]float64)
	for strat, kv := range existing {
		merged[strat] = make(map[string]float64)
		for k, v := range kv {
			merged[strat][k] = v
		}
	}

	for _, raw := range rawParams {
		colonIdx := strings.Index(raw, ":")
		if colonIdx < 1 {
			return nil, fmt.Errorf("invalid --params value %q: expected format <strategy>:<key>=<value> or <strategy>:clear", raw)
		}
		strategy := raw[:colonIdx]
		rest := raw[colonIdx+1:]

		if rest == "clear" {
			merged[strategy] = map[string]float64{}
			continue
		}

		eqIdx := strings.Index(rest, "=")
		if eqIdx < 1 {
			return nil, fmt.Errorf("invalid --params value %q: expected format <strategy>:<key>=<value>", raw)
		}
		key := rest[:eqIdx]
		valStr := rest[eqIdx+1:]
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --params value %q: %q is not a valid number", raw, valStr)
		}

		if merged[strategy] == nil {
			merged[strategy] = make(map[string]float64)
		}
		merged[strategy][key] = val
	}

	return merged, nil
}

// fmtStrategyParams formats strategy_params as a compact string for table display.
// Truncates to 40 chars with … if longer. Returns "-" when empty.
func fmtStrategyParams(params map[string]map[string]float64) string {
	if len(params) == 0 {
		return "-"
	}

	strategies := make([]string, 0, len(params))
	for s := range params {
		strategies = append(strategies, s)
	}
	sort.Strings(strategies)

	var parts []string
	for _, strat := range strategies {
		kv := params[strat]
		if len(kv) == 0 {
			continue
		}
		keys := make([]string, 0, len(kv))
		for k := range kv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var kvParts []string
		for _, k := range keys {
			kvParts = append(kvParts, fmt.Sprintf("%s:%.2f", k, kv[k]))
		}
		parts = append(parts, fmt.Sprintf("%s:{%s}", strat, strings.Join(kvParts, ", ")))
	}

	if len(parts) == 0 {
		return "-"
	}

	result := strings.Join(parts, " ")
	if len(result) > 40 {
		runes := []rune(result)
		result = string(runes[:39]) + "…"
	}
	return result
}

// splitCSV splits a comma-separated string into a slice of trimmed strings.
// Returns nil (omitted in JSON) when s is empty — use splitCSVOrEmpty when the
// flag was explicitly set and an empty list should clear the field.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// splitCSVOrEmpty is like splitCSV but returns an empty (non-nil) slice when s
// is empty, so the field is serialised as [] rather than null.  Use this when
// an explicitly-passed empty flag should clear an array on the server.
func splitCSVOrEmpty(s string) []string {
	if s == "" {
		return []string{}
	}
	return splitCSV(s)
}

func init() {
	tradingListCmd.Flags().BoolVar(&tradingListEnabled, "enabled", false, "Only show enabled configs")

	tradingSetCmd.Flags().StringVar(&tsGranularity, "granularity", "", "Granularity (e.g. ONE_HOUR)")
	tradingSetCmd.Flags().StringVar(&tsLong, "long", "", "Long strategies (comma-separated)")
	tradingSetCmd.Flags().StringVar(&tsShort, "short", "", "Short strategies (comma-separated)")
	tradingSetCmd.Flags().StringVar(&tsSpot, "spot", "", "Spot strategies (comma-separated)")
	tradingSetCmd.Flags().IntVar(&tsLongLeverage, "long-leverage", 1, "Long leverage")
	tradingSetCmd.Flags().IntVar(&tsShortLeverage, "short-leverage", 1, "Short leverage")
	tradingSetCmd.Flags().BoolVar(&tsTrendFilter, "trend-filter", false, "Enable trend filter")
	tradingSetCmd.Flags().BoolVar(&tsNoTrendFilter, "no-trend-filter", false, "Disable trend filter")
	tradingSetCmd.Flags().BoolVar(&tsEnable, "enable", false, "Enable the config")
	tradingSetCmd.Flags().BoolVar(&tsDisable, "disable", false, "Disable the config")
	tradingSetCmd.Flags().StringArrayVar(&tsParams, "params", nil, "Per-strategy params: <strategy>:<key>=<value> or <strategy>:clear (repeatable)")

	tradingWipeCmd.Flags().BoolVar(&twConfirm, "confirm", false, "Required: confirm the wipe")
	tradingWipeCmd.Flags().StringVar(&twExchange, "exchange", "", "Restrict wipe to a single exchange")

	tradingCmd.AddCommand(tradingListCmd, tradingGetCmd, tradingSetCmd, tradingDeleteCmd, tradingWipeCmd)
	rootCmd.AddCommand(tradingCmd)
}
