package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Signal-ngn/risk"
	"github.com/Signal-ngn/trader/internal/config"
	nats "github.com/nats-io/nats.go"
)

// convictionCandleSubject is the NATS wildcard for 15M candles.
const convictionCandleSubject = "candles.*.*.FIFTEEN_MINUTES"

// forecastKey builds the map key used for the per-product forecast cache.
// Forecasts are product-level (one per HTS 4H evaluation) so the key deliberately
// excludes accountID — every account trading the same product shares the same
// forecast snapshot.
func forecastKey(exchange, product string) string {
	return exchange + "/" + product
}

// cachedForecast is the engine-local wrapper that pairs a wire-deserialised
// SignalForecast with the candle-close timestamp of the signal that produced
// it. The timestamp is what the risk library uses to compute ForecastAge at
// each 15M tick.
type cachedForecast struct {
	forecast  SignalForecast
	timestamp time.Time
}

// currentForecast returns a copy of the cached forecast for the given
// (exchange, product) pair, or nil when no forecast has been observed.
func (e *Engine) currentForecast(exchange, product string) *cachedForecast {
	e.forecastMu.RLock()
	defer e.forecastMu.RUnlock()
	f, ok := e.forecasts[forecastKey(exchange, product)]
	if !ok {
		return nil
	}
	out := *f
	return &out
}

// toForecastSnapshot translates a cached forecast into the risk library's
// ForecastSnapshot. A nil input yields a zero-value snapshot, which the
// scorer treats as "no forecast available" and disables all TFT/regime
// signals — preserving archived behaviour.
func toForecastSnapshot(c *cachedForecast) risk.ForecastSnapshot {
	if c == nil {
		return risk.ForecastSnapshot{}
	}
	return risk.ForecastSnapshot{
		Valid:           c.forecast.Valid,
		MedianReturnH48: c.forecast.MedianReturn,
		UncertaintyH48:  c.forecast.Uncertainty,
		QuantileLowH48:  c.forecast.QuantileLow,
		QuantileHighH48: c.forecast.QuantileHigh,
		ConfirmH24:      c.forecast.ConfirmH24,
		Timestamp:       c.timestamp,
	}
}

// toRegimeSnapshot translates the cached regime subset into the risk
// library's RegimeSnapshot. A nil input yields an empty-State snapshot,
// which disables the regime-adverse erosion signal.
func toRegimeSnapshot(c *cachedForecast) risk.RegimeSnapshot {
	if c == nil {
		return risk.RegimeSnapshot{}
	}
	return risk.RegimeSnapshot{
		State:          c.forecast.Regime.State,
		AllowLong:      c.forecast.Regime.AllowLong,
		AllowShort:     c.forecast.Regime.AllowShort,
		VolatilityMult: c.forecast.Regime.VolatilityMult,
	}
}

// candlePayload matches the JSON structure published by the ingestion server.
type candlePayload struct {
	Candle struct {
		Exchange  string    `json:"exchange"`
		ProductID string    `json:"product_id"`
		Timestamp time.Time `json:"timestamp"`
		Open      float64   `json:"open"`
		High      float64   `json:"high"`
		Low       float64   `json:"low"`
		Close     float64   `json:"close"`
		Volume    float64   `json:"volume"`
	} `json:"candle"`
	IsComplete bool `json:"is_complete"`
}

// convictionState holds the ConvictionScorer and entry ATR for a single position.
type convictionState struct {
	scorer   *risk.ConvictionScorer
	entryATR float64

	// Entry-time forecast baseline, frozen at position open from the cached
	// SignalForecast. Zero values (no cached forecast at open) disable the
	// tft_uncertainty_expansion erosion signal for this position's lifetime
	// and preserve archived scorer behaviour.
	entryUncertainty  float64
	entryMedianReturn float64

	// exchange is the authoritative exchange this scorer consumes candles from.
	// Messages from any other exchange are rejected to prevent cross-feed contamination
	// (e.g. binance/coinbase/kraken/okx all publishing the same 15M window with
	// slightly different closes, or a backfill republishing a stale candle).
	exchange string

	// lastCandleTime is the timestamp of the most recent candle fed to the scorer.
	// Candles with a timestamp <= this value are rejected (late / out-of-order / backfill).
	lastCandleTime time.Time
}

