## 1. Create `github.com/Signal-ngn/risk` library

- [ ] 1.1 Create new GitHub repository `github.com/Signal-ngn/risk` and initialise Go module (`go mod init github.com/Signal-ngn/risk`, `go 1.24`)
- [ ] 1.2 Define `Position` struct with all static and mutable fields (EntryPrice, Side, StopLoss, TakeProfit, HardStop, Leverage, Strategy, Granularity, MarketType, OpenedAt, PeakPrice, TrailingStop)
- [ ] 1.3 Define `ExitDecision` struct (Layer int, Label string, Detail string, ExitReason string) and internal `exitReason(layer int, label, detail string) string` helper producing `"Layer N: label — detail"`
- [ ] 1.4 Implement `ComputeHardStop(entryPrice float64, side string, leverage int, marketType string) float64` — formula `max(30%/leverage, 7%)`, applied to entry price for both long and short
- [ ] 1.5 Implement `IsMLStrategy(strategy string) bool` — returns true when `strings.HasPrefix(strategy, "ml_")`
- [ ] 1.6 Implement `MaxHoldDuration(strategy, granularity string) time.Duration` — lookup table from spec (ml_xgboost/5m=2.5h, ml_transformer/5m=2h, ml_transformer/1h=12h, rule-based/5m=4h, rule-based/1h=24h, default=48h)
- [ ] 1.7 Implement internal `slDistance(pos *Position) float64` — `abs(EntryPrice - StopLoss)`, fallback to `EntryPrice × 0.04` when StopLoss is zero
- [ ] 1.8 Implement `Evaluate(pos *Position, high, low, close float64, now time.Time) (ExitDecision, bool)` with layers in priority order:
  - Layer 2: hard stop — check `low <= HardStop` (long) or `high >= HardStop` (short); include `%` adverse and leverage in detail
  - Layer 1: signal SL — check `low <= StopLoss` (long, SL > 0) or `high >= StopLoss` (short, SL > 0); include prices in detail
  - Layer 4: trailing stop — only when `IsMLStrategy`; update PeakPrice/TrailingStop from `close`; check trailing stop breach; include stop and best price in detail
  - Layer 5: time exit — check `now.Sub(OpenedAt) > MaxHoldDuration`; include candle count and elapsed duration in detail
  - Layer 6: signal TP — only when `!IsMLStrategy` and TP > 0; check `close >= TakeProfit` (long) or `close <= TakeProfit` (short); include prices in detail
- [ ] 1.9 Write unit tests for `ComputeHardStop` covering all leverage values (1/spot, 2×, 3×, 5×) and both sides
- [ ] 1.10 Write unit tests for `MaxHoldDuration` covering all strategy/granularity combinations including defaults
- [ ] 1.11 Write unit tests for `Evaluate` — hard stop fires on Low (long) / High (short), not on Close alone
- [ ] 1.12 Write unit tests for `Evaluate` — signal SL fires correctly for long and short
- [ ] 1.13 Write unit tests for `Evaluate` — trailing stop: breakeven activation, active trail advancement, trail does not retreat, exit fires on Close
- [ ] 1.14 Write unit tests for `Evaluate` — time exit fires after hold limit; does not fire before
- [ ] 1.15 Write unit tests for `Evaluate` — priority ordering: hard stop beats signal SL on same candle; trailing stop beats time exit; signal TP skipped for ML strategies
- [ ] 1.16 Write unit tests for `Evaluate` — SL=0 fallback: hard stop still fires; trailing stop uses 4% fallback slDistance
- [ ] 1.17 Tag `v0.1.0` and verify `go get github.com/Signal-ngn/risk@v0.1.0` resolves correctly

## 2. DB migration (trader)

- [ ] 2.1 Write migration SQL: `ALTER TABLE engine_position_state ADD COLUMN hard_stop NUMERIC, ADD COLUMN granularity TEXT` — both nullable so existing rows are unaffected
- [ ] 2.2 Update `InsertPositionState` SQL to include `hard_stop` and `granularity` in INSERT columns and ON CONFLICT DO UPDATE SET clause
- [ ] 2.3 Update `LoadPositionStates` SQL to SELECT `COALESCE(hard_stop, 0)` and `COALESCE(granularity, '')` and scan into new fields
- [ ] 2.4 Add `HardStop float64` and `Granularity string` to `store.EnginePositionState` struct

## 3. Engine PositionState updates (trader)

- [ ] 3.1 Add `HardStop float64`, `Granularity string`, and `Closing bool` fields to `engine.PositionState` struct
- [ ] 3.2 Update `loadStartupState` to copy `HardStop` and `Granularity` from `store.EnginePositionState` into `engine.PositionState`

