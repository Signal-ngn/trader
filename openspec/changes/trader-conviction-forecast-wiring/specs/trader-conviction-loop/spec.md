## ADDED Requirements

### Requirement: Forecast ingestion on signal stream

The engine SHALL parse the optional `forecast` object on incoming `SignalPayload` messages (published on `signals.<exchange>.<product>.<granularity>.<strategy>`) and cache the most recent forecast per product, keyed by `exchange + "/" + product`. The cache SHALL NOT be persisted — restart warms from the next signal.

When the incoming payload omits `forecast` (non-forecast-aware strategies or older publishers), the engine SHALL NOT update the cache and SHALL NOT treat it as an error. The signal itself MUST still be processed normally (allowlist, confidence floor, routing, etc.).

#### Scenario: Forecast attached to signal updates cache
- **WHEN** a `SignalPayload` for `coinbase:BTC-USD` arrives carrying `forecast` with `valid: true, prob_long: 0.72, median_return: 0.0125, uncertainty: 0.18, regime: { state: "bull_trend", allow_long: true, ... }`
- **THEN** the engine's forecast cache for key `"coinbase/BTC-USD"` SHALL hold those values after the signal is processed

#### Scenario: Missing forecast leaves cache untouched
- **WHEN** a `SignalPayload` for `coinbase:ETH-USD` arrives with no `forecast` field
- **THEN** the engine SHALL process the signal normally and the forecast cache for `"coinbase/ETH-USD"` SHALL remain at whatever value (or absence) it held before

#### Scenario: Legacy publisher remains compatible
- **WHEN** a producer that predates the `forecast` field publishes a signal
- **THEN** the engine SHALL parse it into `SignalPayload` with `Forecast == nil` and SHALL NOT log any parse error

---

### Requirement: Entry-time forecast baseline capture

When a position is opened — either at runtime via `onConvictionPositionOpen` or during startup warm-up via `warmScorer` — the engine SHALL read the current cached forecast for `(exchange, product)` and freeze the forecast baseline by calling `scorer.MarkEntryForecast(snapshot)`. The captured `EntryUncertainty` and `EntryMedianReturn` SHALL be stored on `convictionState` and SHALL NOT be re-read from the cache on subsequent 15M ticks.

When no forecast is cached at position open (cold restart, pre-publisher rollout, missed signal), the captured baseline SHALL remain zero. Zero `EntryUncertainty` disables the `tft_uncertainty_expansion` erosion signal and preserves archived scorer behaviour for that position's lifetime.

#### Scenario: Baseline captured from cached forecast
- **WHEN** a forecast for `coinbase:BTC-USD` (`uncertainty: 0.18, median_return: 0.012`) is cached
- **AND** a position on that product opens and the conviction scorer is initialised
- **THEN** `convictionState.entryUncertainty == 0.18` and `convictionState.entryMedianReturn == 0.012` for the lifetime of the position

#### Scenario: No cached forecast at open
- **WHEN** a position opens on a product with no cached forecast
- **THEN** `convictionState.entryUncertainty == 0` and the uncertainty-expansion erosion signal SHALL NOT fire on subsequent ticks regardless of forecast evolution

---

### Requirement: ForecastSnapshot threaded into Score()

On every 15M candle that passes `shouldProcessCandle`, the engine SHALL build a `risk.PositionContext` populated with:

- `Side`, `EntryPrice`, `EntryATR`, `SignalAge` — as before.
- `Forecast` — a `risk.ForecastSnapshot` derived from the current cached forecast for `(exchange, product)`; zero-valued when absent.
- `Regime` — a `risk.RegimeSnapshot` derived from the cached forecast's nested regime; empty `State` when absent.
- `ForecastAge` — `time.Since(cachedForecast.Timestamp)` when a cached forecast exists; zero otherwise.
- `EntryUncertainty`, `EntryMedianReturn` — the values frozen at position open.
- `GraceWindow` — the engine-configured grace window (default `4h`, configurable via env).

The resulting context SHALL be passed to `scorer.Score(pos)`.

#### Scenario: Zero-valued forecast preserves archived behaviour
- **WHEN** the forecast cache for a product is empty AND the scorer is warm
- **THEN** `Score()` SHALL return the same (score, signals) as the archived strategy-agnostic scorer — no TFT or regime-based signal fires

#### Scenario: Adverse regime flip fires erosion signal
- **WHEN** a long position is open on a product whose cached forecast now has `regime.allow_long == false`
- **THEN** the `regime_adverse` erosion signal SHALL appear in the `Score()` output and the score SHALL decrease by `weightRegimeAdverse` (0.15)

---

### Requirement: Grace-window threshold ramp

The engine SHALL apply the grace-window threshold ramp at the action-decision step: effective thresholds are computed via `risk.EffectiveThresholds(pos, exitThreshold, tightenThreshold, graceFactor)` and compared against the raw score to decide exit / tighten / healthy. The grace window and ramp factor SHALL be configured from environment variables `CONVICTION_GRACE_WINDOW` (default `4h`) and `CONVICTION_GRACE_RAMP` (default `0.5`).

When `GraceWindow == 0` or the forecast is invalid, the raw thresholds SHALL be used (no ramp) — this preserves the current production behaviour if the ramp is explicitly disabled.

#### Scenario: Score below ramped exit threshold triggers tighten, not exit
- **GIVEN** `exitThreshold == 0.35`, `tightenThreshold == 0.55`, `graceFactor == 0.5`, `graceWindow == 4h`
- **WHEN** a position's `ForecastAge == 1h` and the raw score is `0.22`
- **THEN** the effective exit threshold is `0.175` (0.5 × 0.35), the score `0.22` is above it, and the action SHALL be `tighten` (`0.22 <= 0.275` effective tighten = 0.5 × 0.55)

#### Scenario: Same score after grace window triggers exit
- **WHEN** the same position's `ForecastAge == 4h1m` with score `0.22`
- **THEN** effective thresholds equal the raw thresholds, `0.22 <= 0.35`, and the action SHALL be `exit`

#### Scenario: Ramp inert when GraceWindow is zero
- **WHEN** `CONVICTION_GRACE_WINDOW` is unset (default 0) or `CONVICTION_GRACE_RAMP` is 0
- **THEN** `EffectiveThresholds` SHALL return the raw thresholds and the loop SHALL behave identically to the archived strategy-agnostic loop