// shouldProcessCandle reports whether a candle with the given exchange and timestamp
// should be fed to this scorer. It rejects cross-exchange messages and stale/duplicate
// timestamps. Zero-valued exchange on the scorer disables the exchange check (for
// backwards compatibility with fallback code paths where exchange is unknown).
func (cs *convictionState) shouldProcessCandle(exchange string, ts time.Time) bool {
	if cs.exchange != "" && exchange != cs.exchange {
		return false
	}
	if !cs.lastCandleTime.IsZero() && !ts.After(cs.lastCandleTime) {
		return false
	}
	return true
}

// convictionManager manages per-position ConvictionScorers.
type convictionManager struct {
	mu      sync.RWMutex
	scorers map[string]*convictionState // keyed by posKey(accountID, symbol)

	exitThreshold    float64
	tightenThreshold float64

	// Grace-window threshold ramp. When both are > 0 and a position's cached
	// forecast is valid with ForecastAge < graceWindow, the exit/tighten
	// thresholds are scaled by graceFactor for that tick (via
	// risk.EffectiveThresholds). Zero disables the ramp — raw thresholds apply.
	graceWindow time.Duration
	graceFactor float64
}

func newConvictionManager(exitThreshold, tightenThreshold float64, graceWindow time.Duration, graceFactor float64) *convictionManager {
	return &convictionManager{
		scorers:          make(map[string]*convictionState),
		exitThreshold:    exitThreshold,
		tightenThreshold: tightenThreshold,
		graceWindow:      graceWindow,
		graceFactor:      graceFactor,
	}
}

// createScorer creates a new ConvictionScorer for a position, bound to the
// given exchange. Subsequent candle messages from other exchanges are ignored.
func (cm *convictionManager) createScorer(key, exchange string) *convictionState {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cs := &convictionState{
		scorer:   risk.NewConvictionScorer(),
		exchange: exchange,
	}
	cm.scorers[key] = cs
	return cs
}

// feedCandle feeds a candle to the scorer for key if it passes shouldProcessCandle.
// Returns the updated state (with lastCandleTime advanced) and true if accepted, or
// nil and false if the candle was rejected (or no scorer exists for key).
func (cm *convictionManager) feedCandle(key string, payload candlePayload) (*convictionState, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cs, ok := cm.scorers[key]
	if !ok {
		return nil, false
	}
	if !cs.shouldProcessCandle(payload.Candle.Exchange, payload.Candle.Timestamp) {
		return nil, false
	}
	cs.scorer.Update(risk.OHLCV{
		Open:   payload.Candle.Open,
		High:   payload.Candle.High,
		Low:    payload.Candle.Low,
		Close:  payload.Candle.Close,
		Volume: payload.Candle.Volume,
	})
	cs.lastCandleTime = payload.Candle.Timestamp
	return cs, true
}

// markEntry captures the entry ATR for a position's scorer.
func (cm *convictionManager) markEntry(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cs, ok := cm.scorers[key]; ok {
		cs.entryATR = cs.scorer.MarkEntry()
	}
}

// destroyScorer removes the scorer for a closed position.
func (cm *convictionManager) destroyScorer(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.scorers, key)
}

// getScorer returns the scorer state for a position, or nil.
func (cm *convictionManager) getScorer(key string) *convictionState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.scorers[key]
}

// enabled returns true if conviction thresholds are configured.
func (cm *convictionManager) enabled() bool {
	return cm.exitThreshold > 0
}

// subscribeToCandleUpdates subscribes to 15M candle NATS subjects.
// It feeds candles to the appropriate ConvictionScorer and evaluates conviction.
func (e *Engine) subscribeToCandleUpdates(ctx context.Context, nc *nats.Conn) error {
	if e.conviction == nil || !e.conviction.enabled() {
		return nil
	}

	_, err := nc.Subscribe(convictionCandleSubject, func(msg *nats.Msg) {
		e.handleCandleMessage(ctx, msg)
	})
	return err
}

