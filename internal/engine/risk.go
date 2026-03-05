package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/Signal-ngn/risk"
	"github.com/Signal-ngn/trader/internal/store"
)

const (
	riskLoopInterval = 30 * time.Second
	defaultSLPct     = 0.04 // 4% fallback SL distance used by risk library when SL=0
)

// exchangeForProduct returns the exchange name for a product by scanning the
// in-memory allowlist. Returns "" if the product is not found.
func (e *Engine) exchangeForProduct(product string) string {
	e.allowlistMu.RLock()
	defer e.allowlistMu.RUnlock()
	for key := range e.allowlist {
		if key.product == product {
			return key.exchange
		}
	}
	return ""
}

// startRiskLoop runs the risk management loop every 30 seconds.
// This acts as a fallback for products with infrequent price signals.
func (e *Engine) startRiskLoop(ctx context.Context) {
	ticker := time.NewTicker(riskLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.evaluatePositions(ctx); err != nil {
				e.logger.Error().Err(err).Msg("risk loop evaluation failed")
			}
		}
	}
}

// evaluatePositions reconciles position state and evaluates risk for all managed accounts.
func (e *Engine) evaluatePositions(ctx context.Context) error {
	// Kill switch — still allow closes, just log.
	if e.killSwitchActive() {
		e.logger.Warn().Str("file", e.cfg.KillSwitchFile).Msg("kill switch active — skipping new opens in risk loop")
	}

	// Build a set of posKey(accountID, symbol) that have open ledger positions
	// across all managed accounts.
	openKeys := make(map[string]bool)
	for _, accountID := range e.accounts {
		openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, accountID)
		if err != nil {
			return fmt.Errorf("list open positions for %s: %w", accountID, err)
		}
		for _, p := range openPositions {
			openKeys[posKey(accountID, p.Symbol)] = true
		}
	}

	// Prune orphaned engine_position_state rows (position closed externally).
	tenantID := e.tenantID()
	e.posStateMu.Lock()
	for key := range e.posState {
		if !openKeys[key] {
			ps := e.posState[key]
			e.logger.Info().Str("account", ps.AccountID).Str("symbol", ps.Symbol).Msg("pruning orphaned position state")
			if err := e.repo.DeletePositionState(ctx, tenantID, ps.Symbol, ps.MarketType, ps.AccountID); err != nil {
				e.logger.Warn().Err(err).Str("key", key).Msg("failed to delete orphaned position state")
			}
			delete(e.posState, key)
			e.conflictMu.Lock()
			delete(e.conflict, key)
			e.conflictMu.Unlock()
		}
	}
	e.posStateMu.Unlock()

	// Evaluate each position with engine state.
	e.posStateMu.RLock()
	states := make([]*PositionState, 0, len(e.posState))
	for _, ps := range e.posState {
		states = append(states, ps)
	}
	e.posStateMu.RUnlock()

	for _, ps := range states {
		e.evaluatePosition(ctx, ps)
	}

	return nil
}

// evaluateOpenPositionsForSymbol evaluates exit conditions for all open positions
// whose Symbol matches product. Called on every incoming price signal to provide
// tick-level risk evaluation latency.
func (e *Engine) evaluateOpenPositionsForSymbol(ctx context.Context, product string) {
	e.posStateMu.RLock()
	var matching []*PositionState
	for _, ps := range e.posState {
		if ps.Symbol == product {
			matching = append(matching, ps)
		}
	}
	e.posStateMu.RUnlock()

	for _, ps := range matching {
		e.evaluatePosition(ctx, ps)
	}
}

