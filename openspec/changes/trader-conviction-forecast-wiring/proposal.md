## Why

The archived `tft-aware-conviction-scorer` change (spot-canvas-app, 2026-04-13) made `risk.ConvictionScorer` forecast-aware: `PositionContext` grew `Forecast`, `Regime`, `ForecastAge`, `EntryUncertainty`, `EntryMedianReturn`, `GraceWindow` fields; five new erosion signals fire when a forecast turns against the position or its regime flips; a grace-window threshold ramp relaxes the exit/tighten thresholds for a configurable window after entry so 15M noise cannot unwind a still-intact 4H thesis.

That change intentionally excluded trader wiring. The follow-up in spot-canvas-app (`publish-signal-forecast`, merged 2026-04-13) added the `forecast` field to the NATS `StrategySignal` payload, closing the cross-process gap.

Today the trader is re-vendored on `risk v0.3.0` but still calls `Score()` with a zero-valued `PositionContext.Forecast`, so the new TFT signals and grace ramp are inert in production — all the new scoring logic still only fires in backtests. This change consumes the forecast on the wire and passes it into `Score()` per 15M tick, bringing live conviction scoring to parity with the backtester.

## What Changes

- **Extend `SignalPayload`** (`internal/engine/signals.go`) with optional `Forecast *SignalForecast`. Define trader-local mirror structs `SignalForecast` and `Regime` with snake_case JSON tags matching `strategy.SignalForecastPayload` / `strategy.RegimePayload` in spot-canvas-app. Non-HTS producers omit the field — wire schema stays backward-compatible.
- **Add per-product forecast cache** on `Engine` — `forecasts map[string]*SignalForecast` keyed by `exchange + "/" + product`. Updated in `handleSignal` whenever the incoming payload carries `Forecast != nil`. Same pattern as the existing `lastPrice` cache.
- **Freeze `EntryUncertainty` and `EntryMedianReturn` at position open**. Extend `convictionState` with those two floats. In `onConvictionPositionOpen`, after `MarkEntry()`, look up the current cached forecast for the product, call `scorer.MarkEntryForecast(snapshot)`, and stash the captured values on `convictionState`. If no forecast is cached (e.g. pre-publisher rollout or missed message), leave the fields zero — risk library treats zero uncertainty as "no baseline" and disables the expansion signal.
- **Build `ForecastSnapshot` + `RegimeSnapshot` at the `Score()` boundary** in `handleCandleMessage`. Look up the cache by product; populate `PositionContext.{Forecast, Regime, ForecastAge, EntryUncertainty, EntryMedianReturn, GraceWindow}`. On cache miss fall back to zero-valued snapshots → archived scorer behaviour (verified byte-for-byte in risk library tests).
- **Grace-window threshold ramp**. Add `CONVICTION_GRACE_WINDOW` (default `4h`, matches the live-brain/HTS 4H bar) and `CONVICTION_GRACE_RAMP` (default `0.5`) env knobs. Wire through `config.Config` → `convictionManager` → `risk.EffectiveThresholds(pos, exit, tighten, graceFactor)` at the comparison site. Zero values preserve current production behaviour.

**Backward compatibility**: every new field is optional. The `ForecastSnapshot.Valid == false` path preserves archived scorer behaviour. The grace ramp is inert when `GraceWindow == 0`. Missing-forecast payloads (non-HTS signals) continue to trade normally — the conviction loop degrades to the 8-signal strategy-agnostic scorer.

## Capabilities

### Added Capabilities

- `trader-conviction-loop`: the trader's 15M conviction loop SHALL consume TFT forecasts published on the signal stream, cache them per product, freeze entry-time baseline values at position open, and apply the grace-window threshold ramp during `Score()` evaluation. Missing forecasts degrade to the strategy-agnostic scorer.

## Impact

- **`internal/engine/signals.go`**: new `SignalForecast` + `Regime` mirror types; `SignalPayload.Forecast` optional field; forecast cache write in `handleSignal`.
- **`internal/engine/engine.go`**: `forecasts map[string]*SignalForecast` + `forecastMu sync.RWMutex` on `Engine`.
- **`internal/engine/conviction.go`**: `convictionState.{entryUncertainty, entryMedianReturn}`; `convictionManager.{graceWindow, graceFactor}`; `onConvictionPositionOpen` captures entry baseline; `handleCandleMessage` builds `ForecastSnapshot`/`RegimeSnapshot` and uses `risk.EffectiveThresholds`.
- **`internal/config/config.go`**: `ConvictionGraceWindow time.Duration` + `ConvictionGraceRamp float64`.
- **`cloudbuild-prod.yaml`**: set `CONVICTION_GRACE_WINDOW=4h` and `CONVICTION_GRACE_RAMP=0.5`.
- **`go.mod`**: already bumped to `github.com/Signal-ngn/risk v0.3.0`.
- **Metrics**: existing `conviction_score` / `erosion_signal` panels start seeing the new TFT and regime signal names automatically — no dashboard change required, just new rows appear.
