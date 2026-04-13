package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Signal-ngn/risk"
	"github.com/Signal-ngn/trader/internal/domain"
)

// flushAccountSignals is the flush callback wired into the signal buffer.
// It implements the 4-phase batched flow:
//   1. Process close signals (InsertTradeOnly, accumulate balance delta)
//   2. Read balance once, add close deltas
//   3. Process open signals by confidence desc (InsertTradeOnly, track local balance)
//   4. Write balance once with net delta
func (e *Engine) flushAccountSignals(ctx context.Context, signals []bufferedSignal) {
	if len(signals) == 0 {
		return
	}

	accountID := signals[0].accountID
	logger := e.logger.With().
		Str("account", accountID).
		Int("signal_count", len(signals)).
		Logger()

	logger.Info().Msg("flushing signal burst")

	// Split into closes and opens (already sorted by buffer).
	var closes, opens []bufferedSignal
	for _, sig := range signals {
		if isCloseAction(sig.signal.Action) {
			closes = append(closes, sig)
		} else {
			opens = append(opens, sig)
		}
	}

	tenantID := e.tenantID()
	var netDelta float64

	// --- Phase 1: Process closes ---
	for _, sig := range closes {
		delta, err := e.processCloseBatched(ctx, sig)
		if err != nil {
			logger.Error().Err(err).
				Str("product", sig.product).
				Str("action", sig.signal.Action).
				Msg("close signal failed during flush")
			continue
		}
		netDelta += delta
	}

	// --- Phase 2 & 3: Process opens (if any) ---
	if len(opens) > 0 {
		// Read balance once.
		balance, err := e.repo.GetAccountBalance(ctx, tenantID, accountID, "USD")
		if err != nil {
			logger.Error().Err(err).Msg("failed to read balance for open phase")
		} else {
			localBalance := 0.0
			if balance != nil {
				localBalance = *balance
			}
			// Add accumulated close deltas.
			localBalance += netDelta

			logger.Info().
				Float64("effective_balance", localBalance).
				Int("open_count", len(opens)).
				Msg("starting open phase")

			for _, sig := range opens {
				margin, err := e.processOpenBatched(ctx, sig, &localBalance)
				if err != nil {
					logger.Warn().Err(err).
						Str("product", sig.product).
						Str("action", sig.signal.Action).
						Float64("confidence", sig.signal.Confidence).
						Msg("open signal skipped during flush")
					continue
				}
				localBalance -= margin
				netDelta -= margin
			}
		}
	}

	// --- Phase 4: Single balance write ---
	if netDelta != 0 {
		if err := e.repo.AdjustBalance(ctx, tenantID, accountID, "USD", netDelta); err != nil {
			logger.Error().Err(err).Float64("delta", netDelta).Msg("failed to write batched balance")
		} else {
			logger.Info().Float64("delta", netDelta).Msg("batched balance adjusted")
		}
	}
}

// processCloseBatched processes a single close signal using InsertTradeOnly.
// Returns the balance delta (positive: margin returned + P&L) on success.
func (e *Engine) processCloseBatched(ctx context.Context, sig bufferedSignal) (float64, error) {
	logger := e.logger.With().
		Str("account", sig.accountID).
		Str("product", sig.product).
		Str("action", sig.signal.Action).
		Logger()

	// Fetch trading config (needed for market type).
	tradingConfigs, err := e.fetchTradingConfigsFn(ctx, e.cfg)
	if err != nil {
		return 0, fmt.Errorf("fetch trading configs: %w", err)
	}
	tc, ok := tradingConfigs[tradingConfigKey{accountID: sig.accountID, productID: sig.product}]
	if !ok {
		return 0, fmt.Errorf("no trading config for %s+%s", sig.accountID, sig.product)
	}

	// Check open position state.
	e.posStateMu.RLock()
	ps, exists := e.posState[posKey(sig.accountID, sig.product)]
	e.posStateMu.RUnlock()
	if !exists {
		logger.Debug().Msg("no open position for close signal, ignoring")
		return 0, nil
	}

	// Apply per-account confidence thresholds for exits.
	baseName := signalBaseName(sig.strategy)
	if params, ok := tc.StrategyParams[baseName]; ok {
		if !sig.signal.IsExit {
			if thresh, ok := params["exit_confidence"]; ok && sig.signal.Confidence > 0 && sig.signal.Confidence < thresh {
				logger.Debug().Msg("exit signal below exit_confidence threshold, skipping")
				return 0, nil
			}
		}
	}

	exitReason := "Layer 3: conviction drop"
	if sig.signal.Reason != "" {
		exitReason = sig.signal.Reason
	}

	// Execute close trade using InsertTradeOnly.
	delta, err := e.executeCloseTradeOnly(ctx, ps, sig.signal.Price, exitReason)
	if err != nil {
		return 0, err
	}

	logger.Info().
		Str("position_side", ps.Side).
		Float64("entry_price", ps.EntryPrice).
		Float64("exit_price", sig.signal.Price).
		Float64("delta", delta).
		Msg("position closed (batched)")

	return delta, nil
}

