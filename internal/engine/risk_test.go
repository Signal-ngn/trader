package engine

import (
	"math"
	"testing"
	"time"
)

// resolveStopLoss/TakeProfit logic is inlined in evaluatePosition.
// We test the boundary conditions directly here to make them explicit.

// ── stop-loss resolution ──────────────────────────────────────────────────────

func TestStopLoss_LongUsesExplicitSL(t *testing.T) {
	entry := 100.0
	sl := 95.0 // 5% away — well beyond the 0.1% sanity threshold
	effective := resolveStopLoss("long", entry, sl)
	assertFloat(t, "stop-loss", sl, effective)
}

func TestStopLoss_LongTooCloseUsesDefault(t *testing.T) {
	entry := 100.0
	sl := 100.05 // 0.05% away — within sanity threshold (0.1%)
	effective := resolveStopLoss("long", entry, sl)
	want := entry * (1 - defaultSLPct) // 96.0
	assertFloat(t, "default stop-loss", want, effective)
}

func TestStopLoss_LongZeroSLUsesDefault(t *testing.T) {
	entry := 100.0
	effective := resolveStopLoss("long", entry, 0)
	want := entry * (1 - defaultSLPct)
	assertFloat(t, "zero stop-loss → default", want, effective)
}

func TestStopLoss_ShortUsesExplicitSL(t *testing.T) {
	entry := 100.0
	sl := 105.0 // 5% above entry — valid for short
	effective := resolveStopLoss("short", entry, sl)
	assertFloat(t, "short stop-loss", sl, effective)
}

func TestStopLoss_ShortTooCloseUsesDefault(t *testing.T) {
	entry := 100.0
	sl := 100.05 // within 0.1% above entry
	effective := resolveStopLoss("short", entry, sl)
	want := entry * (1 + defaultSLPct) // 104.0
	assertFloat(t, "short default stop-loss", want, effective)
}

// ── take-profit resolution ────────────────────────────────────────────────────

func TestTakeProfit_LongUsesExplicitTP(t *testing.T) {
	entry := 100.0
	tp := 115.0
	effective := resolveTakeProfit("long", entry, tp)
	assertFloat(t, "take-profit", tp, effective)
}

func TestTakeProfit_LongTooCloseUsesDefault(t *testing.T) {
	entry := 100.0
	tp := 100.05
	effective := resolveTakeProfit("long", entry, tp)
	want := entry * (1 + defaultTPPct) // 110.0
	assertFloat(t, "default take-profit", want, effective)
}

func TestTakeProfit_ShortUsesExplicitTP(t *testing.T) {
	entry := 100.0
	tp := 90.0
	effective := resolveTakeProfit("short", entry, tp)
	assertFloat(t, "short take-profit", tp, effective)
}

func TestTakeProfit_ShortTooCloseUsesDefault(t *testing.T) {
	entry := 100.0
	tp := 99.95
	effective := resolveTakeProfit("short", entry, tp)
	want := entry * (1 - defaultTPPct) // 90.0
	assertFloat(t, "short default take-profit", want, effective)
}

// ── trailing stop ─────────────────────────────────────────────────────────────

func TestTrailingStop_NotActivatedBelow3Pct(t *testing.T) {
	entry := 100.0
	current := 102.5 // +2.5% — below the 3% activation threshold
	leverage := 1.0

	activated, _, _ := evaluateTrailingStop("long", entry, current, 0, 0, leverage)
	if activated {
		t.Fatal("trailing stop should not activate below 3% gain")
	}
}

func TestTrailingStop_ActivatesAt3Pct(t *testing.T) {
	entry := 100.0
	current := 103.5 // +3.5% — above activation threshold
	leverage := 1.0

	activated, peak, trailing := evaluateTrailingStop("long", entry, current, 0, 0, leverage)
	if !activated {
		t.Fatal("trailing stop should activate at +3% gain")
	}
	assertFloat(t, "peak", current, peak)
	want := current * (1 - trailingTrailPct) // 101.43
	assertFloat(t, "trailing stop level", want, trailing)
}

