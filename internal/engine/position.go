package engine

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/Signal-ngn/risk"
	"github.com/Signal-ngn/trader/internal/domain"
)

// processSignal routes a signal to the position engine for a specific account.
func (e *Engine) processSignal(ctx context.Context, signal SignalPayload, product, strategy, accountID string) {
	logger := e.logger.With().
		Str("account", accountID).
		Str("product", product).
		Str("action", signal.Action).
		Float64("price", signal.Price).
		Logger()

	// Kill switch check.
	if (signal.Action == "BUY" || signal.Action == "SHORT") && e.killSwitchActive() {
		logger.Warn().Str("file", e.cfg.KillSwitchFile).Msg("kill switch active — skipping open trade")
		return
	}

	// Fetch trading config for this account+product to get market type and leverage.
	tradingConfigs, err := fetchTradingConfigs(ctx, e.cfg)
	if err != nil {
		logger.Error().Err(err).Msg("failed to fetch trading configs, skipping signal")
		return
	}
	tradingConfig, ok := tradingConfigs[tradingConfigKey{accountID: accountID, productID: product}]
	if !ok {
		logger.Warn().Msg("no trading config for account+product, skipping signal")
		return
	}

	// Validate that the signal strategy is configured for this account+product.
	if signal.Action == "BUY" || signal.Action == "SHORT" {
		var allowedStrategies []string
		if signal.Action == "BUY" {
			allowedStrategies = tradingConfig.StrategiesLong
		} else {
			allowedStrategies = tradingConfig.StrategiesShort
		}
		strategyAllowed := false
		for _, s := range allowedStrategies {
			if s == strategy || strings.HasPrefix(strategy, s+"_") || strings.HasPrefix(strategy, s+"+") {
				strategyAllowed = true
				break
			}
		}
		if !strategyAllowed {
			logger.Debug().Str("strategy", strategy).Msg("strategy not in trading config for account+product, skipping signal")
			return
		}
	}

	// Apply per-account confidence threshold from StrategyParams.
	// The ingestion server publishes signals using the most permissive threshold
	// across all accounts (so no trader misses a signal). Each trader engine is
	// responsible for enforcing its own configured threshold here.
	baseName := signalBaseName(strategy)
	if params, ok := tradingConfig.StrategyParams[baseName]; ok {
		switch signal.Action {
		case "BUY", "SHORT":
			if thresh, ok := params["confidence"]; ok && signal.Confidence < thresh {
				logger.Debug().
					Str("strategy", strategy).
					Float64("confidence", signal.Confidence).
					Float64("threshold", thresh).
					Msg("signal below account confidence threshold, skipping")
				return
			}
		case "SELL", "COVER":
			// ML strategies self-govern exit confidence server-side: the ingestion
			// server only emits a SELL when P(long) drops below the strategy's own
			// exit threshold, and sets is_exit=true. Applying an additional
			// exit_confidence gate here would double-filter and silently swallow
			// legitimate exits. Skip the gate for any signal flagged is_exit=true.
			if !signal.IsExit {
				if thresh, ok := params["exit_confidence"]; ok && signal.Confidence > 0 && signal.Confidence < thresh {
					logger.Debug().
						Str("strategy", strategy).
						Float64("confidence", signal.Confidence).
						Float64("exit_threshold", thresh).
						Msg("exit signal below account exit_confidence threshold, skipping")
					return
				}
			}
		}
	}

	switch signal.Action {
	case "BUY", "SHORT":
		e.handleOpenSignal(ctx, signal, product, strategy, accountID, tradingConfig)
	case "SELL", "COVER":
		e.handleCloseSignal(ctx, signal, product, strategy, accountID, tradingConfig)
	default:
		logger.Warn().Str("action", signal.Action).Msg("unknown signal action, skipping")
	}
}