// executeCloseTradeOnly is like executeCloseTrade but uses InsertTradeOnly
// and returns the balance delta instead of adjusting balance directly.
func (e *Engine) executeCloseTradeOnly(ctx context.Context, ps *PositionState, currentPrice float64, exitReason string) (float64, error) {
	logger := e.logger.With().
		Str("account", ps.AccountID).
		Str("symbol", ps.Symbol).
		Logger()
	tenantID := e.tenantID()

	var side domain.Side
	if ps.Side == "long" {
		side = domain.SideSell
	} else {
		side = domain.SideBuy
	}

	marketType := domain.MarketType(ps.MarketType)
	now := time.Now().UTC()

	// Load quantity from open position.
	openPositions, err := e.repo.ListOpenPositionsForAccount(ctx, ps.AccountID)
	if err != nil {
		return 0, fmt.Errorf("list open positions: %w", err)
	}

	var qty float64
	for _, p := range openPositions {
		if p.Symbol == ps.Symbol && string(p.MarketType) == ps.MarketType {
			qty = p.Quantity
			break
		}
	}
	if qty <= 0 {
		return 0, fmt.Errorf("no open position quantity found")
	}

	if e.cfg.TradingMode == "live" {
		req := ClosePositionRequest{
			Symbol:     ps.Symbol,
			Side:       domain.PositionSide(ps.Side),
			MarketType: marketType,
		}
		result, err := e.exchange.ClosePosition(ctx, req)
		if err != nil {
			return 0, fmt.Errorf("exchange close: %w", err)
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

	trade := &domain.Trade{
		TenantID:     tenantID,
		TradeID:      fmt.Sprintf("engine-close-%s-%s-%d", ps.AccountID, ps.Symbol, now.UnixNano()),
		AccountID:    ps.AccountID,
		Symbol:       ps.Symbol,
		Side:         side,
		PositionSide: domain.PositionSide(ps.Side),
		Quantity:     qty,
		Price:        currentPrice,
		Fee:          0,
		FeeCurrency:  "USD",
		MarketType:   marketType,
		Timestamp:    now,
		IngestedAt:   now,
		Leverage:     leveragePtr,
		Strategy:     stratPtr,
		ExitReason:   &exitReason,
	}

	avgEntry, err := e.repo.GetAvgEntryPrice(ctx, tenantID, ps.AccountID, ps.Symbol, marketType)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get avg entry price")
	}
	costBasisForTrade(trade, avgEntry)

	inserted, err := e.repo.InsertTradeOnly(ctx, tenantID, trade)
	if err != nil {
		return 0, fmt.Errorf("insert close trade: %w", err)
	}
	if !inserted {
		return 0, nil // duplicate
	}

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

	// Destroy conviction scorer for the closed position.
	e.onConvictionPositionClose(ps.AccountID, ps.Symbol)

	e.conflictMu.Lock()
	delete(e.conflict, posKey(ps.AccountID, ps.Symbol))
	e.conflictMu.Unlock()

	return CostDeltaForTrade(trade), nil
}

// processOpenBatched processes a single open signal using InsertTradeOnly
// with the provided local balance. Returns the margin consumed on success.
func (e *Engine) processOpenBatched(ctx context.Context, sig bufferedSignal, localBalance *float64) (float64, error) {
	logger := e.logger.With().
		Str("account", sig.accountID).
		Str("product", sig.product).
		Str("action", sig.signal.Action).
		Str("strategy", sig.strategy).
		Float64("confidence", sig.signal.Confidence).
		Logger()

	// Kill switch check.
	if e.killSwitchActive() {
		return 0, fmt.Errorf("kill switch active")
	}

	// Fetch trading config.
	tradingConfigs, err := e.fetchTradingConfigsFn(ctx, e.cfg)
	if err != nil {
		return 0, fmt.Errorf("fetch trading configs: %w", err)
	}
	tc, ok := tradingConfigs[tradingConfigKey{accountID: sig.accountID, productID: sig.product}]
	if !ok {
		return 0, fmt.Errorf("no trading config for %s+%s", sig.accountID, sig.product)
	}

	// Validate strategy.
	var allowedStrategies []string
	if sig.signal.Action == "BUY" {
		allowedStrategies = tc.StrategiesLong
	} else {
		allowedStrategies = tc.StrategiesShort
	}
	strategyAllowed := false
	for _, s := range allowedStrategies {
		if s == sig.strategy || strings.HasPrefix(sig.strategy, s+"_") || strings.HasPrefix(sig.strategy, s+"+") {
			strategyAllowed = true
			break
		}
	}
	if !strategyAllowed {
		return 0, fmt.Errorf("strategy %s not allowed", sig.strategy)
	}

	// Per-account confidence threshold.
	baseName := signalBaseName(sig.strategy)
	if params, ok := tc.StrategyParams[baseName]; ok {
		if thresh, ok := params["confidence"]; ok && sig.signal.Confidence < thresh {
			return 0, fmt.Errorf("confidence %.2f below threshold %.2f", sig.signal.Confidence, thresh)
		}
	}

	side, positionSide, marketType := mapSignalToSide(sig.signal.Action, tc)

	// Daily loss limit.
	if e.cfg.DailyLossLimit > 0 && e.isDailyLossLimitReached(ctx, sig.accountID) {
		return 0, fmt.Errorf("daily loss limit reached")
	}

	// Direction conflict guard.
	e.conflictMu.Lock()
	if openSide, exists := e.conflict[posKey(sig.accountID, sig.product)]; exists {
		if openSide != string(positionSide) {
			e.conflictMu.Unlock()
			return 0, fmt.Errorf("direction conflict: open %s, want %s", openSide, string(positionSide))
		}
	}
	e.conflictMu.Unlock()

	// Per-config confidence threshold.
	if tc.MinConfidence > 0 && sig.signal.Confidence < tc.MinConfidence {
		return 0, fmt.Errorf("confidence below config threshold")
	}

	// Max positions check.
	if e.cfg.MaxPositions > 0 {
		states, err := e.repo.CountOpenPositionStates(ctx, sig.accountID)
		if err != nil {
			return 0, fmt.Errorf("count positions: %w", err)
		}
		if states >= e.cfg.MaxPositions {
			return 0, fmt.Errorf("max positions reached (%d/%d)", states, e.cfg.MaxPositions)
		}
	}

	// Calculate position size against local balance.
	size, qty, margin, err := e.calculatePositionSize(sig.signal, tc, marketType, localBalance)
	if err != nil {
		return 0, fmt.Errorf("calculate position size: %w", err)
	}

	required := margin
	if marketType == domain.MarketTypeSpot {
		required = size
	}

	if e.cfg.MinPositionSize > 0 && required < e.cfg.MinPositionSize {
		return 0, fmt.Errorf("position size $%.2f below minimum $%.2f", required, e.cfg.MinPositionSize)
	}

	if localBalance != nil && *localBalance < required {
		return 0, fmt.Errorf("insufficient balance: need $%.2f, have $%.2f", required, *localBalance)
	}

	// Build trade.
	tenantID := e.tenantID()
	now := time.Now().UTC()
	leverage := tc.LongLeverage
	if sig.signal.Action == "SHORT" {
		leverage = tc.ShortLeverage
	}

	var sl, tp *float64
	if sig.signal.StopLoss > 0 {
		v := sig.signal.StopLoss
		sl = &v
	}
	if sig.signal.TakeProfit > 0 {
		v := sig.signal.TakeProfit
		tp = &v
	}
	var conf *float64
	if sig.signal.Confidence > 0 {
		v := sig.signal.Confidence
		conf = &v
	}
	var stratPtr *string
	if sig.strategy != "" {
		v := sig.strategy
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
		TradeID:      fmt.Sprintf("engine-%s-%s-%d", sig.accountID, sig.product, now.UnixNano()),
		AccountID:    sig.accountID,
		Symbol:       sig.product,
		Side:         side,
		PositionSide: positionSide,
		Quantity:     qty,
		Price:        sig.signal.Price,
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
	if sig.signal.Reason != "" {
		r := sig.signal.Reason
		trade.EntryReason = &r
	}

	// Compute cost basis.
	if trade.MarketType == domain.MarketTypeFutures && trade.Margin != nil && *trade.Margin > 0 {
		trade.CostBasis = *trade.Margin
	} else {
		trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
	}

	// Execute via exchange in live mode.
	if e.cfg.TradingMode == "live" {
		req := OpenPositionRequest{
			Symbol:  trade.Symbol,
			Side:    positionSide,
			SizeUSD: sig.signal.Price * trade.Quantity,
			Price:   sig.signal.Price,
		}
		if trade.Leverage != nil {
			req.Leverage = *trade.Leverage
		}
		result, err := e.exchange.OpenPosition(ctx, req)
		if err != nil {
			return 0, fmt.Errorf("exchange open: %w", err)
		}
		trade.Price = result.FillPrice
		trade.Quantity = result.Quantity
		trade.Fee = result.Fee
		if trade.Margin != nil && result.Margin > 0 {
			m := result.Margin
			trade.Margin = &m
		}
		// Recalculate cost basis with actual fill.
		if trade.MarketType == domain.MarketTypeFutures && trade.Margin != nil && *trade.Margin > 0 {
			trade.CostBasis = *trade.Margin
		} else {
			trade.CostBasis = trade.Quantity*trade.Price + trade.Fee
		}
	}

	logger.Info().
		Str("market_type", string(marketType)).
		Str("position_side", string(positionSide)).
		Float64("size_usd", size).
		Float64("qty", qty).
		Int("leverage", leverage).
		Msg("opening position (batched)")

	inserted, err := e.repo.InsertTradeOnly(ctx, tenantID, trade)
	if err != nil {
		return 0, fmt.Errorf("insert open trade: %w", err)
	}
	if !inserted {
		return 0, nil // duplicate
	}

	if e.publisher != nil {
		e.publisher.Publish(trade.AccountID, trade)
	}

	// Update cooldown.
	key := cooldownKey{accountID: sig.accountID, symbol: sig.product, action: sig.signal.Action}
	e.cooldownMu.Lock()
	e.cooldown[key] = time.Now().Add(5 * time.Minute)
	e.cooldownMu.Unlock()

	// Update conflict guard.
	e.conflictMu.Lock()
	e.conflict[posKey(sig.accountID, sig.product)] = string(positionSide)
	e.conflictMu.Unlock()

	// Compute hard stop and persist position state.
	hardStop := risk.ComputeHardStop(sig.signal.Price, string(positionSide), leverage, string(marketType))
	dbState := &EnginePositionState{
		AccountID:   sig.accountID,
		Symbol:      sig.product,
		MarketType:  string(marketType),
		Side:        string(positionSide),
		EntryPrice:  sig.signal.Price,
		HardStop:    hardStop,
		Leverage:    leverage,
		Strategy:    sig.strategy,
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
		e.posState[posKey(sig.accountID, sig.product)] = ps
		e.posStateMu.Unlock()

		// Create conviction scorer for the new position.
		e.onConvictionPositionOpen(ctx, ps)
	}

	return required, nil
}