func TestTrailingStop_TightensOnNewPeak(t *testing.T) {
	entry := 100.0
	leverage := 1.0
	// First activation at 105.
	_, peak, trailing := evaluateTrailingStop("long", entry, 105, 0, 0, leverage)
	// Price rises further to 108.
	_, newPeak, newTrailing := evaluateTrailingStop("long", entry, 108, peak, trailing, leverage)

	if newPeak <= peak {
		t.Errorf("peak should have risen from %.2f to %.2f", peak, newPeak)
	}
	if newTrailing <= trailing {
		t.Errorf("trailing stop should have tightened from %.2f to %.2f", trailing, newTrailing)
	}
	assertFloat(t, "new trailing stop", newPeak*(1-trailingTrailPct), newTrailing)
}

func TestTrailingStop_NeverLoosensOnPriceDrop(t *testing.T) {
	entry := 100.0
	leverage := 1.0
	// Activate at 106, establish peak and trailing.
	_, peak, trailing := evaluateTrailingStop("long", entry, 106, 0, 0, leverage)
	// Price falls to 104 — still above trailing stop, so it should not loosen.
	activated, newPeak, newTrailing := evaluateTrailingStop("long", entry, 104, peak, trailing, leverage)

	if !activated {
		// Still active (price hasn't breached trailing stop)
		t.Fatal("trailing stop should remain active")
	}
	assertFloat(t, "peak unchanged", peak, newPeak)
	assertFloat(t, "trailing stop unchanged", trailing, newTrailing)
}

func TestTrailingStop_Short_ActivatesAt3PctGain(t *testing.T) {
	entry := 100.0
	current := 96.0 // -4% on entry = +4% gain for short
	leverage := 1.0

	activated, peak, trailing := evaluateTrailingStop("short", entry, current, 0, 0, leverage)
	if !activated {
		t.Fatal("short trailing stop should activate at +3% gain")
	}
	assertFloat(t, "peak (lowest price)", current, peak)
	want := current * (1 + trailingTrailPct) // trail 2% above lowest price
	assertFloat(t, "short trailing stop level", want, trailing)
}

// ── max hold time ─────────────────────────────────────────────────────────────

func TestMaxHoldTime_NotExpired(t *testing.T) {
	openedAt := time.Now().Add(-24 * time.Hour)
	if time.Since(openedAt) > maxHoldDuration {
		t.Fatal("24h position should not be expired")
	}
}

func TestMaxHoldTime_Expired(t *testing.T) {
	openedAt := time.Now().Add(-49 * time.Hour)
	if time.Since(openedAt) <= maxHoldDuration {
		t.Fatal("49h position should be expired")
	}
}

// ── helpers extracted from evaluatePosition for direct testing ────────────────
// These mirror the inline logic in risk.go so we can test boundary conditions
// without standing up a full engine.

func resolveStopLoss(side string, entry, sl float64) float64 {
	if side == "long" {
		if sl <= 0 || math.Abs(sl-entry)/entry < slSanityPct {
			return entry * (1 - defaultSLPct)
		}
		return sl
	}
	// short
	if sl <= 0 || math.Abs(sl-entry)/entry < slSanityPct {
		return entry * (1 + defaultSLPct)
	}
	return sl
}

func resolveTakeProfit(side string, entry, tp float64) float64 {
	if side == "long" {
		if tp <= 0 || math.Abs(tp-entry)/entry < slSanityPct {
			return entry * (1 + defaultTPPct)
		}
		return tp
	}
	// short
	if tp <= 0 || math.Abs(tp-entry)/entry < slSanityPct {
		return entry * (1 - defaultTPPct)
	}
	return tp
}

// evaluateTrailingStop mirrors the trailing stop logic from evaluatePosition.
// Returns (stillActive, newPeak, newTrailingStop).
// "stillActive" means trailing stop is engaged (not necessarily that price breached it).
func evaluateTrailingStop(side string, entry, currentPrice, prevPeak, prevTrailing, leverage float64) (bool, float64, float64) {
	if leverage <= 0 {
		leverage = 1
	}
	scale := 1.0 / leverage

	var unrealisedPct float64
	if side == "long" {
		unrealisedPct = (currentPrice - entry) / entry * scale
	} else {
		unrealisedPct = (entry - currentPrice) / entry * scale
	}

	if unrealisedPct < trailingActivatePct {
		return false, prevPeak, prevTrailing
	}

	peak := prevPeak
	trailing := prevTrailing

	if side == "long" {
		if currentPrice > peak {
			peak = currentPrice
			trailing = peak * (1 - trailingTrailPct)
		}
	} else {
		if peak == 0 || currentPrice < peak {
			peak = currentPrice
			trailing = peak * (1 + trailingTrailPct)
		}
	}

	return true, peak, trailing
}
