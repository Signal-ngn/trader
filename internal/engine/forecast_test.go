package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Signal-ngn/risk"

	"github.com/Signal-ngn/trader/internal/config"
)

// TestSignalPayload_ForecastRoundTrip verifies the wire shape matches the
// spot-canvas-app publisher's snake_case tags so StrategySignal → SignalPayload
// round-trips cleanly across the NATS boundary.
func TestSignalPayload_ForecastRoundTrip(t *testing.T) {
	orig := SignalPayload{
		Strategy:  "hts",
		Product:   "BTC-USD",
		Exchange:  "coinbase",
		Action:    "BUY",
		Timestamp: 1707900000,
		Forecast: &SignalForecast{
			Valid:        true,
			ProbLong:     0.72,
			ProbShort:    0.08,
			ProbHold:     0.20,
			MedianReturn: 0.0125,
			Uncertainty:  0.18,
			QuantileLow:  -0.02,
			QuantileHigh: 0.045,
			ConfirmH24:   0.009,
			Regime: Regime{
				State:          "bull_trend",
				VolatilityMult: 1.2,
				AllowLong:      true,
				AllowShort:     false,
				Confidence:     0.85,
			},
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SignalPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Forecast == nil {
		t.Fatal("forecast: nil after round-trip")
	}
	if *got.Forecast != *orig.Forecast {
		t.Errorf("forecast mismatch:\n got: %+v\nwant: %+v", *got.Forecast, *orig.Forecast)
	}
}

func TestSignalPayload_ForecastOmitempty(t *testing.T) {
	sig := SignalPayload{Strategy: "rsi_mean_reversion", Product: "ETH-USD", Action: "BUY"}
	data, _ := json.Marshal(sig)
	var asMap map[string]json.RawMessage
	_ = json.Unmarshal(data, &asMap)
	if _, present := asMap["forecast"]; present {
		t.Errorf("forecast key present when nil; JSON: %s", data)
	}
}

func TestSignalPayload_ForecastBackwardCompat(t *testing.T) {
	legacy := `{"strategy":"rsi","product":"BTC-USD","exchange":"coinbase","action":"BUY","price":42000,"timestamp":1707900000}`
	var got SignalPayload
	if err := json.Unmarshal([]byte(legacy), &got); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if got.Forecast != nil {
		t.Errorf("forecast should be nil for legacy payload; got %+v", got.Forecast)
	}
}

// TestCurrentForecast_CacheReadWrite verifies that a forecast written via the
// signal path can be read back via currentForecast and is returned by value
// (mutating the returned copy must not affect the cache).
func TestCurrentForecast_CacheReadWrite(t *testing.T) {
	e := &Engine{
		forecasts: make(map[string]*cachedForecast),
	}
	if got := e.currentForecast("binance", "SOL-USD"); got != nil {
		t.Fatalf("expected nil for uncached product; got %+v", got)
	}

	ts := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	e.forecasts[forecastKey("binance", "SOL-USD")] = &cachedForecast{
		forecast: SignalForecast{Valid: true, MedianReturn: 0.01, Uncertainty: 0.2,
			Regime: Regime{State: "normal", AllowLong: true, AllowShort: true}},
		timestamp: ts,
	}

	got := e.currentForecast("binance", "SOL-USD")
	if got == nil {
		t.Fatal("expected cached forecast; got nil")
	}
	if !got.forecast.Valid || got.forecast.MedianReturn != 0.01 || got.timestamp != ts {
		t.Errorf("unexpected cached forecast: %+v", got)
	}

	// Mutating the returned copy must not touch the cache.
	got.forecast.MedianReturn = 99
	if e.forecasts[forecastKey("binance", "SOL-USD")].forecast.MedianReturn != 0.01 {
		t.Error("mutating returned copy leaked back into cache")
	}
}

func TestToForecastSnapshot_NilSafe(t *testing.T) {
	if snap := toForecastSnapshot(nil); snap != (risk.ForecastSnapshot{}) {
		t.Errorf("nil input must yield zero snapshot; got %+v", snap)
	}
	if snap := toRegimeSnapshot(nil); snap != (risk.RegimeSnapshot{}) {
		t.Errorf("nil input must yield zero regime snapshot; got %+v", snap)
	}
}

func TestToForecastSnapshot_FieldMapping(t *testing.T) {
	ts := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	c := &cachedForecast{
		forecast: SignalForecast{
			Valid: true, MedianReturn: 0.0125, Uncertainty: 0.18,
			QuantileLow: -0.02, QuantileHigh: 0.045, ConfirmH24: 0.009,
			Regime: Regime{State: "bull_trend", AllowLong: true, AllowShort: false, VolatilityMult: 1.2},
		},
		timestamp: ts,
	}
	snap := toForecastSnapshot(c)
	want := risk.ForecastSnapshot{
		Valid: true, MedianReturnH48: 0.0125, UncertaintyH48: 0.18,
		QuantileLowH48: -0.02, QuantileHighH48: 0.045, ConfirmH24: 0.009, Timestamp: ts,
	}
	if snap != want {
		t.Errorf("forecast snapshot:\n got: %+v\nwant: %+v", snap, want)
	}
	reg := toRegimeSnapshot(c)
	wantReg := risk.RegimeSnapshot{State: "bull_trend", AllowLong: true, VolatilityMult: 1.2}
	if reg != wantReg {
		t.Errorf("regime snapshot:\n got: %+v\nwant: %+v", reg, wantReg)
	}
}

// TestOnConvictionPositionOpen_CapturesEntryForecastBaseline verifies that
// opening a position reads the cached forecast and freezes EntryUncertainty /
// EntryMedianReturn on the scorer state. Missing forecast (the fallback
// degradation path) must leave both at zero so the archived scorer behaviour
// is preserved byte-for-byte.
func TestOnConvictionPositionOpen_CapturesEntryForecastBaseline(t *testing.T) {
	e := newConvictionTestEngine(func(_ context.Context, _ *config.Config, _, _ string, limit int) ([]risk.OHLCV, error) {
		return generatedWarmupCandles(limit), nil
	})
	// Cache a forecast for the product BEFORE opening the position.
	e.forecasts[forecastKey("binance", "SOL-USD")] = &cachedForecast{
		forecast: SignalForecast{
			Valid: true, MedianReturn: 0.012, Uncertainty: 0.18,
			Regime: Regime{State: "bull_trend", AllowLong: true},
		},
		timestamp: time.Now().UTC(),
	}

	ps := &PositionState{AccountID: "acc", Symbol: "SOL-USD", Side: "long", EntryPrice: 100, OpenedAt: time.Now().UTC()}
	e.onConvictionPositionOpen(context.Background(), ps)

	cs := e.conviction.getScorer(posKey("acc", "SOL-USD"))
	if cs == nil {
		t.Fatal("scorer missing after open")
	}
	if cs.entryUncertainty != 0.18 {
		t.Errorf("entryUncertainty: got %v, want 0.18", cs.entryUncertainty)
	}
	if cs.entryMedianReturn != 0.012 {
		t.Errorf("entryMedianReturn: got %v, want 0.012", cs.entryMedianReturn)
	}
}

func TestOnConvictionPositionOpen_NoCachedForecastLeavesBaselineZero(t *testing.T) {
	e := newConvictionTestEngine(func(_ context.Context, _ *config.Config, _, _ string, limit int) ([]risk.OHLCV, error) {
		return generatedWarmupCandles(limit), nil
	})
	// Deliberately no forecast cached.
	ps := &PositionState{AccountID: "acc", Symbol: "SOL-USD", Side: "long", EntryPrice: 100, OpenedAt: time.Now().UTC()}
	e.onConvictionPositionOpen(context.Background(), ps)

	cs := e.conviction.getScorer(posKey("acc", "SOL-USD"))
	if cs == nil {
		t.Fatal("scorer missing after open")
	}
	if cs.entryUncertainty != 0 || cs.entryMedianReturn != 0 {
		t.Errorf("baseline must be zero without cached forecast; got (unc=%v, med=%v)", cs.entryUncertainty, cs.entryMedianReturn)
	}
}

// TestConvictionManager_GraceConfigPlumbed verifies that the new grace-window
// knobs land on the manager and that EffectiveThresholds applies them.
func TestConvictionManager_GraceConfigPlumbed(t *testing.T) {
	cm := newConvictionManager(0.35, 0.55, 4*time.Hour, 0.5)
	if cm.graceWindow != 4*time.Hour || cm.graceFactor != 0.5 {
		t.Fatalf("grace fields not stored: window=%v factor=%v", cm.graceWindow, cm.graceFactor)
	}

	// Inside the grace window with a valid forecast: thresholds are halved.
	posIn := risk.PositionContext{
		Forecast:    risk.ForecastSnapshot{Valid: true},
		GraceWindow: cm.graceWindow,
		ForecastAge: 1 * time.Hour,
	}
	exit, tighten := risk.EffectiveThresholds(posIn, cm.exitThreshold, cm.tightenThreshold, cm.graceFactor)
	if exit != 0.175 || tighten != 0.275 {
		t.Errorf("in-grace: got exit=%v tighten=%v, want 0.175 / 0.275", exit, tighten)
	}

	// Outside the grace window: raw thresholds.
	posOut := risk.PositionContext{
		Forecast:    risk.ForecastSnapshot{Valid: true},
		GraceWindow: cm.graceWindow,
		ForecastAge: 5 * time.Hour,
	}
	exit, tighten = risk.EffectiveThresholds(posOut, cm.exitThreshold, cm.tightenThreshold, cm.graceFactor)
	if exit != 0.35 || tighten != 0.55 {
		t.Errorf("out-of-grace: got exit=%v tighten=%v, want 0.35 / 0.55", exit, tighten)
	}
}

// TestConvictionManager_GraceDisabledPreservesRawThresholds verifies the
// default-off path: graceWindow == 0 means no ramp, raw thresholds apply
// regardless of forecast state.
func TestConvictionManager_GraceDisabledPreservesRawThresholds(t *testing.T) {
	cm := newConvictionManager(0.35, 0.55, 0, 0)
	pos := risk.PositionContext{
		Forecast:    risk.ForecastSnapshot{Valid: true},
		ForecastAge: 1 * time.Hour, // inside would-be grace window, if one existed
	}
	exit, tighten := risk.EffectiveThresholds(pos, cm.exitThreshold, cm.tightenThreshold, cm.graceFactor)
	if exit != 0.35 || tighten != 0.55 {
		t.Errorf("ramp disabled must yield raw thresholds; got exit=%v tighten=%v", exit, tighten)
	}
}

// hasSignal is a tiny helper for scoring tests.
func hasSignal(sigs []risk.ErosionSignal, name string) bool {
	for _, s := range sigs {
		if s.Name == name {
			return true
		}
	}
	return false
}

// warmScorer returns a ConvictionScorer warmed with 30 synthetic candles
// (monotonically upward trend) so Score() produces a deterministic baseline.
func warmScorer(t *testing.T) *risk.ConvictionScorer {
	t.Helper()
	cs := risk.NewConvictionScorer()
	for _, c := range generatedWarmupCandles(30) {
		cs.Update(c)
	}
	if !cs.IsWarm() {
		t.Fatalf("scorer not warm after 30 candles; count=%d", cs.CandleCount())
	}
	return cs
}

// TestScore_MissingForecastMatchesArchivedBaseline exercises the archived
// behaviour path: when PositionContext carries no forecast data, the forecast-
// and regime-based erosion signals must NOT fire, and the score must equal the
// score of an equally-zeroed context — i.e. the caller's zero-valued degradation
// path is byte-for-byte compatible with the archived scorer. Task 16.
func TestScore_MissingForecastMatchesArchivedBaseline(t *testing.T) {
	cs := warmScorer(t)

	posArchived := risk.PositionContext{Side: "long", EntryPrice: 100, EntryATR: 1.5}
	posZeroed := risk.PositionContext{
		Side: "long", EntryPrice: 100, EntryATR: 1.5,
		Forecast: risk.ForecastSnapshot{}, // zero-valued — Valid: false
		Regime:   risk.RegimeSnapshot{},   // empty State
	}

	scoreA, _ := cs.Score(posArchived)
	scoreB, sigsB := cs.Score(posZeroed)

	if scoreA != scoreB {
		t.Errorf("zero-valued forecast must preserve archived score: got %v vs %v", scoreA, scoreB)
	}
	for _, name := range []string{
		"tft_direction_flip_h48",
		"tft_confirm_disagree_h24",
		"tft_uncertainty_expansion",
		"regime_adverse",
		"tft_reconfirm_bonus",
	} {
		if hasSignal(sigsB, name) {
			t.Errorf("erosion signal %q must NOT fire with zero-valued forecast; got signals=%+v", name, sigsB)
		}
	}
}

// TestScore_AdverseRegimeFlipFiresErosionSignal verifies end-to-end that the
// regime data carried on the signal wire reaches Score() and causes the
// regime_adverse erosion signal to fire for a long position whose regime no
// longer allows longs. Task 17.
func TestScore_AdverseRegimeFlipFiresErosionSignal(t *testing.T) {
	cs := warmScorer(t)

	// Build the posCtx the same way handleCandleMessage does, from a cached
	// forecast whose regime blocks longs.
	cached := &cachedForecast{
		forecast: SignalForecast{
			Valid:        true,
			MedianReturn: 0.01,
			Uncertainty:  0.15,
			Regime: Regime{
				State:          "bear_trend",
				AllowLong:      false, // ← the adverse flip
				AllowShort:     true,
				VolatilityMult: 1.0,
			},
		},
		timestamp: time.Now().UTC().Add(-30 * time.Minute),
	}
	pos := risk.PositionContext{
		Side: "long", EntryPrice: 100, EntryATR: 1.5,
		Forecast:    toForecastSnapshot(cached),
		Regime:      toRegimeSnapshot(cached),
		ForecastAge: 30 * time.Minute,
	}

	_, sigs := cs.Score(pos)
	if !hasSignal(sigs, "regime_adverse") {
		t.Errorf("regime_adverse must fire for long position with AllowLong=false; got signals=%+v", sigs)
	}
}