// handleCandleMessage processes a 15M candle from NATS.
func (e *Engine) handleCandleMessage(ctx context.Context, msg *nats.Msg) {
	var payload candlePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		e.logger.Debug().Err(err).Msg("failed to parse candle message")
		return
	}

	if !payload.IsComplete {
		return
	}

	product := payload.Candle.ProductID

	// Find open positions for this product and feed the candle to their scorers.
	e.posStateMu.RLock()
	var matching []*PositionState
	for _, ps := range e.posState {
		if ps.Symbol == product {
			matching = append(matching, ps)
		}
	}
	e.posStateMu.RUnlock()

	if len(matching) == 0 {
		return
	}

	for _, ps := range matching {
		key := posKey(ps.AccountID, ps.Symbol)

		// feedCandle filters out messages from other exchanges and stale/duplicate
		// timestamps, and only feeds the scorer if the candle is accepted.
		cs, accepted := e.conviction.feedCandle(key, payload)
		if !accepted {
			continue
		}
		ohlcv := risk.OHLCV{
			Open:   payload.Candle.Open,
			High:   payload.Candle.High,
			Low:    payload.Candle.Low,
			Close:  payload.Candle.Close,
			Volume: payload.Candle.Volume,
		}

		// Don't evaluate if scorer is cold.
		if !cs.scorer.IsWarm() {
			count := cs.scorer.CandleCount()
			recordConvictionWarmup(ps.Symbol, true)
			// Emit an INFO milestone at halfway and one-off-from-warm so stalls
			// are visible without enabling DEBUG globally.
			if count == 13 || count == 25 {
				e.logger.Info().
					Str("symbol", ps.Symbol).
					Str("exchange", cs.exchange).
					Int("candles", count).
					Msg("conviction scorer warming up")
			} else {
				e.logger.Debug().
					Str("symbol", ps.Symbol).
					Int("candles", count).
					Msg("conviction scorer warming up")
			}
			continue
		}

		cached := e.currentForecast(cs.exchange, ps.Symbol)
		forecastSnap := toForecastSnapshot(cached)
		regimeSnap := toRegimeSnapshot(cached)
		var forecastAge time.Duration
		if cached != nil && !cached.timestamp.IsZero() {
			forecastAge = payload.Candle.Timestamp.Sub(cached.timestamp)
		}

		posCtx := risk.PositionContext{
			Side:              ps.Side,
			EntryPrice:        ps.EntryPrice,
			EntryATR:          cs.entryATR,
			SignalAge:         time.Since(ps.OpenedAt),
			Forecast:          forecastSnap,
			Regime:            regimeSnap,
			ForecastAge:       forecastAge,
			EntryUncertainty:  cs.entryUncertainty,
			EntryMedianReturn: cs.entryMedianReturn,
			GraceWindow:       e.conviction.graceWindow,
		}

		score, signals := cs.scorer.Score(posCtx)
		recordConvictionScore(ps.Symbol, score, signals)
		recordConvictionWarmup(ps.Symbol, false)

		effectiveExit, effectiveTighten := risk.EffectiveThresholds(
			posCtx, e.conviction.exitThreshold, e.conviction.tightenThreshold, e.conviction.graceFactor,
		)

		if score <= effectiveExit {
			// Log at INFO
			signalNames := make([]string, len(signals))
			for i, s := range signals {
				signalNames[i] = s.Name
			}
			e.logger.Info().
				Str("symbol", ps.Symbol).
				Str("side", ps.Side).
				Float64("score", score).
				Strs("signals", signalNames).
				Str("action", "exit").
				Msg("conviction loss triggered exit")
			recordConvictionAction(ps.Symbol, "exit")

			// Guard against double-close
			e.posStateMu.Lock()
			psInMap, exists := e.posState[key]
			if !exists || psInMap.Closing {
				e.posStateMu.Unlock()
				continue
			}
			psInMap.Closing = true
			e.posStateMu.Unlock()

			e.executeCloseTrade(ctx, ps, ohlcv.Close, "algorithmic_conviction_loss")
		} else if score <= effectiveTighten {
			// Tighten stop
			e.logger.Info().
				Str("symbol", ps.Symbol).
				Str("side", ps.Side).
				Float64("score", score).
				Str("action", "tighten").
				Msg("conviction degraded — tightening stop")
			recordConvictionAction(ps.Symbol, "tighten")

			e.tightenStop(ctx, ps, ohlcv.Close)
		} else {
			e.logger.Debug().
				Str("symbol", ps.Symbol).
				Float64("score", score).
				Msg("conviction healthy")
		}
	}
}