// signalBaseName strips direction suffixes (_short, _long) from a strategy name
// to find the base name used as a key in StrategyParams.
// e.g. "ml_transformer_1h_short" → "ml_transformer_1h"
//      "ml_transformer_1h"       → "ml_transformer_1h"
func signalBaseName(strategy string) string {
	for _, suffix := range []string{"_short", "_long"} {
		if strings.HasSuffix(strategy, suffix) {
			return strategy[:len(strategy)-len(suffix)]
		}
	}
	return strategy
}

// handleOpenSignal handles BUY and SHORT signals for a specific account.
func (e *Engine) handleOpenSignal(ctx context.Context, signal SignalPayload, product, strategy, accountID string, tc *TradingConfig) {
	logger := e.logger.With().
		Str("account", accountID).
		Str("product", product).
		Str("action", signal.Action).
		Str("strategy", strategy).
		Float64("price", signal.Price).
		Float64("confidence", signal.Confidence).
		Logger()

	// Determine market type and side.
	side, positionSide, marketType := mapSignalToSide(signal.Action, tc)

	// Daily loss limit check.
	if e.cfg.DailyLossLimit > 0 && e.isDailyLossLimitReached(ctx, accountID) {
		logger.Warn().Float64("limit", e.cfg.DailyLossLimit).Msg("daily loss limit reached — skipping open trade")
		return
	}

	// Direction conflict guard.
	e.conflictMu.Lock()
	if openSide, exists := e.conflict[posKey(accountID, product)]; exists {
		if openSide != string(positionSide) {
			e.conflictMu.Unlock()
			logger.Warn().Str("open_side", openSide).Str("want", string(positionSide)).
				Msg("direction conflict — skipping trade")
			return
		}
	}
	e.conflictMu.Unlock()

	// Per-config confidence threshold check.
	if tc.MinConfidence > 0 && signal.Confidence < tc.MinConfidence {
		logger.Debug().
			Float64("confidence", signal.Confidence).
			Float64("min_confidence", tc.MinConfidence).
			Msg("signal confidence below configured threshold, dropping")
		return
	}

	// Max positions check (per account).
	if e.cfg.MaxPositions > 0 {
		states, err := e.repo.CountOpenPositionStates(ctx, accountID)
		if err != nil {
			logger.Error().Err(err).Msg("failed to count open positions")
			return
		}
		if states >= e.cfg.MaxPositions {
			logger.Warn().Int("max", e.cfg.MaxPositions).Int("open", states).
				Msg("max positions reached — skipping trade")
			return
		}
	}

	// Fetch current balance to size position within available funds.
	tenantID := e.tenantID()
	balance, balErr := e.repo.GetAccountBalance(ctx, tenantID, accountID, "USD")
	if balErr != nil {
		logger.Error().Err(balErr).Msg("failed to fetch account balance")
		return
	}

	// Calculate position size capped to available balance.
	size, qty, margin, err := e.calculatePositionSize(signal, tc, marketType, balance)
	if err != nil {
		logger.Error().Err(err).Msg("failed to calculate position size")
		return
	}

	// Determine the required capital for this position.
	required := margin
	if marketType == domain.MarketTypeSpot {
		required = size
	}

	// Reject if the effective position is below the configured minimum.
	if e.cfg.MinPositionSize > 0 && required < e.cfg.MinPositionSize {
		logger.Warn().
			Float64("required", required).
			Float64("min_position_size", e.cfg.MinPositionSize).
			Msg("available balance below minimum position size — skipping trade")
		return
	}

	// Safety-net balance check (guards against races and zero-balance edge cases).
	if err := e.checkBalance(ctx, tenantID, accountID, required); err != nil {
		logger.Warn().Err(err).Msg("insufficient balance — skipping trade")
		return
	}

	// Build trade.
	now := time.Now().UTC()
	leverage := tc.LongLeverage
	if signal.Action == "SHORT" {
		leverage = tc.ShortLeverage
	}

	var sl, tp *float64
	if signal.StopLoss > 0 {
		v := signal.StopLoss
		sl = &v
	}
	if signal.TakeProfit > 0 {
		v := signal.TakeProfit
		tp = &v
	}
	var conf *float64
	if signal.Confidence > 0 {
		v := signal.Confidence
		conf = &v
	}
	var stratPtr *string
	if strategy != "" {
		v := strategy
		stratPtr = &v
	}
	var marginPtr *float64
	if marketType == domain.MarketTypeFutures && margin > 0 {
		v := margin
		marginPtr = &v
	}
	var leveragePtr *int
	if leverage > 0 {
		v := leverage
		leveragePtr = &v
	}

	trade := &domain.Trade{
		TenantID:     tenantID,
		TradeID:      fmt.Sprintf("engine-%s-%s-%d", accountID, product, now.UnixNano()),
		AccountID:    accountID,
		Symbol:       product,
		Side:         side,
		PositionSide: positionSide,
		Quantity:     qty,
		Price:        signal.Price,
		Fee:          0,
		FeeCurrency:  "USD",
		MarketType:   marketType,
		Timestamp:    now,
		IngestedAt:   now,
		Leverage:     leveragePtr,
		Margin:       marginPtr,
		Strategy:     stratPtr,
		Confidence:   conf,
		StopLoss:     sl,
		TakeProfit:   tp,
	}
	if signal.Reason != "" {
		r := signal.Reason
		trade.EntryReason = &r
	}

	// Log before executing so a crash mid-flight is still visible.
	slVal, tpVal := 0.0, 0.0
	if sl != nil {
		slVal = *sl
	}
	if tp != nil {
		tpVal = *tp
	}
	logger.Info().
		Str("market_type", string(marketType)).
		Str("position_side", string(positionSide)).
		Float64("size_usd", size).
		Float64("qty", qty).
		Float64("stop_loss", slVal).
		Float64("take_profit", tpVal).
		Int("leverage", leverage).
		Str("mode", e.cfg.TradingMode).
		Msg("opening position")

	// Execute the trade.
	if err := e.executeOpenTrade(ctx, signal, trade, positionSide); err != nil {
		logger.Error().Err(err).Msg("failed to execute open trade")
		return
	}

	// Update cooldown.
	key := cooldownKey{accountID: accountID, symbol: product, action: signal.Action}
	e.cooldownMu.Lock()
	e.cooldown[key] = time.Now().Add(5 * time.Minute)
	e.cooldownMu.Unlock()

	// Update conflict guard.
	e.conflictMu.Lock()
	e.conflict[posKey(accountID, product)] = string(positionSide)
	e.conflictMu.Unlock()

	// Compute hard stop price at entry (circuit-breaker, immutable for lifetime of position).
	hardStop := risk.ComputeHardStop(signal.Price, string(positionSide), leverage, string(marketType))

	// Persist position state.
	dbState := &EnginePositionState{
		AccountID:   accountID,
		Symbol:      product,
		MarketType:  string(marketType),
		Side:        string(positionSide),
		EntryPrice:  signal.Price,
		HardStop:    hardStop,
		Leverage:    leverage,
		Strategy:    strategy,
		Granularity: tc.Granularity,
		OpenedAt:    now,
	}
	if sl != nil {
		dbState.StopLoss = *sl
	}
	if tp != nil {
		dbState.TakeProfit = *tp
	}

	if err := e.repo.InsertPositionState(ctx, tenantID, dbState); err != nil {
		logger.Error().Err(err).Msg("failed to persist position state")
	} else {
		ps := &PositionState{
			AccountID:   dbState.AccountID,
			Symbol:      dbState.Symbol,
			MarketType:  dbState.MarketType,
			Side:        dbState.Side,
			EntryPrice:  dbState.EntryPrice,
			StopLoss:    dbState.StopLoss,
			TakeProfit:  dbState.TakeProfit,
			HardStop:    dbState.HardStop,
			Leverage:    dbState.Leverage,
			Strategy:    dbState.Strategy,
			Granularity: dbState.Granularity,
			OpenedAt:    dbState.OpenedAt,
		}
		e.posStateMu.Lock()
		e.posState[posKey(accountID, product)] = ps
		e.posStateMu.Unlock()
	}
}

