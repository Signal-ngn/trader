package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	nats "github.com/nats-io/nats.go"

	"github.com/Signal-ngn/trader/internal/config"
)

// defaultNATSCreds holds embedded Synadia NGS credentials scoped to
// subscribe-only access. Copied from sn/cmd/sn/signals.go — same embedded JWT.
//
// Override at runtime: set SN_NATS_CREDS_FILE env var.
const defaultNATSCreds = `-----BEGIN NATS USER JWT-----
eyJ0eXAiOiJKV1QiLCJhbGciOiJlZDI1NTE5LW5rZXkifQ.eyJqdGkiOiJMTklBM1VUNk5VT1U0WlhPUkQyUEFKWkxSRE41UlFLRU9YVFVRU0VYRFdKRE80TzdXQTRBIiwiaWF0IjoxNzcxNjA4MTEzLCJpc3MiOiJBQUMzQlhCNE9VVVU2R0paT1pDNkpRQlBMMkxVS1BLSDJCRDNKVDdNWjRJSUpDNEdKM1lYV1VOMiIsIm5hbWUiOiJDTEkiLCJzdWIiOiJVREZTM0dZUVVGV1FCQlBPR1JSNExZSk9WN0hRNVNMVElMR1NPMkhLQlhPNEk2S1BPNjdNSEFPUiIsIm5hdHMiOnsicHViIjp7ImRlbnkiOlsiKiJdfSwic3ViIjp7fSwic3VicyI6LTEsImRhdGEiOi0xLCJwYXlsb2FkIjotMSwiaXNzdWVyX2FjY291bnQiOiJBRDdDNEpYTDVKR01MQ1hYTjVJR1BZWFNYRVNPUE1LWVdYSTM1UkpXRjNVSk9UWENCUENVR1dUNiIsInR5cGUiOiJ1c2VyIiwidmVyc2lvbiI6Mn19.f74k_pg8ZpW5uvdcwYonbNn7cniZwoWNCUPvZxJt70NWA5Izkyk-9U2wGpUVecyyOKdXjNK1IVSv2YpkR9_-DQ
------END NATS USER JWT------

-----BEGIN USER NKEY SEED-----
SUAJZ64KHDJ62K4YRMOCSO57HI6D5XX5MVH5WYPPAOZ2BJSET43GKMPN5M
------END USER NKEY SEED------`

const ngsURL = "tls://connect.ngs.global"

// SignalPayload is the NATS message payload for a trading signal.
// Mirrors strategy.StrategySignal in spot-canvas-app.
type SignalPayload struct {
	Strategy      string             `json:"strategy"`
	Product       string             `json:"product"`
	Exchange      string             `json:"exchange"`
	AccountID     string             `json:"account_id"` // target account; empty = all accounts (legacy)
	Action        string             `json:"action"`
	Market        string             `json:"market"`
	Leverage      int                `json:"leverage"`
	Price         float64            `json:"price"`
	Confidence    float64            `json:"confidence"`
	Reason        string             `json:"reason"`
	StopLoss      float64            `json:"stop_loss"`
	TakeProfit    float64            `json:"take_profit"`
	RiskReasoning string             `json:"risk_reasoning"`
	PositionPct   float64            `json:"position_pct"`
	IsExit        bool               `json:"is_exit"` // true when the strategy is closing an existing position
	Indicators    map[string]float64 `json:"indicators"`
	Forecast      *SignalForecast    `json:"forecast,omitempty"` // optional; populated by forecast-aware strategies (HTS)
	Timestamp     int64              `json:"timestamp"`          // Unix seconds
}

// SignalForecast mirrors strategy.SignalForecastPayload in spot-canvas-app.
// Consumers translate it into risk.ForecastSnapshot at the scoring boundary.
type SignalForecast struct {
	Valid        bool    `json:"valid"`
	ProbLong     float64 `json:"prob_long"`
	ProbShort    float64 `json:"prob_short"`
	ProbHold     float64 `json:"prob_hold"`
	MedianReturn float64 `json:"median_return"`
	Uncertainty  float64 `json:"uncertainty"`
	QuantileLow  float64 `json:"quantile_low"`
	QuantileHigh float64 `json:"quantile_high"`
	ConfirmH24   float64 `json:"confirm_h24"`
	Regime       Regime  `json:"regime"`
}

