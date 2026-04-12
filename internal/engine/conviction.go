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
}

func newConvictionManager(exitThreshold, tightenThreshold float64) *convictionManager {
	return &convictionManager{
		scorers:          make(map[string]*convictionState),
		exitThreshold:    exitThreshold,
		tightenThreshold: tightenThreshold,
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

		// Don't evaluate if scorer is cold
		if !cs.scorer.IsWarm() {
			e.logger.Debug().
				Str("symbol", ps.Symbol).
				Int("candles", cs.scorer.CandleCount()).
				Msg("conviction scorer warming up")
			recordConvictionWarmup(ps.Symbol, true)
			return
		}

		posCtx := risk.PositionContext{
			Side:       ps.Side,
			EntryPrice: ps.EntryPrice,
			EntryATR:   cs.entryATR,
			SignalAge:  time.Since(ps.OpenedAt),
		}

		score, signals := cs.scorer.Score(posCtx)
		recordConvictionScore(ps.Symbol, score, signals)
		recordConvictionWarmup(ps.Symbol, false)

		if score <= e.conviction.exitThreshold {
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
		} else if score <= e.conviction.tightenThreshold {
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

// warmScorer fetches 30 recent 15M candles from the platform API and feeds them to the scorer.
func (e *Engine) warmScorer(ctx context.Context, ps *PositionState) {
	key := posKey(ps.AccountID, ps.Symbol)

	exchange := e.exchangeForProduct(ps.Symbol)
	if exchange == "" {
		e.logger.Warn().Str("symbol", ps.Symbol).Msg("conviction warm: exchange unknown, skipping scorer")
		return
	}

	// Create scorer for this position, bound to the resolved exchange.
	cs := e.conviction.createScorer(key, exchange)

	candles, err := fetchWarmupCandles(ctx, e.cfg, exchange, ps.Symbol, 30)
	if err != nil {
		e.logger.Warn().Err(err).Str("symbol", ps.Symbol).Msg("conviction warm: API fetch failed, cold start")
		return
	}

	for _, c := range candles {
		cs.scorer.Update(c)
	}

	// Mark entry with ATR from the warmed-up scorer
	cs.entryATR = cs.scorer.MarkEntry()

	e.logger.Debug().
		Str("symbol", ps.Symbol).
		Int("candles_fed", len(candles)).
		Bool("warm", cs.scorer.IsWarm()).
		Msg("conviction scorer pre-warmed")
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
// Creates a ConvictionScorer and marks entry.
func (e *Engine) onConvictionPositionOpen(ps *PositionState) {
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
	cs.entryATR = cs.scorer.MarkEntry()
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