// handleCloseSignal handles SELL and COVER signals for a specific account.
func (e *Engine) handleCloseSignal(ctx context.Context, signal SignalPayload, product, strategy, accountID string, tc *TradingConfig) {
	logger := e.logger.With().
		Str("account", accountID).
		Str("product", product).
		Str("action", signal.Action).
		Str("strategy", strategy).
		Float64("price", signal.Price).
		Logger()

	// Check if we have an open position for this account+product.
	e.posStateMu.RLock()
	ps, exists := e.posState[posKey(accountID, product)]
	e.posStateMu.RUnlock()

	if !exists {
		logger.Debug().Msg("no open position state for account+product, ignoring close signal")
		return
	}

	logger.Info().
		Str("position_side", ps.Side).
		Float64("entry_price", ps.EntryPrice).
		Str("mode", e.cfg.TradingMode).
		Msg("closing position on signal")

	// Use the strategy-supplied reason if present; otherwise fall back to the
	// canonical Layer 3 conviction-drop label.
	exitReason := "Layer 3: conviction drop"
	if signal.Reason != "" {
		exitReason = signal.Reason
	}
	e.executeCloseTrade(ctx, ps, signal.Price, exitReason)
}

// mapSignalToSide maps a signal action to trade side, position side, and market type.
func mapSignalToSide(action string, tc *TradingConfig) (domain.Side, domain.PositionSide, domain.MarketType) {
	// Determine market type: if there are long/short strategies, it's futures; otherwise spot.
	marketType := domain.MarketTypeSpot
	if len(tc.StrategiesLong) > 0 || len(tc.StrategiesShort) > 0 {
		marketType = domain.MarketTypeFutures
	}

	switch action {
	case "BUY":
		return domain.SideBuy, domain.PositionSideLong, marketType
	case "SELL":
		return domain.SideSell, domain.PositionSideLong, marketType
	case "SHORT":
		return domain.SideSell, domain.PositionSideShort, marketType
	case "COVER":
		return domain.SideBuy, domain.PositionSideShort, marketType
	default:
		return domain.SideBuy, domain.PositionSideLong, marketType
	}
}

