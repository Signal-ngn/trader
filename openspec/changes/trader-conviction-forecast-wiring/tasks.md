# Tasks

## Signal ingestion — `internal/engine/signals.go`

- [x] Add `SignalForecast` struct mirroring `strategy.SignalForecastPayload` in spot-canvas-app (snake_case JSON tags): `valid`, `prob_long`, `prob_short`, `prob_hold`, `median_return`, `uncertainty`, `quantile_low`, `quantile_high`, `confirm_h24`, nested `regime`.
- [x] Add `Regime` struct mirroring `strategy.RegimePayload`: `state`, `volatility_mult`, `allow_long`, `allow_short`, `confidence`.
- [x] Add `Forecast *SignalForecast` field to `SignalPayload` with tag `json:"forecast,omitempty"`.
- [x] In `handleSignal`, right after the `lastPrice` cache write: if `signal.Forecast != nil` take `forecastMu`, clone the forecast into `e.forecasts[exchange + "/" + product]`.

## Engine state — `internal/engine/engine.go`

- [x] Add `forecastMu sync.RWMutex` and `forecasts map[string]*SignalForecast` fields on `Engine`. Initialize `forecasts` alongside `lastPrice` in `New`.
- [x] Add helper `currentForecast(exchange, product string) *SignalForecast` that takes the RLock and returns a value copy (or nil).

## Conviction state — `internal/engine/conviction.go`

- [x] Extend `convictionState` with `entryUncertainty float64` and `entryMedianReturn float64`.
- [x] Extend `convictionManager` with `graceWindow time.Duration` and `graceFactor float64`. Update constructor signature.
- [x] In `onConvictionPositionOpen` and `warmScorer` (startup path), after `MarkEntry()` read `e.currentForecast(exchange, ps.Symbol)`, build a `risk.ForecastSnapshot` (or leave zero if nil), call `cs.scorer.MarkEntryForecast(snapshot)`, and copy the resulting `entryUncertainty` / `entryMedianReturn` onto `cs`.
- [x] In `handleCandleMessage`, replace the `posCtx` construction so that it:
  - looks up the current forecast for `(cs.exchange, ps.Symbol)`
  - builds a `risk.ForecastSnapshot` (with `Timestamp` taken from the cached forecast's signal timestamp)
  - builds a `risk.RegimeSnapshot` from the cached regime fields
  - populates `PositionContext.{Forecast, Regime, ForecastAge, EntryUncertainty, EntryMedianReturn, GraceWindow}`
- [x] Replace the naked threshold comparison with `risk.EffectiveThresholds(posCtx, cm.exitThreshold, cm.tightenThreshold, cm.graceFactor)` and compare the returned values.

## Config — `internal/config/config.go`

- [x] Add `ConvictionGraceWindow time.Duration` and `ConvictionGraceRamp float64` to `Config`.
- [x] Parse `CONVICTION_GRACE_WINDOW` (env var, `time.ParseDuration`, default `4h`) and `CONVICTION_GRACE_RAMP` (float, default `0.5`).
- [x] Pass both into `newConvictionManager` at engine construction.

## Deployment — `cloudbuild-prod.yaml`

- [x] Add `CONVICTION_GRACE_WINDOW=4h` and `CONVICTION_GRACE_RAMP=0.5` to the prod env-vars line.

## Tests — `internal/engine/conviction_test.go`

- [x] Missing-forecast path: open a position without caching any forecast; drive scorer to warmth with candles; assert score matches existing archived baseline and no forecast-based erosion signals fire.
- [x] Adverse regime flip: cache a forecast with `regime.allow_long = false` for a long position; assert `regime_adverse` signal fires and score drops by `weightRegimeAdverse`.
- [x] Grace-window ramp active: open position, score 0.22, `ForecastAge < GraceWindow`, graceFactor 0.5 → action is "tighten" (threshold scaled to 0.175) rather than "exit"; after grace expires, same score → "exit".
- [x] Forecast cache update via signal: call `handleSignal` with a payload carrying `Forecast`; assert `e.forecasts[exchange+"/"+product]` reflects the value.

## Tests — `internal/engine/signals_test.go`

- [x] JSON round-trip: marshal a `SignalPayload` with populated `Forecast` and nested `Regime`, unmarshal, assert fields match.
- [x] Omitempty: marshal a `SignalPayload` with `Forecast == nil`, assert no `"forecast"` key appears in output.
- [x] Backward-compat: unmarshal a legacy payload (no `forecast` field) into `SignalPayload`, assert `Forecast == nil` with no error.

## OpenSpec validation

- [x] `openspec validate trader-conviction-forecast-wiring --strict` passes.
- [x] `openspec change show trader-conviction-forecast-wiring` reviewed.

## Verification

- [x] `go test ./...` all green.
- [x] `go build ./...` clean.
- [ ] Staging deploy: conviction logs show `forecast_age`, `regime`, new signal names under real HTS positions.
