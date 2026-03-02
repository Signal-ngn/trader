package engine

import (
	"context"
	"math"
	"time"

	"github.com/Signal-ngn/trader/internal/store"
)

const (
	riskLoopInterval    = 5 * time.Minute
	maxHoldDuration     = 48 * time.Hour
	trailingActivatePct = 0.03  // 3% unrealised P&L to activate trailing stop
	trailingTrailPct    = 0.02  // trail 2% behind peak
	slSanityPct         = 0.001 // 0.1% — if SL/TP within this, use default
	defaultSLPct        = 0.04  // -4% default stop-loss
	defaultTPPct        = 0.10  // +10% default take-profit
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

// startRiskLoop runs the risk management loop every 5 minutes.
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

// evaluatePositions reconciles position state and evaluates risk for each position.
func (e *Engine) evaluatePositions(ctx context.Context) error {
	// Kill switch — still allow closes, just log.
	killActive := e.killSwitchActive()
	if killActive {
		e.logger.Warn().Str("file", e.cfg.KillSwitchFile).Msg("kill switch active — skipping new opens in risk loop")
	}

	// Load open ledger positions.
	openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, e.cfg.TraderAccount)
	if err != nil {
		return err
	}

	// Build a set of symbols with open ledger positions.
	openSymbols := make(map[string]bool)
	for _, p := range openPositions {
		openSymbols[p.Symbol] = true
	}

	// Prune orphaned engine_position_state rows (position closed externally).
	e.posStateMu.Lock()
	for sym := range e.posState {
		if !openSymbols[sym] {
			e.logger.Info().Str("symbol", sym).Msg("pruning orphaned position state")
			tenantID := e.tenantID()
			if err := e.repo.DeletePositionState(ctx, tenantID, sym, e.posState[sym].MarketType, e.cfg.TraderAccount); err != nil {
				e.logger.Warn().Err(err).Str("symbol", sym).Msg("failed to delete orphaned position state")
			}
			delete(e.posState, sym)
			e.conflictMu.Lock()
			delete(e.conflict, sym)
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

// evaluatePosition applies all risk rules to a single position.
//
// Price resolution order:
//  1. Last price seen in a received NGS signal for this symbol (updated live as signals arrive).
//  2. SN price API fetch (GET /prices/{exchange}/{product}) — used when no signal has arrived
//     since startup or since the last risk loop tick.
//  3. Skip evaluation for this tick if neither source is available (logs a warning).
//
// This means risk enforcement is as timely as the signal stream for active products.
// For products with infrequent signals, the SN API provides a fallback with ~1-minute
// granularity so SL/TP/trailing-stop checks still fire on each risk loop tick.
func (e *Engine) evaluatePosition(ctx context.Context, ps *PositionState) {
	tenantID := e.tenantID()

	// 1. Try cached signal price.
	e.lastPriceMu.RLock()
	currentPrice := e.lastPrice[ps.Symbol]
	e.lastPriceMu.RUnlock()

	// 2. Fall back to SN price API.
	if currentPrice <= 0 {
		// We need the exchange name to call the price API. It's not stored in PositionState,
		// so we derive it from the allowlist by looking for a matching product entry.
		// If we can't find it, we skip rather than use a stale proxy.
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

	logger := e.logger.With().Str("symbol", ps.Symbol).Float64("current_price", currentPrice).Logger()

	// Resolve effective stop-loss.
	sl := ps.StopLoss
	if ps.Side == "long" {
		if sl <= 0 || math.Abs(sl-ps.EntryPrice)/ps.EntryPrice < slSanityPct {
			sl = ps.EntryPrice * (1 - defaultSLPct)
		}
	} else {
		if sl <= 0 || math.Abs(sl-ps.EntryPrice)/ps.EntryPrice < slSanityPct {
			sl = ps.EntryPrice * (1 + defaultSLPct)
		}
	}

	// Resolve effective take-profit.
	tp := ps.TakeProfit
	if ps.Side == "long" {
		if tp <= 0 || math.Abs(tp-ps.EntryPrice)/ps.EntryPrice < slSanityPct {
			tp = ps.EntryPrice * (1 + defaultTPPct)
		}
	} else {
		if tp <= 0 || math.Abs(tp-ps.EntryPrice)/ps.EntryPrice < slSanityPct {
			tp = ps.EntryPrice * (1 - defaultTPPct)
		}
	}

	// 1. Max hold time check.
	if time.Since(ps.OpenedAt) > maxHoldDuration {
		logger.Info().Dur("held_for", time.Since(ps.OpenedAt)).Msg("max hold time reached — closing position")
		e.executeCloseTrade(ctx, ps, currentPrice, "max hold time")
		return
	}

	// 2. Stop-loss check.
	if ps.Side == "long" && currentPrice <= sl {
		logger.Info().Float64("stop_loss", sl).Msg("stop-loss hit — closing position")
		e.executeCloseTrade(ctx, ps, currentPrice, "stop loss")
		return
	}
	if ps.Side == "short" && currentPrice >= sl {
		logger.Info().Float64("stop_loss", sl).Msg("stop-loss hit — closing position")
		e.executeCloseTrade(ctx, ps, currentPrice, "stop loss")
		return
	}

	// 3. Take-profit check.
	if ps.Side == "long" && currentPrice >= tp {
		logger.Info().Float64("take_profit", tp).Msg("take-profit hit — closing position")
		e.executeCloseTrade(ctx, ps, currentPrice, "take profit")
		return
	}
	if ps.Side == "short" && currentPrice <= tp {
		logger.Info().Float64("take_profit", tp).Msg("take-profit hit — closing position")
		e.executeCloseTrade(ctx, ps, currentPrice, "take profit")
		return
	}

	// 4. Trailing stop evaluation.
	leverage := float64(ps.Leverage)
	if leverage <= 0 {
		leverage = 1
	}
	scale := 1.0 / leverage

	var unrealisedPctRaw float64
	if ps.Side == "long" {
		unrealisedPctRaw = (currentPrice - ps.EntryPrice) / ps.EntryPrice * scale
	} else {
		unrealisedPctRaw = (ps.EntryPrice - currentPrice) / ps.EntryPrice * scale
	}

	updated := false
	if unrealisedPctRaw >= trailingActivatePct {
		peak := ps.PeakPrice
		newTrailing := ps.TrailingStop

		if ps.Side == "long" {
			if currentPrice > peak {
				peak = currentPrice
				newTrailing = peak * (1 - trailingTrailPct)
				updated = true
			}
		} else {
			if peak == 0 || currentPrice < peak {
				peak = currentPrice
				newTrailing = peak * (1 + trailingTrailPct)
				updated = true
			}
		}

		if updated {
			ps.PeakPrice = peak
			ps.TrailingStop = newTrailing
			e.posStateMu.Lock()
			if existing, ok := e.posState[ps.Symbol]; ok {
				existing.PeakPrice = peak
				existing.TrailingStop = newTrailing
			}
			e.posStateMu.Unlock()
			dbState := &store.EnginePositionState{
				ID:           ps.ID,
				AccountID:    ps.AccountID,
				Symbol:       ps.Symbol,
				MarketType:   ps.MarketType,
				Side:         ps.Side,
				EntryPrice:   ps.EntryPrice,
				StopLoss:     ps.StopLoss,
				TakeProfit:   ps.TakeProfit,
				Leverage:     ps.Leverage,
				Strategy:     ps.Strategy,
				OpenedAt:     ps.OpenedAt,
				PeakPrice:    peak,
				TrailingStop: newTrailing,
			}
			if err := e.repo.UpdatePositionState(ctx, tenantID, dbState); err != nil {
				logger.Warn().Err(err).Msg("failed to update position state trailing stop")
			}
			logger.Debug().Float64("peak", peak).Float64("trailing_stop", newTrailing).Msg("trailing stop updated")
		}

		// Check if trailing stop is breached.
		if ps.TrailingStop > 0 {
			if ps.Side == "long" && currentPrice <= ps.TrailingStop {
				logger.Info().Float64("trailing_stop", ps.TrailingStop).Msg("trailing stop hit — closing position")
				e.executeCloseTrade(ctx, ps, currentPrice, "trailing stop")
				return
			}
			if ps.Side == "short" && currentPrice >= ps.TrailingStop {
				logger.Info().Float64("trailing_stop", ps.TrailingStop).Msg("trailing stop hit — closing position")
				e.executeCloseTrade(ctx, ps, currentPrice, "trailing stop")
				return
			}
		}
	}
}