// scaledPct returns an effective position percentage that scales progressively
// with the account balance using a power-law curve.
//
// Two anchors define the curve:
//   - At PortfolioSize        → PositionSizePct  (the "full-size" floor)
//   - At PortfolioSize / 10   → PositionSizeMaxPct (the small-account cap)
//
// Between the anchors the formula is:
//
//	pct = basePct × (portfolioSize / balance)^k
//	k   = log(maxPct / basePct) / log(10)
//
// Progressive scaling is disabled (returns basePct) when PositionSizeMaxPct is
// zero or not greater than basePct.
func (e *Engine) scaledPct(basePct, balance float64) float64 {
	maxPct := e.cfg.PositionSizeMaxPct
	portfolioSize := e.cfg.PortfolioSize

	if maxPct <= basePct || portfolioSize <= 0 || balance <= 0 {
		return basePct
	}

	k := math.Log(maxPct/basePct) / math.Log(10)
	pct := basePct * math.Pow(portfolioSize/balance, k)

	// Floor at basePct for accounts at or above the reference size.
	if pct < basePct {
		pct = basePct
	}
	// Cap at maxPct for accounts well below the reference size.
	if pct > maxPct {
		pct = maxPct
	}
	return pct
}

// calculatePositionSize returns (size, quantity, margin, error).
//
// Sizing logic (in order):
//  1. Base: live account balance when available; PortfolioSize as fallback.
//  2. Percentage: progressively scaled from PositionSizePct (at PortfolioSize)
//     up to PositionSizeMaxPct (at PortfolioSize/10) for small accounts.
//     Signal-provided PositionPct overrides the config PCT directly (no scaling).
//  3. Clamp: applied against [MinPositionSize, MaxPositionSize] when set.
//  4. Hard cap: position never exceeds available balance.
func (e *Engine) calculatePositionSize(signal SignalPayload, tc *TradingConfig, marketType domain.MarketType, availableBalance *float64) (size, qty, margin float64, err error) {
	// Use live balance as the sizing base so PCT is applied to what the account
	// actually holds.  Fall back to PortfolioSize only when no balance record exists.
	base := e.cfg.PortfolioSize
	if availableBalance != nil && *availableBalance > 0 {
		base = *availableBalance
	}

	var pct float64
	if signal.PositionPct > 0 {
		// Explicit signal override — honour as-is, no progressive scaling.
		pct = signal.PositionPct * 100 // signal uses 0–1 fraction
	} else {
		// Apply progressive scaling: higher % for small accounts, lower for large.
		pct = e.scaledPct(e.cfg.PositionSizePct, base)
	}

	size = base * (pct / 100)

	// Clamp to [min, max] from config.
	if e.cfg.MinPositionSize > 0 && size < e.cfg.MinPositionSize {
		size = e.cfg.MinPositionSize
	}
	if e.cfg.MaxPositionSize > 0 && size > e.cfg.MaxPositionSize {
		size = e.cfg.MaxPositionSize
	}

	if signal.Price <= 0 {
		return 0, 0, 0, fmt.Errorf("signal price is zero or negative")
	}

	// Determine leverage for futures (use the correct side).
	var leverage float64
	if marketType == domain.MarketTypeFutures {
		if signal.Action == "SHORT" {
			leverage = float64(tc.ShortLeverage)
		} else {
			leverage = float64(tc.LongLeverage)
		}
		if leverage <= 0 {
			leverage = 1
		}
		margin = size / leverage
	}

	// Cap to available balance so we never invest more than the account holds.
	if availableBalance != nil && *availableBalance > 0 {
		if marketType == domain.MarketTypeSpot {
			if size > *availableBalance {
				size = *availableBalance
			}
		} else {
			// For futures the capital at risk is the margin, not the full notional.
			if margin > *availableBalance {
				margin = *availableBalance
				size = margin * leverage
			}
		}
	}

	qty = size / signal.Price
	return size, qty, margin, nil
}

