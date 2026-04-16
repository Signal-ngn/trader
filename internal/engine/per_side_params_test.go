package engine

import (
	"reflect"
	"testing"
)

func TestSideFromAction(t *testing.T) {
	cases := map[string]string{
		"BUY":     "long",
		"SELL":    "long",
		"SHORT":   "short",
		"COVER":   "short",
		"":        "",
		"UNKNOWN": "",
	}
	for action, want := range cases {
		if got := sideFromAction(action); got != want {
			t.Errorf("sideFromAction(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestEffectiveStrategyParams_OnlyShared(t *testing.T) {
	tc := &TradingConfig{
		StrategyParams: map[string]map[string]float64{
			"hts": {"confidence": 0.8},
		},
	}
	got := tc.effectiveStrategyParams("long", "hts")
	want := map[string]float64{"confidence": 0.8}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEffectiveStrategyParams_OnlySideOverride(t *testing.T) {
	tc := &TradingConfig{
		StrategyParamsShort: map[string]map[string]float64{
			"hts": {"uncertainty_cap": 1.7},
		},
	}
	got := tc.effectiveStrategyParams("short", "hts")
	want := map[string]float64{"uncertainty_cap": 1.7}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEffectiveStrategyParams_PerKeyMerge(t *testing.T) {
	// Mirrors the spec's worked example: shared has confidence=0.8, short has
	// uncertainty_cap=1.7. The short side should see both keys; the long side
	// should see only confidence (no override).
	tc := &TradingConfig{
		StrategyParams: map[string]map[string]float64{
			"hts": {"confidence": 0.8},
		},
		StrategyParamsShort: map[string]map[string]float64{
			"hts": {"uncertainty_cap": 1.7},
		},
	}

	short := tc.effectiveStrategyParams("short", "hts")
	wantShort := map[string]float64{"confidence": 0.8, "uncertainty_cap": 1.7}
	if !reflect.DeepEqual(short, wantShort) {
		t.Errorf("short side: got %v, want %v", short, wantShort)
	}

	long := tc.effectiveStrategyParams("long", "hts")
	wantLong := map[string]float64{"confidence": 0.8}
	if !reflect.DeepEqual(long, wantLong) {
		t.Errorf("long side: got %v, want %v", long, wantLong)
	}
}

func TestEffectiveStrategyParams_SideKeyOverridesSharedKey(t *testing.T) {
	// When a key is set on both shared and side, the side override wins.
	tc := &TradingConfig{
		StrategyParams: map[string]map[string]float64{
			"hts": {"confidence": 0.8},
		},
		StrategyParamsLong: map[string]map[string]float64{
			"hts": {"confidence": 0.6},
		},
	}
	long := tc.effectiveStrategyParams("long", "hts")
	if long["confidence"] != 0.6 {
		t.Errorf("expected long confidence 0.6 (override), got %v", long["confidence"])
	}
	short := tc.effectiveStrategyParams("short", "hts")
	if short["confidence"] != 0.8 {
		t.Errorf("expected short confidence 0.8 (shared, no override), got %v", short["confidence"])
	}
}

func TestEffectiveStrategyParams_NilEverything(t *testing.T) {
	tc := &TradingConfig{}
	if got := tc.effectiveStrategyParams("long", "hts"); got != nil {
		t.Errorf("expected nil for fully-empty config, got %v", got)
	}
	// nil map is safe to read from — verifies the contract callers rely on.
	if _, ok := tc.effectiveStrategyParams("short", "anything")["confidence"]; ok {
		t.Errorf("read on nil result should report ok=false")
	}
}

func TestEffectiveStrategyParams_EmptySideMapFallsBackToShared(t *testing.T) {
	// {hts: {}} means "no override keys for hts" — should not erase shared.
	tc := &TradingConfig{
		StrategyParams: map[string]map[string]float64{
			"hts": {"confidence": 0.8},
		},
		StrategyParamsLong: map[string]map[string]float64{
			"hts": {},
		},
	}
	got := tc.effectiveStrategyParams("long", "hts")
	want := map[string]float64{"confidence": 0.8}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEffectiveStrategyParams_DoesNotMutateInputs(t *testing.T) {
	// The merge must not alias or mutate the shared / per-side maps —
	// callers iterate StrategyParams elsewhere and a leaked alias would be a
	// silent corruption bug.
	shared := map[string]float64{"confidence": 0.8}
	side := map[string]float64{"uncertainty_cap": 1.7}
	tc := &TradingConfig{
		StrategyParams:      map[string]map[string]float64{"hts": shared},
		StrategyParamsShort: map[string]map[string]float64{"hts": side},
	}
	merged := tc.effectiveStrategyParams("short", "hts")
	merged["confidence"] = 0.99
	if shared["confidence"] != 0.8 {
		t.Errorf("merge mutated shared map: confidence=%v", shared["confidence"])
	}
	if _, ok := side["confidence"]; ok {
		t.Errorf("merge leaked into side map: confidence=%v", side["confidence"])
	}
}
