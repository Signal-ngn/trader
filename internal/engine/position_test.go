package engine

import (
	"testing"
	"time"

	"github.com/Signal-ngn/trader/internal/config"
	"github.com/Signal-ngn/trader/internal/domain"
)

// makeEngine builds a minimal Engine for unit tests — no DB, no exchange.
func makeEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:       cfg,
		posState:  make(map[string]*PositionState),
		cooldown:  make(map[cooldownKey]time.Time),
		conflict:  make(map[string]string),
		lastPrice: make(map[string]float64),
	}
}

// ── mapSignalToSide ───────────────────────────────────────────────────────────

func TestMapSignalToSide_BUY_Futures(t *testing.T) {
	tc := &TradingConfig{StrategiesLong: []string{"ml_xgboost"}}
	side, posSide, mt := mapSignalToSide("BUY", tc)
	assertEq(t, "side", string(domain.SideBuy), string(side))
	assertEq(t, "posSide", string(domain.PositionSideLong), string(posSide))
	assertEq(t, "marketType", string(domain.MarketTypeFutures), string(mt))
}

func TestMapSignalToSide_SHORT_Futures(t *testing.T) {
	tc := &TradingConfig{StrategiesShort: []string{"ml_xgboost"}}
	side, posSide, mt := mapSignalToSide("SHORT", tc)
	assertEq(t, "side", string(domain.SideSell), string(side))
	assertEq(t, "posSide", string(domain.PositionSideShort), string(posSide))
	assertEq(t, "marketType", string(domain.MarketTypeFutures), string(mt))
}

func TestMapSignalToSide_SELL_Futures(t *testing.T) {
	tc := &TradingConfig{StrategiesLong: []string{"ml_xgboost"}}
	side, posSide, _ := mapSignalToSide("SELL", tc)
	assertEq(t, "side", string(domain.SideSell), string(side))
	assertEq(t, "posSide", string(domain.PositionSideLong), string(posSide))
}

func TestMapSignalToSide_COVER_Futures(t *testing.T) {
	tc := &TradingConfig{StrategiesShort: []string{"ml_xgboost"}}
	side, posSide, _ := mapSignalToSide("COVER", tc)
	assertEq(t, "side", string(domain.SideBuy), string(side))
	assertEq(t, "posSide", string(domain.PositionSideShort), string(posSide))
}

func TestMapSignalToSide_BUY_Spot(t *testing.T) {
	// No long/short strategies → spot market.
	tc := &TradingConfig{StrategiesSpot: []string{"ml_xgboost"}}
	_, _, mt := mapSignalToSide("BUY", tc)
	assertEq(t, "marketType", string(domain.MarketTypeSpot), string(mt))
}

// ── calculatePositionSize ─────────────────────────────────────────────────────

func TestCalcSize_DefaultPct(t *testing.T) {
	cfg := &config.Config{
		PortfolioSize:   10000,
		PositionSizePct: 15,
	}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 1}
	signal := SignalPayload{Price: 50000}

	size, qty, _, err := e.calculatePositionSize(signal, tc, domain.MarketTypeSpot)
	if err != nil {
		t.Fatal(err)
	}
	// 10000 * 15% = 1500
	assertFloat(t, "size", 1500, size)
	// qty = 1500 / 50000 = 0.03
	assertFloat(t, "qty", 0.03, qty)
}

func TestCalcSize_SignalPositionPctOverride(t *testing.T) {
	cfg := &config.Config{
		PortfolioSize:   10000,
		PositionSizePct: 10, // would give 1000, but signal overrides
	}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 1}
	// signal.PositionPct is 0–1 fraction; 0.20 → 20%
	signal := SignalPayload{Price: 50000, PositionPct: 0.20}

	size, _, _, err := e.calculatePositionSize(signal, tc, domain.MarketTypeSpot)
	if err != nil {
		t.Fatal(err)
	}
	// 10000 * 20% = 2000
	assertFloat(t, "size", 2000, size)
}

func TestCalcSize_ClampedToMax(t *testing.T) {
	cfg := &config.Config{
		PortfolioSize:   10000,
		PositionSizePct: 50, // would give 5000
		MaxPositionSize: 2000,
	}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 1}
	signal := SignalPayload{Price: 50000}

	size, _, _, err := e.calculatePositionSize(signal, tc, domain.MarketTypeSpot)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, "size (clamped to max)", 2000, size)
}

func TestCalcSize_ClampedToMin(t *testing.T) {
	cfg := &config.Config{
		PortfolioSize:   10000,
		PositionSizePct: 1, // would give 100
		MinPositionSize: 500,
	}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 1}
	signal := SignalPayload{Price: 50000}

	size, _, _, err := e.calculatePositionSize(signal, tc, domain.MarketTypeSpot)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, "size (clamped to min)", 500, size)
}

func TestCalcSize_FuturesMarginWithLeverage(t *testing.T) {
	cfg := &config.Config{
		PortfolioSize:   10000,
		PositionSizePct: 20, // 2000 notional
	}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 4}
	signal := SignalPayload{Price: 50000}

	size, qty, margin, err := e.calculatePositionSize(signal, tc, domain.MarketTypeFutures)
	if err != nil {
		t.Fatal(err)
	}
	assertFloat(t, "size", 2000, size)
	assertFloat(t, "qty", 2000.0/50000, qty)
	// margin = size / leverage = 2000 / 4 = 500
	assertFloat(t, "margin", 500, margin)
}

func TestCalcSize_FuturesLeverageZeroDefaultsToOne(t *testing.T) {
	cfg := &config.Config{PortfolioSize: 10000, PositionSizePct: 10}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 0} // zero → treated as 1
	signal := SignalPayload{Price: 1000}

	size, _, margin, err := e.calculatePositionSize(signal, tc, domain.MarketTypeFutures)
	if err != nil {
		t.Fatal(err)
	}
	// leverage defaults to 1, so margin = size
	assertFloat(t, "margin", size, margin)
}

func TestCalcSize_ZeroPriceError(t *testing.T) {
	cfg := &config.Config{PortfolioSize: 10000, PositionSizePct: 10}
	e := makeEngine(cfg)
	tc := &TradingConfig{LongLeverage: 1}
	signal := SignalPayload{Price: 0}

	_, _, _, err := e.calculatePositionSize(signal, tc, domain.MarketTypeSpot)
	if err == nil {
		t.Fatal("expected error for zero price")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertEq(t *testing.T, name, want, got string) {
	t.Helper()
	if want != got {
		t.Errorf("%s: want %q, got %q", name, want, got)
	}
}

func assertFloat(t *testing.T, name string, want, got float64) {
	t.Helper()
	const epsilon = 1e-9
	diff := want - got
	if diff < -epsilon || diff > epsilon {
		t.Errorf("%s: want %v, got %v", name, want, got)
	}
}
