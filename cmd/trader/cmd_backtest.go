package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// BacktestJob mirrors the job record returned by POST /backtests and GET /jobs/{id}.
type BacktestJob struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	ResultID    *int       `json:"result_id"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// BacktestMetrics mirrors the server model.
type BacktestMetrics struct {
	TotalReturn   float64 `json:"total_return"`
	WinRate       float64 `json:"win_rate"`
	NumTrades     int     `json:"num_trades"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	ProfitFactor  float64 `json:"profit_factor"`
	AvgWin        float64 `json:"avg_win"`
	AvgLoss       float64 `json:"avg_loss"`
	MaxConsecLoss int     `json:"max_consec_loss"`
}

// BacktestFuturesMetrics mirrors the server model.
type BacktestFuturesMetrics struct {
	TotalFundingCost        float64 `json:"total_funding_cost"`
	NearLiquidationCount    int     `json:"near_liquidation_count"`
	MaxLiquidationProximity float64 `json:"max_liquidation_proximity"`
}

// BacktestResult mirrors the server model.
type BacktestResult struct {
	ID             int                    `json:"id"`
	Exchange       string                 `json:"exchange"`
	ProductID      string                 `json:"product_id"`
	Strategy       string                 `json:"strategy"`
	Granularity    string                 `json:"granularity"`
	MarketMode     string                 `json:"market_mode"`
	TrendFilter    bool                   `json:"trend_filter"`
	Leverage       int                    `json:"leverage"`
	Params         map[string]float64     `json:"params"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	CandleCount    int                    `json:"candle_count"`
	Metrics        BacktestMetrics        `json:"metrics"`
	FuturesMetrics BacktestFuturesMetrics `json:"futures_metrics"`
	CreatedAt      time.Time              `json:"created_at"`
}

// BacktestListResponse mirrors the server model.
type BacktestListResponse struct {
	Results []BacktestResult `json:"results"`
	Total   int              `json:"total"`
}

var backtestCmd = &cobra.Command{
	Use:   "backtest",
	Short: "Run and query backtests",
}

// ---- run ----

var (
	btExchange    string
	btProduct     string
	btStrategy    string
	btGranularity string
	btMode        string
	btStart       string
	btEnd         string
	btLeverage    int
	btTrendFilter bool
	btParams      []string
	btNoWait      bool
)

var backtestRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Submit a backtest and wait for results (use --no-wait to return immediately)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if btExchange == "" || btProduct == "" || btStrategy == "" || btGranularity == "" {
			return fmt.Errorf("--exchange, --product, --strategy, and --granularity are required")
		}
		c := newPlatformClient()
		body := map[string]any{
			"exchange":     btExchange,
			"product_id":   btProduct,
			"strategy":     btStrategy,
			"granularity":  btGranularity,
			"market_mode":  btMode,
			"trend_filter": btTrendFilter,
		}
		if btStart != "" {
			body["start_date"] = btStart
		}
		if btEnd != "" {
			body["end_date"] = btEnd
		}
		if btLeverage > 0 {
			body["leverage"] = btLeverage
		}
		if len(btParams) > 0 {
			params, err := parseParams(btParams)
			if err != nil {
				return fmt.Errorf("--params: %w", err)
			}
			body["params"] = params
		}

		fmt.Println("Submitting backtest...")
		var job BacktestJob
		if err := c.Post(c.apiURL("/backtests"), body, &job); err != nil {
			return err
		}

		useJSON, _ := cmd.Flags().GetBool("json")

		if btNoWait {
			fmt.Printf("Job ID: %s  Status: %s\n", job.ID, job.Status)
			fmt.Printf("Poll:   trader backtest job %s\n", job.ID)
			return nil
		}

		// Poll until completed or failed.
		fmt.Printf("Job ID: %s  Waiting", job.ID)
		pollInterval := 2 * time.Second
		for {
			time.Sleep(pollInterval)
			if err := c.Get(c.apiURL("/jobs/"+job.ID), &job); err != nil {
				fmt.Println()
				return fmt.Errorf("polling job: %w", err)
			}
			switch job.Status {
			case "completed":
				fmt.Println(" done.")
			case "failed":
				fmt.Println(" failed.")
				return fmt.Errorf("backtest failed: %s", job.Error)
			default:
				fmt.Print(".")
				continue
			}
			break
		}

		if job.ResultID == nil {
			return fmt.Errorf("job completed but result_id is missing")
		}

		var result BacktestResult
		if err := c.Get(c.apiURL(fmt.Sprintf("/backtests/%d", *job.ResultID)), &result); err != nil {
			return err
		}
		if useJSON {
			return PrintJSON(result)
		}
		printBacktestResult(result)
		return nil
	},
}

// ---- list ----

var (
	btListExchange string
	btListProduct  string
	btListStrategy string
	btListLimit    int
	btListSort     string
)

var backtestListCmd = &cobra.Command{
	Use:   "list",
	Short: "List backtest results",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		q := url.Values{}
		if btListExchange != "" {
			q.Set("exchange", btListExchange)
		}
		if btListProduct != "" {
			q.Set("product_id", btListProduct)
		}
		if btListStrategy != "" {
			q.Set("strategy", btListStrategy)
		}
		if btListLimit == 0 {
			q.Set("limit", "1000000")
		} else {
			q.Set("limit", strconv.Itoa(btListLimit))
		}

		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/backtests", q))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}

		var resp BacktestListResponse
		if err := c.Get(c.apiURL("/backtests", q), &resp); err != nil {
			return err
		}

		switch btListSort {
		case "date":
			sort.Slice(resp.Results, func(i, j int) bool {
				return resp.Results[i].CreatedAt.After(resp.Results[j].CreatedAt)
			})
		case "winrate":
			sort.Slice(resp.Results, func(i, j int) bool {
				return resp.Results[i].Metrics.WinRate > resp.Results[j].Metrics.WinRate
			})
		default:
			return fmt.Errorf("unknown sort option %q: use \"date\" or \"winrate\"", btListSort)
		}

		rows := make([][]string, len(resp.Results))
		for i, r := range resp.Results {
			rows[i] = []string{
				strconv.Itoa(r.ID),
				r.Exchange,
				r.ProductID,
				r.Strategy,
				r.Granularity,
				fmt.Sprintf("%.2f%%", r.Metrics.TotalReturn),
				fmt.Sprintf("%.2f%%", r.Metrics.WinRate),
				strconv.Itoa(r.Metrics.NumTrades),
				r.CreatedAt.Format("2006-01-02"),
				fmtBacktestParams(r.Params),
			}
		}
		PrintTable([]string{"ID", "EXCHANGE", "PRODUCT", "STRATEGY", "GRANULARITY", "RETURN", "WIN RATE", "TRADES", "DATE", "PARAMS"}, rows)
		return nil
	},
}

// ---- get ----

var backtestGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a backtest result by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		useJSON, _ := cmd.Flags().GetBool("json")
		if useJSON {
			_, raw, err := c.GetRaw(c.apiURL("/backtests/" + args[0]))
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		}
		var result BacktestResult
		if err := c.Get(c.apiURL("/backtests/"+args[0]), &result); err != nil {
			return err
		}
		printBacktestResult(result)
		return nil
	},
}

// ---- job (poll) ----

var backtestJobCmd = &cobra.Command{
	Use:   "job <job-id>",
	Short: "Poll a backtest job status; prints result when completed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newPlatformClient()
		useJSON, _ := cmd.Flags().GetBool("json")
		var job BacktestJob
		if err := c.Get(c.apiURL("/jobs/"+args[0]), &job); err != nil {
			return err
		}
		switch job.Status {
		case "completed":
			if job.ResultID == nil {
				return fmt.Errorf("job completed but result_id is missing")
			}
			var result BacktestResult
			if err := c.Get(c.apiURL(fmt.Sprintf("/backtests/%d", *job.ResultID)), &result); err != nil {
				return err
			}
			if useJSON {
				return PrintJSON(result)
			}
			printBacktestResult(result)
		case "failed":
			return fmt.Errorf("backtest failed: %s", job.Error)
		default:
			if useJSON {
				return PrintJSON(job)
			}
			fmt.Printf("Job ID: %s  Status: %s\n", job.ID, job.Status)
			fmt.Printf("Poll:   trader backtest job %s\n", job.ID)
		}
		return nil
	},
}

// ---- shared helpers ----

// printBacktestResult renders a full backtest result table, including
// futures-specific rows when market_mode is futures-long or futures-short.
func printBacktestResult(r BacktestResult) {
	rows := [][]string{
		{"ID", strconv.Itoa(r.ID)},
		{"Exchange", r.Exchange},
		{"Product", r.ProductID},
		{"Strategy", r.Strategy},
		{"Granularity", r.Granularity},
		{"Market Mode", r.MarketMode},
		{"Trend Filter", fmtBool(r.TrendFilter)},
		{"Leverage", fmtLeverage(r.Leverage)},
		{"Params", fmtBacktestParams(r.Params)},
		{"Candles", strconv.Itoa(r.CandleCount)},
		{"Total Return", fmt.Sprintf("%.2f%%", r.Metrics.TotalReturn)},
		{"Win Rate", fmt.Sprintf("%.2f%%", r.Metrics.WinRate)},
		{"Max Drawdown", fmt.Sprintf("%.2f%%", r.Metrics.MaxDrawdown)},
		{"Sharpe Ratio", fmtFloat(r.Metrics.SharpeRatio)},
		{"Profit Factor", fmtFloat(r.Metrics.ProfitFactor)},
		{"Trades", strconv.Itoa(r.Metrics.NumTrades)},
		{"Avg Win", fmt.Sprintf("%.2f%%", r.Metrics.AvgWin)},
		{"Avg Loss", fmt.Sprintf("%.2f%%", r.Metrics.AvgLoss)},
		{"Max Consec Losses", strconv.Itoa(r.Metrics.MaxConsecLoss)},
	}
	if r.MarketMode == "futures-long" || r.MarketMode == "futures-short" {
		rows = append(rows,
			[]string{"Funding Cost", fmt.Sprintf("%.4f%%", r.FuturesMetrics.TotalFundingCost)},
			[]string{"Near Liquidations", strconv.Itoa(r.FuturesMetrics.NearLiquidationCount)},
			[]string{"Min Liq Distance", fmt.Sprintf("%.2f%%", r.FuturesMetrics.MaxLiquidationProximity)},
		)
	}
	rows = append(rows, []string{"Created", r.CreatedAt.Format(time.RFC3339)})
	PrintTable([]string{"FIELD", "VALUE"}, rows)
}

// parseParams parses a slice of "key=value" strings into a map[string]float64.
func parseParams(raw []string) (map[string]float64, error) {
	out := make(map[string]float64, len(raw))
	for _, s := range raw {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid param %q: expected key=value", s)
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %w", k, err)
		}
		out[k] = f
	}
	return out, nil
}

// fmtBacktestParams formats a flat params map as a compact string for table display.
func fmtBacktestParams(params map[string]float64) string {
	if len(params) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%.2f", k, params[k]))
	}
	result := strings.Join(parts, " ")
	if len(result) > 40 {
		runes := []rune(result)
		result = string(runes[:39]) + "…"
	}
	return result
}

func init() {
	backtestRunCmd.Flags().StringVar(&btExchange, "exchange", "", "Exchange (required)")
	backtestRunCmd.Flags().StringVar(&btProduct, "product", "", "Product ID (required)")
	backtestRunCmd.Flags().StringVar(&btStrategy, "strategy", "", "Strategy name (required)")
	backtestRunCmd.Flags().StringVar(&btGranularity, "granularity", "", "Granularity e.g. ONE_HOUR (required)")
	backtestRunCmd.Flags().StringVar(&btMode, "mode", "spot", "Market mode: spot, futures-long, futures-short")
	backtestRunCmd.Flags().StringVar(&btStart, "start", "", "Start date YYYY-MM-DD (default: 1 year ago)")
	backtestRunCmd.Flags().StringVar(&btEnd, "end", "", "End date YYYY-MM-DD (default: today)")
	backtestRunCmd.Flags().IntVar(&btLeverage, "leverage", 0, "Leverage (futures modes)")
	backtestRunCmd.Flags().BoolVar(&btTrendFilter, "trend-filter", false, "Enable trend filter")
	backtestRunCmd.Flags().BoolVar(&btNoWait, "no-wait", false, "Return immediately after submitting; print poll command")
	backtestRunCmd.Flags().StringArrayVar(&btParams, "params", nil, "Strategy param overrides, e.g. --params confidence=0.80")

	backtestListCmd.Flags().StringVar(&btListExchange, "exchange", "", "Filter by exchange")
	backtestListCmd.Flags().StringVar(&btListProduct, "product", "", "Filter by product ID")
	backtestListCmd.Flags().StringVar(&btListStrategy, "strategy", "", "Filter by strategy")
	backtestListCmd.Flags().IntVar(&btListLimit, "limit", 20, "Number of results to show (0 = all)")
	backtestListCmd.Flags().StringVar(&btListSort, "sort", "date", "Sort order: date (newest first) or winrate (highest first)")

	backtestCmd.AddCommand(backtestRunCmd, backtestListCmd, backtestGetCmd, backtestJobCmd)
	rootCmd.AddCommand(backtestCmd)
}