// checkBalance checks whether the account has sufficient balance for a trade.
// Returns an error if balance exists and is insufficient. Bypasses check if no balance row exists.
func (e *Engine) checkBalance(ctx context.Context, tenantID uuid.UUID, accountID string, required float64) error {
	balance, err := e.repo.GetAccountBalance(ctx, tenantID, accountID, "USD")
	if err != nil {
		return fmt.Errorf("get balance: %w", err)
	}
	if balance == nil {
		return nil // no balance set, bypass check
	}
	if *balance < required {
		return fmt.Errorf("insufficient balance: need $%.2f, have $%.2f", required, *balance)
	}
	return nil
}

// executeOpenTrade executes an open trade in paper or live mode.
func (e *Engine) executeOpenTrade(ctx context.Context, signal SignalPayload, trade *domain.Trade, positionSide domain.PositionSide) error {
	tenantID := e.tenantID()

	if e.cfg.TradingMode == "live" {
		req := OpenPositionRequest{
			Symbol:    trade.Symbol,
			Side:      positionSide,
			SizeUSD:   signal.Price * trade.Quantity,
			Leverage:  0,
			Price:     signal.Price,
		}
		if trade.Leverage != nil {
			req.Leverage = *trade.Leverage
		}
		result, err := e.exchange.OpenPosition(ctx, req)
		if err != nil {
			return fmt.Errorf("exchange open position: %w", err)
		}
		trade.Price = result.FillPrice
		trade.Quantity = result.Quantity
		trade.Fee = result.Fee
		if trade.Margin != nil && result.Margin > 0 {
			m := result.Margin
			trade.Margin = &m
		}
	}

	// Compute cost basis = margin committed for this trade.
	// For futures: margin = notional / leverage (already in trade.Margin).
	// For spot: full notional.
	if trade.MarketType == domain.MarketTypeFutures && trade.Margin != nil && *trade.Margin > 0 {
		trade.CostBasis = *trade.Margin
	} else {
		trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
	}

	inserted, err := e.repo.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
	if err != nil {
		if e.cfg.TradingMode == "live" {
			log.Error().
				Str("trade_id", trade.TradeID).
				Str("symbol", trade.Symbol).
				Float64("price", trade.Price).
				Float64("qty", trade.Quantity).
				Msg("CRITICAL: exchange order executed but ledger write failed — manual recovery required")
		}
		return fmt.Errorf("insert trade: %w", err)
	}
	if inserted {
		ev := e.logger.Info().
			Str("trade_id", trade.TradeID).
			Str("account", trade.AccountID).
			Str("symbol", trade.Symbol).
			Str("side", string(trade.Side)).
			Str("market_type", string(trade.MarketType)).
			Float64("qty", trade.Quantity).
			Float64("price", trade.Price).
			Float64("fee", trade.Fee)
		if trade.Strategy != nil {
			ev = ev.Str("strategy", *trade.Strategy)
		}
		if trade.Leverage != nil {
			ev = ev.Int("leverage", *trade.Leverage)
		}
		if trade.Margin != nil {
			ev = ev.Float64("margin", *trade.Margin)
		}
		ev.Msg("position opened")

		if e.publisher != nil {
			e.publisher.Publish(trade.AccountID, trade)
		}
	}
	return nil
}