// evaluatePosition applies all risk rules to a single position via the risk library.
//
// Price resolution order:
//  1. Last price seen in a received NGS signal for this symbol (updated live as signals arrive).
//  2. SN price API fetch (GET /prices/{exchange}/{product}) — used when no signal has arrived
//     since startup or since the last risk loop tick.
//  3. Skip evaluation for this tick if neither source is available (logs a warning).
func (e *Engine) evaluatePosition(ctx context.Context, ps *PositionState) {
	tenantID := e.tenantID()

	// 1. Try cached signal price.
	e.lastPriceMu.RLock()
	currentPrice := e.lastPrice[ps.Symbol]
	e.lastPriceMu.RUnlock()

	// 2. Fall back to SN price API.
	if currentPrice <= 0 {
		exchange := e.exchangeForProduct(ps.Symbol)
		if exchange == "" {
			e.logger.Warn().Str("symbol", ps.Symbol).
				Msg("risk loop: no cached price and exchange unknown — skipping tick")
			return
		}
		price, err := fetchCurrentPrice(ctx, e.cfg, exchange, ps.Symbol)
		if err != nil {
			e.logger.Warn().Err(err).Str("symbol", ps.Symbol).
				Msg("risk loop: price API fetch failed — skipping tick")
			return
		}
		currentPrice = price
		// Warm the cache for subsequent checks within this tick.
		e.lastPriceMu.Lock()
		e.lastPrice[ps.Symbol] = currentPrice
		e.lastPriceMu.Unlock()
	}

	if currentPrice <= 0 {
		return
	}

	logger := e.logger.With().
		Str("symbol", ps.Symbol).
		Float64("current_price", currentPrice).
		Logger()

	// Build risk.Position from PositionState for library evaluation.
	riskPos := &risk.Position{
		EntryPrice:   ps.EntryPrice,
		Side:         ps.Side,
		StopLoss:     ps.StopLoss,
		TakeProfit:   ps.TakeProfit,
		HardStop:     ps.HardStop,
		Leverage:     ps.Leverage,
		Strategy:     ps.Strategy,
		Granularity:  ps.Granularity,
		MarketType:   ps.MarketType,
		OpenedAt:     ps.OpenedAt,
		PeakPrice:    ps.PeakPrice,
		TrailingStop: ps.TrailingStop,
	}

	oldPeak := riskPos.PeakPrice
	oldTrail := riskPos.TrailingStop

	// Tick mode: use currentPrice for high, low, and close.
	decision, shouldExit := risk.Evaluate(riskPos, currentPrice, currentPrice, currentPrice, time.Now())

	if shouldExit {
		// Guard against concurrent closes: set Closing flag under write lock.
		e.posStateMu.Lock()
		psInMap, exists := e.posState[posKey(ps.AccountID, ps.Symbol)]
		if !exists || psInMap.Closing {
			e.posStateMu.Unlock()
			return
		}
		psInMap.Closing = true
		e.posStateMu.Unlock()

		logger.Info().
			Str("exit_reason", decision.ExitReason).
			Int("layer", decision.Layer).
			Str("strategy", ps.Strategy).
			Str("position_side", ps.Side).
			Float64("entry_price", ps.EntryPrice).
			Msg("risk evaluation triggered exit")

		e.executeCloseTrade(ctx, ps, currentPrice, decision.ExitReason)
		return
	}

	// If trailing stop advanced, persist the updated state.
	if riskPos.PeakPrice != oldPeak || riskPos.TrailingStop != oldTrail {
		e.posStateMu.Lock()
		if psInMap, exists := e.posState[posKey(ps.AccountID, ps.Symbol)]; exists {
			psInMap.PeakPrice = riskPos.PeakPrice
			psInMap.TrailingStop = riskPos.TrailingStop
		}
		e.posStateMu.Unlock()

		dbState := &store.EnginePositionState{
			ID:           ps.ID,
			AccountID:    ps.AccountID,
			Symbol:       ps.Symbol,
			MarketType:   ps.MarketType,
			PeakPrice:    riskPos.PeakPrice,
			TrailingStop: riskPos.TrailingStop,
		}
		if err := e.repo.UpdatePositionState(ctx, tenantID, dbState); err != nil {
			logger.Warn().Err(err).Msg("failed to persist trailing stop update")
		} else {
			logger.Debug().
				Float64("peak_price", riskPos.PeakPrice).
				Float64("trailing_stop", riskPos.TrailingStop).
				Msg("trailing stop state advanced")
		}
	}
}