## 4. Engine entry path (trader)

- [ ] 4.1 Add `go get github.com/Signal-ngn/risk@v0.1.0` to trader go.mod
- [ ] 4.2 In `handleOpenSignal`: compute `hardStop = risk.ComputeHardStop(signal.Price, side, leverage, marketType)` and store in `dbState.HardStop` and `ps.HardStop`
- [ ] 4.3 In `handleOpenSignal`: store `tradingConfig.Granularity` in `dbState.Granularity` and `ps.Granularity`

## 5. Engine evaluation path (trader)

- [ ] 5.1 Add `evaluateOpenPositionsForSymbol(ctx context.Context, product string)` method that iterates `posState` and calls `evaluatePosition` for each state whose `Symbol` matches
- [ ] 5.2 In `handleSignal`, after caching `lastPrice[product]`, launch `go e.evaluateOpenPositionsForSymbol(ctx, product)` (non-blocking so NATS handler is not delayed)
- [ ] 5.3 Reduce `riskLoopInterval` constant from `5 * time.Minute` to `30 * time.Second`
- [ ] 5.4 Refactor `evaluatePosition` to build a `risk.Position` from `PositionState` and call `risk.Evaluate(riskPos, currentPrice, currentPrice, currentPrice, time.Now())` (tick mode: same price for high/low/close)
- [ ] 5.5 In `evaluatePosition`: when `risk.Evaluate` returns an exit, check `ps.Closing` under `posStateMu` write lock; set `ps.Closing = true` before calling `executeCloseTrade` to prevent double-close from concurrent goroutines
- [ ] 5.6 In `evaluatePosition`: when `risk.Evaluate` returns no exit but `riskPos.PeakPrice` or `riskPos.TrailingStop` changed (trailing stop advanced), write updated values back to `ps` and persist via `repo.UpdatePositionState`
- [ ] 5.7 Remove the old manual SL/TP/trailing/hold-time logic from `evaluatePosition` (replaced by library call in 5.4)
- [ ] 5.8 Remove now-unused constants: `trailingActivatePct`, `trailingTrailPct`, `defaultTPPct`, `maxHoldDuration` (keep `defaultSLPct` as it is used by the library fallback and may still be referenced)

## 6. Engine close path (trader)

- [ ] 6.1 Update `executeCloseTrade` to accept a `exitReason string` parameter (already does) — verify callers from the risk loop pass `decision.ExitReason` from `risk.ExitDecision`
- [ ] 6.2 In `handleCloseSignal`: set `exitReason` to the signal's `Reason` field when non-empty, otherwise `"Layer 3: conviction drop"`

## 7. Backtester updates (spot-canvas-app)

- [ ] 7.1 Add `go get github.com/Signal-ngn/risk@v0.1.0` to spot-canvas-app go.mod
- [ ] 7.2 Add `ExitReason string` field to backtest `Trade` struct
- [ ] 7.3 In `Backtester.Run`: at entry, build a `risk.Position` (EntryPrice, Side, StopLoss, TakeProfit, HardStop via `risk.ComputeHardStop`, Leverage, Strategy from `strat.Name()`, Granularity from `candle.Granularity.String()`, MarketType, OpenedAt from `candle.Timestamp`)
- [ ] 7.4 Replace / extend `checkSLTP` to call `risk.Evaluate(riskPos, candle.High, candle.Low, candle.Close, candle.Timestamp)` — use candle range so hard stop and signal SL are checked against High/Low, trailing stop against Close
- [ ] 7.5 When `risk.Evaluate` returns an exit, record `decision.ExitReason` on the `Trade` and close the position; when it returns no exit but trailing state changed, update `riskPos` in place
- [ ] 7.6 Write a regression test: 1h futures-short at 2× leverage with a candle that moves 16% adverse — verify hard stop fires and the trade does NOT reach liquidation loss

## 8. Verification

- [ ] 8.1 Run full trader test suite; confirm no regressions in existing engine behaviour
- [ ] 8.2 Run `go vet ./...` and `go build ./...` in both trader and spot-canvas-app
- [ ] 8.3 Open a paper position on the engine (via test signal), let it run and verify hard stop exit appears in logs with correct `"Layer 2: ..."` exit_reason in the ledger trade record
- [ ] 8.4 Verify trailing stop state is persisted: open a paper position, let it profit past 1× SL distance, restart the engine, confirm `peak_price` and `trailing_stop` are restored from DB
- [ ] 8.5 Run a 1h futures-short backtest on XLM or SUI with the updated backtester and confirm max drawdown is bounded by the hard stop (no 100%+ DD)