// Regime mirrors strategy.RegimePayload — the subset of hts.MarketRegime
// carried inside SignalForecast on the wire.
type Regime struct {
	State          string  `json:"state"`
	VolatilityMult float64 `json:"volatility_mult"`
	AllowLong      bool    `json:"allow_long"`
	AllowShort     bool    `json:"allow_short"`
	Confidence     float64 `json:"confidence"`
}

// TradingConfig mirrors the SignalNGN server model for a trading config.
//
// StrategyParamsLong and StrategyParamsShort are optional per-side overrides:
// when present, their keys win over the shared StrategyParams for the matching
// side. Resolution is per-key, not per-strategy — a side override that omits a
// key still inherits that key from the shared map. See effectiveStrategyParams.
type TradingConfig struct {
	ID                  int                           `json:"id"`
	AccountID           string                        `json:"account_id"`
	Exchange            string                        `json:"exchange"`
	ProductID           string                        `json:"product_id"`
	Granularity         string                        `json:"granularity"`
	StrategiesSpot      []string                      `json:"strategies_spot"`
	StrategiesLong      []string                      `json:"strategies_long"`
	StrategiesShort     []string                      `json:"strategies_short"`
	LongLeverage        int                           `json:"long_leverage"`
	ShortLeverage       int                           `json:"short_leverage"`
	Enabled             bool                          `json:"enabled"`
	StrategyParams      map[string]map[string]float64 `json:"strategy_params"`
	StrategyParamsLong  map[string]map[string]float64 `json:"strategy_params_long"`
	StrategyParamsShort map[string]map[string]float64 `json:"strategy_params_short"`
	MinConfidence       float64                       `json:"min_confidence"`
}

// sideFromAction returns "long" for BUY/SELL and "short" for SHORT/COVER.
// Empty string for unknown actions (defensive — callers should already have
// filtered by action).
func sideFromAction(action string) string {
	switch action {
	case "BUY", "SELL":
		return "long"
	case "SHORT", "COVER":
		return "short"
	default:
		return ""
	}
}

// effectiveStrategyParams resolves the params map for a given side and
// strategy base name. Per-key resolution order (earlier wins):
//  1. Side-specific override (StrategyParamsLong / StrategyParamsShort)
//  2. Shared StrategyParams
//
// Returns nil when neither side nor shared has any keys for the strategy.
// nil is a valid map for read-only lookups, so callers can index it directly.
func (tc *TradingConfig) effectiveStrategyParams(side, baseName string) map[string]float64 {
	var sideMap map[string]float64
	switch side {
	case "long":
		sideMap = tc.StrategyParamsLong[baseName]
	case "short":
		sideMap = tc.StrategyParamsShort[baseName]
	}
	shared := tc.StrategyParams[baseName]
	if len(sideMap) == 0 {
		return shared
	}
	if len(shared) == 0 {
		return sideMap
	}
	merged := make(map[string]float64, len(shared)+len(sideMap))
	for k, v := range shared {
		merged[k] = v
	}
	for k, v := range sideMap {
		merged[k] = v
	}
	return merged
}

// signalKey uniquely identifies a (exchange, product, granularity, strategy) tuple.
type signalKey struct {
	exchange    string
	product     string
	granularity string
	strategy    string
}

// signalAllowlist is the set of signal keys the user is allowed to trade.
type signalAllowlist map[signalKey]struct{}

// tradingConfigKey uniquely identifies a trading config by account and product.
type tradingConfigKey struct {
	accountID string
	productID string
}

// tradingConfigByProduct maps (accountID, productID) → TradingConfig for fast lookup.
type tradingConfigByProduct map[tradingConfigKey]*TradingConfig