// tightenStop reduces the stop-loss distance by 50% from current price.
func (e *Engine) tightenStop(ctx context.Context, ps *PositionState, currentPrice float64) {
	e.posStateMu.Lock()
	psInMap, exists := e.posState[posKey(ps.AccountID, ps.Symbol)]
	if !exists || psInMap.StopLoss <= 0 {
		e.posStateMu.Unlock()
		return
	}

	dist := math.Abs(psInMap.EntryPrice - psInMap.StopLoss)
	tightened := dist * 0.5

	if psInMap.Side == "long" {
		newSL := currentPrice - tightened
		if newSL > psInMap.StopLoss {
			psInMap.StopLoss = newSL
		}
	} else {
		newSL := currentPrice + tightened
		if psInMap.StopLoss == 0 || newSL < psInMap.StopLoss {
			psInMap.StopLoss = newSL
		}
	}
	e.posStateMu.Unlock()

	// Persist updated state
	tenantID := e.tenantID()
	dbState := &EnginePositionState{
		ID:           ps.ID,
		AccountID:    ps.AccountID,
		Symbol:       ps.Symbol,
		MarketType:   ps.MarketType,
		PeakPrice:    ps.PeakPrice,
		TrailingStop: ps.TrailingStop,
	}
	if err := e.repo.UpdatePositionState(ctx, tenantID, dbState); err != nil {
		e.logger.Warn().Err(err).Str("symbol", ps.Symbol).Msg("failed to persist stop tightening")
	}
}

// preWarmConvictionScorers fetches recent 15M candles for open positions and warms scorers.
func (e *Engine) preWarmConvictionScorers(ctx context.Context) {
	if e.conviction == nil || !e.conviction.enabled() {
		return
	}

	e.posStateMu.RLock()
	states := make([]*PositionState, 0, len(e.posState))
	for _, ps := range e.posState {
		states = append(states, ps)
	}
	e.posStateMu.RUnlock()

	if len(states) == 0 {
		return
	}

	// Warm scorers concurrently
	var wg sync.WaitGroup
	for _, ps := range states {
		wg.Add(1)
		go func(ps *PositionState) {
			defer wg.Done()
			e.warmScorer(ctx, ps)
		}(ps)
	}
	wg.Wait()

	e.logger.Info().Int("positions", len(states)).Msg("pre-warmed conviction scorers")
}

// warmScorer creates a scorer for the position and pre-warms it from the platform API.
// Used at engine startup to restore scorers for already-open positions.
func (e *Engine) warmScorer(ctx context.Context, ps *PositionState) {
	key := posKey(ps.AccountID, ps.Symbol)

	exchange := e.exchangeForProduct(ps.Symbol)
	if exchange == "" {
		e.logger.Warn().Str("symbol", ps.Symbol).Msg("conviction warm: exchange unknown, skipping scorer")
		return
	}

	// Create scorer for this position, bound to the resolved exchange.
	cs := e.conviction.createScorer(key, exchange)

	candlesFed := e.prewarmScorer(ctx, cs, exchange, ps.Symbol)

	// Mark entry with ATR from the (possibly) warmed-up scorer.
	cs.entryATR = cs.scorer.MarkEntry()
	e.markEntryForecast(cs, exchange, ps.Symbol)

	// Emit the warm-up gauge immediately so Grafana can distinguish a scorer
	// that exists-but-isn't-warm from one that was never created at all.
	recordConvictionWarmup(ps.Symbol, !cs.scorer.IsWarm())

	e.logger.Info().
		Str("symbol", ps.Symbol).
		Str("exchange", exchange).
		Int("candles_fed", candlesFed).
		Bool("warm", cs.scorer.IsWarm()).
		Float64("entry_atr", cs.entryATR).
		Float64("entry_uncertainty", cs.entryUncertainty).
		Float64("entry_median_return", cs.entryMedianReturn).
		Msg("conviction scorer initialized (startup)")
}