// executeCloseTrade executes a close trade.
func (e *Engine) executeCloseTrade(ctx context.Context, ps *PositionState, currentPrice float64, exitReason string) {
	logger := e.logger.With().
		Str("account", ps.AccountID).
		Str("symbol", ps.Symbol).
		Str("exit_reason", exitReason).
		Logger()
	tenantID := e.tenantID()

	// Determine close side (opposite of open side).
	var side domain.Side
	if ps.Side == "long" {
		side = domain.SideSell
	} else {
		side = domain.SideBuy
	}

	marketType := domain.MarketType(ps.MarketType)
	now := time.Now().UTC()

	// Load current open position to get quantity.
	openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, ps.AccountID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to load open positions for close")
		return
	}

	var qty float64
	for _, p := range openPositions {
		if p.Symbol == ps.Symbol && string(p.MarketType) == ps.MarketType {
			qty = p.Quantity
			break
		}
	}
	if qty <= 0 {
		logger.Warn().Msg("no open position quantity found, skipping close")
		return
	}

	if e.cfg.TradingMode == "live" {
		req := ClosePositionRequest{
			Symbol:     ps.Symbol,
			Side:       domain.PositionSide(ps.Side),
			MarketType: marketType,
		}
		result, err := e.exchange.ClosePosition(ctx, req)
		if err != nil {
			logger.Error().Err(err).Msg("exchange close position failed")
			return
		}
		currentPrice = result.FillPrice
		qty = result.Quantity
	}

	var leveragePtr *int
	if ps.Leverage > 0 {
		v := ps.Leverage
		leveragePtr = &v
	}
	var stratPtr *string
	if ps.Strategy != "" {
		v := ps.Strategy
		stratPtr = &v
	}
	exitStr := exitReason

	trade := &domain.Trade{
		TenantID:     tenantID,
		TradeID:      fmt.Sprintf("engine-close-%s-%s-%d", ps.AccountID, ps.Symbol, now.UnixNano()),
		AccountID:    ps.AccountID,
		Symbol:       ps.Symbol,
		Side:         side,
		PositionSide: domain.PositionSide(ps.Side), // "long" or "short"
		Quantity:     qty,
		Price:        currentPrice,
		Fee:          0,
		FeeCurrency:  "USD",
		MarketType:   marketType,
		Timestamp:    now,
		IngestedAt:   now,
		Leverage:     leveragePtr,
		Strategy:     stratPtr,
		ExitReason:   &exitStr,
	}

	// Cost basis for sell = avg entry price × quantity (used by store for P&L).
	avgEntry, err := e.repo.GetAvgEntryPrice(ctx, tenantID, ps.AccountID, ps.Symbol, marketType)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get avg entry price")
	}
	costBasisForTrade(trade, avgEntry)

	_, err = e.repo.InsertTradeAndUpdatePosition(ctx, tenantID, trade)
	if err != nil {
		logger.Error().Err(err).Msg("failed to record close trade")
		return
	}

	pnl := (currentPrice - ps.EntryPrice) * qty
	if ps.Side == "short" {
		pnl = (ps.EntryPrice - currentPrice) * qty
	}
	ev := logger.Info().
		Str("trade_id", trade.TradeID).
		Str("account", ps.AccountID).
		Str("position_side", ps.Side).
		Str("market_type", string(marketType)).
		Float64("entry_price", ps.EntryPrice).
		Float64("exit_price", currentPrice).
		Float64("qty", qty).
		Float64("pnl", pnl).
		Str("exit_reason", exitReason)
	if ps.Strategy != "" {
		ev = ev.Str("strategy", ps.Strategy)
	}
	ev.Msg("position closed")

	if e.publisher != nil {
		e.publisher.Publish(trade.AccountID, trade)
	}

	// Clean up position state.
	if err := e.repo.DeletePositionState(ctx, tenantID, ps.Symbol, ps.MarketType, ps.AccountID); err != nil {
		logger.Warn().Err(err).Msg("failed to delete position state")
	}
	e.posStateMu.Lock()
	delete(e.posState, posKey(ps.AccountID, ps.Symbol))
	e.posStateMu.Unlock()

	// Remove from conflict guard.
	e.conflictMu.Lock()
	delete(e.conflict, posKey(ps.AccountID, ps.Symbol))
	e.conflictMu.Unlock()
}