// allows returns true when the given signal tuple matches the allowlist.
// Strategy matching uses prefix matching: "ml_xgboost+trend" matches "ml_xgboost".
func (a signalAllowlist) allows(exchange, product, granularity, strategy string) bool {
	if _, ok := a[signalKey{exchange, product, granularity, strategy}]; ok {
		return true
	}
	for i := len(strategy) - 1; i >= 0; i-- {
		if strategy[i] == '_' || strategy[i] == '+' {
			base := strategy[:i]
			if _, ok := a[signalKey{exchange, product, granularity, base}]; ok {
				return true
			}
		}
	}
	return false
}

// fetchAllowlist fetches enabled trading configs from the SN API and builds an allowlist.
// Also returns a map of product → TradingConfig for position sizing lookups.
func fetchAllowlist(ctx context.Context, cfg *config.Config) (signalAllowlist, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.TraderAPIURL+"/config/trading", nil)
	if err != nil {
		return nil, fmt.Errorf("build allowlist request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SNAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch trading configs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trading config API returned %d", resp.StatusCode)
	}

	var configs []TradingConfig
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return nil, fmt.Errorf("decode trading configs: %w", err)
	}

	allowlist := make(signalAllowlist)
	for _, tc := range configs {
		if !tc.Enabled {
			continue
		}
		allStrategies := append(append(tc.StrategiesLong, tc.StrategiesShort...), tc.StrategiesSpot...)
		for _, strat := range allStrategies {
			allowlist[signalKey{
				exchange:    tc.Exchange,
				product:     tc.ProductID,
				granularity: tc.Granularity,
				strategy:    strat,
			}] = struct{}{}
		}
	}
	return allowlist, nil
}

// fetchTradingConfigs fetches all enabled trading configs indexed by product ID.
func fetchTradingConfigs(ctx context.Context, cfg *config.Config) (tradingConfigByProduct, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.TraderAPIURL+"/config/trading", nil)
	if err != nil {
		return nil, fmt.Errorf("build config request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SNAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch trading configs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trading config API returned %d", resp.StatusCode)
	}

	var configs []TradingConfig
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return nil, fmt.Errorf("decode trading configs: %w", err)
	}

	m := make(tradingConfigByProduct)
	for i := range configs {
		tc := configs[i]
		if tc.Enabled {
			m[tradingConfigKey{accountID: tc.AccountID, productID: tc.ProductID}] = &tc
		}
	}
	return m, nil
}

// resolveNATSCreds returns the path to a NATS credentials file.
// If SN_NATS_CREDS_FILE is set, that path is used directly.
// Otherwise the embedded credentials are written to a temp file.
func resolveNATSCreds(cfg *config.Config) (string, error) {
	if cfg.SNNATSCredsFile != "" {
		return cfg.SNNATSCredsFile, nil
	}

	tmp, err := os.CreateTemp("", "engine-nats-creds-*.creds")
	if err != nil {
		return "", fmt.Errorf("create temp creds file: %w", err)
	}
	if _, err := tmp.WriteString(defaultNATSCreds); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write creds: %w", err)
	}
	tmp.Close()
	return tmp.Name(), nil
}

// buildSubject builds a NATS subscription subject.
func buildSubject(exchange, product, granularity, strategy string) string {
	parts := make([]string, 4)
	if exchange != "" {
		parts[0] = exchange
	} else {
		parts[0] = "*"
	}
	if product != "" {
		parts[1] = product
	} else {
		parts[1] = "*"
	}
	if granularity != "" {
		parts[2] = granularity
	} else {
		parts[2] = "*"
	}
	if strategy != "" {
		parts[3] = strategy
	} else {
		parts[3] = ">"
	}
	return "signals." + strings.Join(parts, ".")
}

// parseSubject extracts exchange, product, granularity, strategy from a signals subject.
func parseSubject(subj string) (exchange, product, granularity, strategy string) {
	parts := strings.SplitN(subj, ".", 5)
	if len(parts) < 5 {
		return "", "", "", ""
	}
	return parts[1], parts[2], parts[3], parts[4]
}

