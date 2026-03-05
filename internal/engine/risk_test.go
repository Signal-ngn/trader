package engine

import (
	"testing"
	"time"
)

// These tests cover engine-level risk behaviour after the refactor:
// the old inline SL/TP/trailing logic has been replaced by the risk library.
// Library-level tests live in github.com/Signal-ngn/risk.

// ── riskLoopInterval ──────────────────────────────────────────────────────────

func TestRiskLoopInterval_IsThirtySeconds(t *testing.T) {
	if riskLoopInterval != 30*time.Second {
		t.Errorf("riskLoopInterval: got %v, want 30s", riskLoopInterval)
	}
}

// ── defaultSLPct ──────────────────────────────────────────────────────────────

func TestDefaultSLPct_IsFourPercent(t *testing.T) {
	if defaultSLPct != 0.04 {
		t.Errorf("defaultSLPct: got %v, want 0.04", defaultSLPct)
	}
}

// ── Closing flag ─────────────────────────────────────────────────────────────

func TestPositionState_ClosingFlag_Default(t *testing.T) {
	ps := &PositionState{
		AccountID:  "acc1",
		Symbol:     "XBT-USD",
		MarketType: "futures",
		Side:       "long",
		EntryPrice: 1.0,
		OpenedAt:   time.Now(),
	}
	if ps.Closing {
		t.Error("PositionState.Closing should default to false")
	}
}