// killSwitchActive returns true if the kill switch file exists.
func (e *Engine) killSwitchActive() bool {
	if e.cfg.KillSwitchFile == "" {
		return false
	}
	_, err := os.Stat(e.cfg.KillSwitchFile)
	return err == nil
}

// isDailyLossLimitReached reads today's realised P&L from Firestore (via the
// EngineStore) and returns true when total losses exceed cfg.DailyLossLimit.
//
// Using Firestore (rather than an in-memory counter) means:
//   - The limit survives engine restarts.
//   - Daily P&L is atomically incremented after each close, so concurrent writes
//     are safe and no increments are lost.
func (e *Engine) isDailyLossLimitReached(ctx context.Context, accountID string) bool {
	pnl, err := e.repo.DailyRealizedPnL(ctx, accountID)
	if err != nil {
		e.logger.Warn().Err(err).Msg("daily loss check: DB query failed, allowing trade")
		return false
	}
	// pnl is negative when there are net losses.
	loss := -pnl
	if loss < 0 {
		loss = 0
	}
	if loss >= e.cfg.DailyLossLimit {
		e.logger.Warn().
			Float64("loss_today", loss).
			Float64("limit", e.cfg.DailyLossLimit).
			Msg("daily loss limit reached")
		return true
	}
	return false
}

// tenantID returns the tenant UUID resolved at startup.
func (e *Engine) tenantID() uuid.UUID {
	return e.tenantUUID
}