// fetchCurrentPrice fetches the latest close price for a product from the SN API.
// Returns 0 and a non-nil error if the price cannot be fetched.
// The SN price endpoint is GET {SN_API_URL}/prices/{exchange}/{product}?granularity=ONE_MINUTE.
func fetchCurrentPrice(ctx context.Context, cfg *config.Config, exchange, product string) (float64, error) {
	url := fmt.Sprintf("%s/prices/%s/%s?granularity=ONE_MINUTE", cfg.TraderAPIURL, exchange, product)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SNAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("price API returned %d", resp.StatusCode)
	}

	var price struct {
		Close float64 `json:"close"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&price); err != nil {
		return 0, err
	}
	return price.Close, nil
}

// startAllowlistRefresher runs a goroutine that re-fetches the allowlist every 5 minutes.
func (e *Engine) startAllowlistRefresher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			al, err := fetchAllowlist(ctx, e.cfg)
			if err != nil {
				e.logger.Warn().Err(err).Msg("failed to refresh signal allowlist")
				continue
			}
			e.allowlistMu.Lock()
			e.allowlist = al
			e.allowlistMu.Unlock()
			e.logger.Debug().Int("slots", len(al)).Msg("allowlist refreshed")
		}
	}
}

// runSignalLoop connects to Synadia NGS and subscribes to signals.
// Uses exponential backoff (10s → 5m) on connection failure.
func (e *Engine) runSignalLoop(ctx context.Context) {
	credsFile, err := resolveNATSCreds(e.cfg)
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to resolve NGS credentials — signal loop aborted")
		return
	}

	backoff := 10 * time.Second
	maxBackoff := 5 * time.Minute

	for {
		if ctx.Err() != nil {
			return
		}

		nc, err := nats.Connect(ngsURL,
			nats.UserCredentials(credsFile),
			nats.Name("trader-engine"),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(5*time.Second),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				e.logger.Info().Str("url", nc.ConnectedUrl()).Msg("reconnected to NGS")
			}),
			nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
				if err != nil {
					e.logger.Warn().Err(err).Msg("disconnected from NGS")
				}
			}),
		)
		if err != nil {
			e.logger.Warn().Err(err).Dur("retry_in", backoff).Msg("failed to connect to NGS, retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		e.ngsConn = nc
		e.logger.Info().Str("url", nc.ConnectedUrl()).Msg("connected to Synadia NGS")
		backoff = 10 * time.Second // reset on success

		sub, err := nc.Subscribe("signals.>", func(msg *nats.Msg) {
			e.handleSignal(ctx, msg)
		})
		if err != nil {
			e.logger.Error().Err(err).Msg("failed to subscribe to signals")
			nc.Close()
			continue
		}

		// Subscribe to 15M candles for conviction evaluation.
		if err := e.subscribeToCandleUpdates(ctx, nc); err != nil {
			e.logger.Warn().Err(err).Msg("failed to subscribe to 15M candles for conviction")
		}

		// Block until context is cancelled or connection drops.
		<-ctx.Done()
		_ = sub.Unsubscribe()
		nc.Close()
		return
	}
}

// resolveTargetAccounts returns the accounts a signal should be routed to.
//
// If signalAccountID is empty (legacy signals that predate the account_id
// field), all managed accounts are targeted and ok is true.
//
// If signalAccountID is non-empty, only that account is targeted. ok is false
// when the requested account is not in the managed list, indicating the signal
// should be dropped.
func resolveTargetAccounts(managed []string, signalAccountID string) (targets []string, ok bool) {
	if signalAccountID == "" {
		return managed, true
	}
	for _, a := range managed {
		if a == signalAccountID {
			return []string{signalAccountID}, true
		}
	}
	return nil, false
}

// handleSignal processes a single signal message from NGS.
func (e *Engine) handleSignal(ctx context.Context, msg *nats.Msg) {
	exchange, product, granularity, strategy := parseSubject(msg.Subject)
	if exchange == "" {
		return
	}

	// Parse payload first so we can log useful info.
	var signal SignalPayload
	if err := json.Unmarshal(msg.Data, &signal); err != nil {
		e.logger.Warn().Str("subject", msg.Subject).Msg("failed to unmarshal signal payload")
		return
	}

	logger := e.logger.With().
		Str("exchange", exchange).
		Str("product", product).
		Str("strategy", strategy).
		Str("action", signal.Action).
		Float64("price", signal.Price).
		Float64("confidence", signal.Confidence).
		Logger()

	// 1. Allowlist check.
	e.allowlistMu.RLock()
	al := e.allowlist
	e.allowlistMu.RUnlock()
	if !al.allows(exchange, product, granularity, strategy) {
		return // silent drop
	}

	// 2. Strategy prefix filter.
	if e.cfg.StrategyFilter != "" && !strings.HasPrefix(strategy, e.cfg.StrategyFilter) {
		return // silent drop
	}

	// 3. Stale signal check.
	// Signals carry the candle-close time as their timestamp. The shortest
	// configured granularity is FIVE_MINUTES, so a freshly-emitted signal is
	// already up to ~90 s old by the time the engine receives it (compute +
	// publish latency). Allow one full candle period (5 min) plus a 5-minute
	// processing buffer = 10 minutes total. Anything older is a replay or a
	// stale batch from a strategy that was offline.
	if signal.Timestamp > 0 {
		age := time.Since(time.Unix(signal.Timestamp, 0))
		if age > 10*time.Minute {
			logger.Warn().Dur("age", age).Msg("signal too old, dropping")
			return
		}
	}

	// 4. Global confidence floor for entry signals.
	// Per-config thresholds (min_confidence) are enforced later in handleOpenSignal
	// once the trading config is loaded. This 0.5 floor is a cheap early exit for
	// obviously low-quality signals before any DB or API work is done.
	if (signal.Action == "BUY" || signal.Action == "SHORT") && signal.Confidence < 0.5 {
		logger.Debug().Msg("signal confidence below global floor of 0.5, dropping")
		return
	}

	// Cache the signal price — used by the risk loop as current market price.
	// Also trigger immediate risk evaluation for all open positions on this symbol
	// (tick-level latency instead of waiting for the 30-second periodic ticker).
	if signal.Price > 0 {
		e.lastPriceMu.Lock()
		e.lastPrice[product] = signal.Price
		e.lastPriceMu.Unlock()
		go e.evaluateOpenPositionsForSymbol(ctx, product)
	}

	// Cache the forecast for forecast-aware strategies (HTS). Read back at the
	// 15M conviction Score() call and at position open to freeze the entry-time
	// baseline. Stored by value so the publisher's pointer lifecycle cannot race.
	// The wrapper captures the signal's candle-close timestamp so the conviction
	// loop can compute ForecastAge against the correct origin.
	if signal.Forecast != nil {
		cached := &cachedForecast{
			forecast:  *signal.Forecast,
			timestamp: time.Unix(signal.Timestamp, 0),
		}
		e.forecastMu.Lock()
		e.forecasts[forecastKey(exchange, product)] = cached
		e.forecastMu.Unlock()
	}

	// Build the set of accounts this signal targets.
	// If the signal carries an account_id, route only to that account (provided
	// it is in the engine's managed list). If account_id is absent (legacy
	// signals), fall back to routing to all managed accounts.
	targetAccounts, ok := resolveTargetAccounts(e.accounts, signal.AccountID)
	if !ok {
		logger.Debug().Str("account_id", signal.AccountID).Msg("signal account_id not in managed accounts, dropping")
		return
	}

	// Route to position engine for each target account.
	for _, accountID := range targetAccounts {
		// Per-account cooldown check (only for opening actions).
		if signal.Action == "BUY" || signal.Action == "SHORT" {
			key := cooldownKey{accountID: accountID, symbol: product, action: signal.Action}
			e.cooldownMu.Lock()
			expiry, active := e.cooldown[key]
			e.cooldownMu.Unlock()
			if active && time.Now().Before(expiry) {
				remaining := time.Until(expiry).Round(time.Second)
				logger.Debug().Str("account", accountID).Dur("remaining", remaining).Msg("cooldown active, dropping signal")
				continue
			}
		}
		e.signalBuf.Add(bufferedSignal{
			signal:    signal,
			product:   product,
			strategy:  strategy,
			accountID: accountID,
		})
	}
}