// markEntryForecast freezes the entry-time forecast baseline on the scorer.
// Reads the current cached forecast (nil-safe) and calls
// scorer.MarkEntryForecast, then copies the captured values onto the
// convictionState so handleCandleMessage can read them without taking the
// engine-level forecast lock on every tick.
func (e *Engine) markEntryForecast(cs *convictionState, exchange, product string) {
	cached := e.currentForecast(exchange, product)
	snap := toForecastSnapshot(cached)
	cs.scorer.MarkEntryForecast(snap)
	cs.entryUncertainty, cs.entryMedianReturn = cs.scorer.EntryForecast()
}

// prewarmScorer fetches 30 recent 15M candles from the platform API and feeds
// them into an existing scorer. Returns the number of candles actually fed
// (0 if the API call failed — the scorer will be cold and warm up from live
// NATS candles instead).
func (e *Engine) prewarmScorer(ctx context.Context, cs *convictionState, exchange, product string) int {
	candles, err := e.fetchWarmupCandlesFn(ctx, e.cfg, exchange, product, 30)
	if err != nil {
		e.logger.Warn().Err(err).Str("symbol", product).Msg("conviction warm: API fetch failed, cold start")
		return 0
	}
	for _, c := range candles {
		cs.scorer.Update(c)
	}
	return len(candles)
}

// fetchWarmupCandles fetches recent 15M candles from the platform API.
func fetchWarmupCandles(ctx context.Context, cfg *config.Config, exchange, product string, limit int) ([]risk.OHLCV, error) {
	url := fmt.Sprintf("%s/candles/%s/%s?granularity=FIFTEEN_MINUTES&limit=%d",
		cfg.TraderAPIURL, exchange, product, limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.SNAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("candles API returned %d", resp.StatusCode)
	}

	var rawCandles []struct {
		Open   float64 `json:"open"`
		High   float64 `json:"high"`
		Low    float64 `json:"low"`
		Close  float64 `json:"close"`
		Volume float64 `json:"volume"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rawCandles); err != nil {
		return nil, err
	}

	result := make([]risk.OHLCV, len(rawCandles))
	for i, c := range rawCandles {
		result[i] = risk.OHLCV{
			Open: c.Open, High: c.High, Low: c.Low,
			Close: c.Close, Volume: c.Volume,
		}
	}
	return result, nil
}

// onPositionOpen is called when a new position is opened.
// Creates a ConvictionScorer, pre-warms it from the platform API, and captures
// entry ATR. Pre-warming is synchronous so the scorer can evaluate conviction
// on the very next 15M candle instead of waiting ~6.5h for live candles to
// reach the 26-bar warm-up threshold.
func (e *Engine) onConvictionPositionOpen(ctx context.Context, ps *PositionState) {
	if e.conviction == nil || !e.conviction.enabled() {
		return
	}
	exchange := e.exchangeForProduct(ps.Symbol)
	if exchange == "" {
		e.logger.Warn().Str("symbol", ps.Symbol).Msg("conviction: exchange unknown, skipping scorer init")
		return
	}
	key := posKey(ps.AccountID, ps.Symbol)
	cs := e.conviction.createScorer(key, exchange)

	candlesFed := e.prewarmScorer(ctx, cs, exchange, ps.Symbol)
	cs.entryATR = cs.scorer.MarkEntry()
	e.markEntryForecast(cs, exchange, ps.Symbol)

	recordConvictionWarmup(ps.Symbol, !cs.scorer.IsWarm())

	e.logger.Info().
		Str("symbol", ps.Symbol).
		Str("exchange", exchange).
		Int("candles_fed", candlesFed).
		Bool("warm", cs.scorer.IsWarm()).
		Float64("entry_atr", cs.entryATR).
		Float64("entry_uncertainty", cs.entryUncertainty).
		Float64("entry_median_return", cs.entryMedianReturn).
		Msg("conviction scorer initialized (runtime open)")
}

// onPositionClose is called when a position is closed.
// Destroys the ConvictionScorer.
func (e *Engine) onConvictionPositionClose(accountID, symbol string) {
	if e.conviction == nil || !e.conviction.enabled() {
		return
	}
	e.conviction.destroyScorer(posKey(accountID, symbol))
	cleanupConvictionMetrics(symbol)
}

// parseCandleSubject extracts exchange and product from a candle NATS subject.
// Format: candles.{exchange}.{product}.{granularity}
func parseCandleSubject(subject string) (exchange, product string) {
	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 4 {
		return "", ""
	}
	return parts[1], parts[2]
}
