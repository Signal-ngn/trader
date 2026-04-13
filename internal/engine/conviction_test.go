package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Signal-ngn/risk"
	"github.com/rs/zerolog"

	"github.com/Signal-ngn/trader/internal/config"
)

// newBinanceState returns a convictionManager with a single scorer bound to
// "binance" for the given key, ready to receive candles.
func newBinanceState(t *testing.T, key string) (*convictionManager, *convictionState) {
	t.Helper()
	cm := newConvictionManager(0.35, 0.55)
	cs := cm.createScorer(key, "binance")
	return cm, cs
}

func TestConvictionState_ShouldProcessCandle_FirstCandleAccepted(t *testing.T) {
	_, cs := newBinanceState(t, "acc:SOL-USD")

	ts := time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)
	if !cs.shouldProcessCandle("binance", ts) {
		t.Fatal("first candle from matching exchange should be accepted")
	}
}

func TestConvictionState_ShouldProcessCandle_RejectsOtherExchange(t *testing.T) {
	_, cs := newBinanceState(t, "acc:SOL-USD")

	ts := time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)

	if cs.shouldProcessCandle("coinbase", ts) {
		t.Fatal("candle from coinbase must be rejected when scorer is bound to binance")
	}
	if cs.shouldProcessCandle("kraken", ts) {
		t.Fatal("candle from kraken must be rejected when scorer is bound to binance")
	}
	if cs.shouldProcessCandle("okx", ts) {
		t.Fatal("candle from okx must be rejected when scorer is bound to binance")
	}
}

func TestConvictionState_ShouldProcessCandle_RejectsStaleTimestamp(t *testing.T) {
	_, cs := newBinanceState(t, "acc:SOL-USD")

	t1 := time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)
	cs.lastCandleTime = t1

	// Exact same timestamp (duplicate) must be rejected.
	if cs.shouldProcessCandle("binance", t1) {
		t.Fatal("duplicate-timestamp candle must be rejected")
	}

	// A candle from 24h earlier (stale backfill) must be rejected.
	stale := t1.Add(-24 * time.Hour)
	if cs.shouldProcessCandle("binance", stale) {
		t.Fatal("stale backfill candle must be rejected")
	}
}

func TestConvictionState_ShouldProcessCandle_AcceptsNewerTimestamp(t *testing.T) {
	_, cs := newBinanceState(t, "acc:SOL-USD")

	cs.lastCandleTime = time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)
	next := cs.lastCandleTime.Add(15 * time.Minute)

	if !cs.shouldProcessCandle("binance", next) {
		t.Fatal("next 15m candle from same exchange should be accepted")
	}
}

// makeCandle produces a candlePayload for tests.
func makeCandle(exchange, product string, ts time.Time, close float64) candlePayload {
	var p candlePayload
	p.IsComplete = true
	p.Candle.Exchange = exchange
	p.Candle.ProductID = product
	p.Candle.Timestamp = ts
	p.Candle.Open = close
	p.Candle.High = close
	p.Candle.Low = close
	p.Candle.Close = close
	p.Candle.Volume = 1
	return p
}

// TestFeedCandle_RejectsForeignExchange reproduces the trade-325 bug where a
// stray coinbase candle was fed into a scorer that should have been isolated
// to binance, causing an exit trade to be recorded at a foreign-exchange
// close price (~$84.68 when the real binance market was ~$82.27).
func TestFeedCandle_RejectsForeignExchange(t *testing.T) {
	cm, _ := newBinanceState(t, "acc:SOL-USD")

	ts := time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)

	// Coinbase candle with stale close from the previous day — this is the
	// pathological value that leaked into trade 325.
	stray := makeCandle("coinbase", "SOL-USD", ts, 84.68)

	if _, ok := cm.feedCandle("acc:SOL-USD", stray); ok {
		t.Fatal("cross-exchange candle must not be fed to scorer")
	}

	cs := cm.getScorer("acc:SOL-USD")
	if cs.scorer.CandleCount() != 0 {
		t.Fatalf("scorer should have 0 candles after rejected message; got %d", cs.scorer.CandleCount())
	}
	if !cs.lastCandleTime.IsZero() {
		t.Fatalf("lastCandleTime should remain zero after rejection; got %v", cs.lastCandleTime)
	}
}

// TestFeedCandle_RejectsStaleBackfill asserts that a re-published historical
// candle (same exchange but old timestamp) is rejected. This is the second
// path to the trade-325 bug: an ingestion restart can re-emit old complete
// candles on NATS, and before the fix the handler treated them as current.
func TestFeedCandle_RejectsStaleBackfill(t *testing.T) {
	cm, _ := newBinanceState(t, "acc:SOL-USD")

	t1 := time.Date(2026, 4, 12, 4, 0, 0, 0, time.UTC)
	t2 := t1.Add(15 * time.Minute)

	// First, a legitimate candle at t1.
	fresh := makeCandle("binance", "SOL-USD", t1, 82.33)
	if _, ok := cm.feedCandle("acc:SOL-USD", fresh); !ok {
		t.Fatal("fresh candle must be accepted")
	}

	// A backfilled coinbase-priced candle from 24h ago, re-published on binance's
	// subject, must not be able to set lastClose on the scorer.
	stale := makeCandle("binance", "SOL-USD", t1.Add(-24*time.Hour), 84.68)
	if _, ok := cm.feedCandle("acc:SOL-USD", stale); ok {
		t.Fatal("stale backfilled candle must be rejected")
	}

	// Next real candle (at t2) must still be accepted after the stale attempt.
	next := makeCandle("binance", "SOL-USD", t2, 82.15)
	if _, ok := cm.feedCandle("acc:SOL-USD", next); !ok {
		t.Fatal("next fresh candle must be accepted after a rejected stale one")
	}

	cs := cm.getScorer("acc:SOL-USD")
	if cs.scorer.CandleCount() != 2 {
		t.Fatalf("scorer should have exactly 2 candles (fresh, next); got %d", cs.scorer.CandleCount())
	}
	if !cs.lastCandleTime.Equal(t2) {
		t.Fatalf("lastCandleTime should equal t2 (%v); got %v", t2, cs.lastCandleTime)
	}
}

// TestFeedCandle_MultipleExchangesDontDoubleFeed asserts that when all four
// exchanges publish the same 15M window, only the first matching-exchange
// message is fed in. Before the fix the scorer was fed 4x per window with
// slightly different closes, corrupting indicator state.
func TestFeedCandle_MultipleExchangesDontDoubleFeed(t *testing.T) {
	cm, _ := newBinanceState(t, "acc:SOL-USD")

	ts := time.Date(2026, 4, 12, 5, 0, 0, 0, time.UTC)

	// All four exchanges publish their version of the same 15M window.
	payloads := []candlePayload{
		makeCandle("binance", "SOL-USD", ts, 82.15),
		makeCandle("coinbase", "SOL-USD", ts, 82.18),
		makeCandle("kraken", "SOL-USD", ts, 82.19),
		makeCandle("okx", "SOL-USD", ts, 82.15),
	}

	accepted := 0
	for _, p := range payloads {
		if _, ok := cm.feedCandle("acc:SOL-USD", p); ok {
			accepted++
		}
	}

	if accepted != 1 {
		t.Fatalf("expected exactly 1 accepted candle (binance), got %d", accepted)
	}
	cs := cm.getScorer("acc:SOL-USD")
	if cs.scorer.CandleCount() != 1 {
		t.Fatalf("scorer should have exactly 1 candle after 4 exchanges publish; got %d", cs.scorer.CandleCount())
	}
}

// TestFeedCandle_WarmupCountsDistinctWindowsNotMessages asserts that the
// scorer becomes "warm" after 26 distinct 15M windows — not after 26 cross-
// exchange messages covering far fewer real windows. Before the fix, all four
// exchanges' messages per window counted toward warm-up, so the scorer flipped
// to warm after ~6–7 real bars and started scoring on half-formed indicators.
func TestFeedCandle_WarmupCountsDistinctWindowsNotMessages(t *testing.T) {
	cm, _ := newBinanceState(t, "acc:SOL-USD")

	start := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	exchanges := []string{"binance", "coinbase", "kraken", "okx"}

	// Simulate 25 real 15M windows, each published by all four exchanges
	// (100 messages total). Only binance should be accepted → 25 accepted.
	for i := 0; i < 25; i++ {
		ts := start.Add(time.Duration(i) * 15 * time.Minute)
		for _, ex := range exchanges {
			cm.feedCandle("acc:SOL-USD", makeCandle(ex, "SOL-USD", ts, 100.0))
		}
	}

	cs := cm.getScorer("acc:SOL-USD")
	if cs.scorer.CandleCount() != 25 {
		t.Fatalf("expected 25 accepted candles after 25 windows × 4 exchanges; got %d", cs.scorer.CandleCount())
	}
	if cs.scorer.IsWarm() {
		t.Fatal("scorer must NOT be warm after only 25 distinct windows (cross-exchange messages must not inflate warm-up)")
	}

	// The 26th distinct window flips the scorer to warm.
	ts26 := start.Add(25 * 15 * time.Minute)
	for _, ex := range exchanges {
		cm.feedCandle("acc:SOL-USD", makeCandle(ex, "SOL-USD", ts26, 100.0))
	}

	if cs.scorer.CandleCount() != 26 {
		t.Fatalf("expected 26 accepted candles after 26 windows; got %d", cs.scorer.CandleCount())
	}
	if !cs.scorer.IsWarm() {
		t.Fatal("scorer must be warm after 26 distinct windows")
	}
}

// generatedWarmupCandles returns n candles with a gentle uptrend so ATR is
// nonzero after replay. Using trivially-equal candles would leave ATR at 0.
func generatedWarmupCandles(n int) []risk.OHLCV {
	out := make([]risk.OHLCV, n)
	price := 100.0
	for i := range out {
		spread := price * 0.005
		out[i] = risk.OHLCV{
			Open:   price,
			High:   price + spread,
			Low:    price - spread,
			Close:  price + 0.1,
			Volume: 100,
		}
		price = out[i].Close
	}
	return out
}

// newConvictionTestEngine builds a minimal Engine wired for conviction tests:
// conviction manager enabled, one product in the allowlist pinned to binance,
// and an injectable fetchWarmupCandlesFn.
func newConvictionTestEngine(fetcher func(ctx context.Context, cfg *config.Config, exchange, product string, limit int) ([]risk.OHLCV, error)) *Engine {
	e := &Engine{
		cfg:                  &config.Config{TradingMode: "paper"},
		posState:             make(map[string]*PositionState),
		cooldown:             make(map[cooldownKey]time.Time),
		conflict:             make(map[string]string),
		lastPrice:            make(map[string]float64),
		conviction:           newConvictionManager(0.35, 0.55),
		fetchWarmupCandlesFn: fetcher,
		logger:               zerolog.Nop(),
		allowlist: signalAllowlist{
			signalKey{exchange: "binance", product: "SOL-USD", granularity: "FOUR_HOURS", strategy: "hts"}: struct{}{},
		},
	}
	return e
}

// TestOnConvictionPositionOpen_PreWarmsAndMarksEntry asserts that a runtime
// position open results in a scorer that is already warm and has a non-zero
// entryATR — so the very next 15M candle can be scored, rather than waiting
// 6.5h for 26 live bars to flow in. This is the bug the ENA-USD diagnosis
// exposed: before the fix, new positions sat cold indefinitely.
func TestOnConvictionPositionOpen_PreWarmsAndMarksEntry(t *testing.T) {
	e := newConvictionTestEngine(func(ctx context.Context, cfg *config.Config, exchange, product string, limit int) ([]risk.OHLCV, error) {
		if exchange != "binance" || product != "SOL-USD" || limit != 30 {
			t.Fatalf("unexpected fetch args: exchange=%q product=%q limit=%d", exchange, product, limit)
		}
		return generatedWarmupCandles(limit), nil
	})

	ps := &PositionState{
		AccountID:  "acc",
		Symbol:     "SOL-USD",
		Side:       "long",
		EntryPrice: 100,
		OpenedAt:   time.Now().UTC(),
	}
	e.onConvictionPositionOpen(context.Background(), ps)

	cs := e.conviction.getScorer(posKey("acc", "SOL-USD"))
	if cs == nil {
		t.Fatal("expected scorer to be created for runtime open")
	}
	if !cs.scorer.IsWarm() {
		t.Fatalf("expected scorer warm after pre-warm; count=%d", cs.scorer.CandleCount())
	}
	if cs.entryATR <= 0 {
		t.Fatalf("expected entryATR > 0 after warmup; got %v", cs.entryATR)
	}
	if cs.exchange != "binance" {
		t.Fatalf("expected exchange=binance; got %q", cs.exchange)
	}
}

// TestOnConvictionPositionOpen_FetchFailureStillSeedsScorer asserts that when
// the platform API fails, we still create a scorer (bound to the resolved
// exchange) so that the live NATS candle stream can feed it — and that
// entryATR stays 0 rather than picking up a poisoned value. This guards
// against a regression where a fetch error silently disables the scorer.
func TestOnConvictionPositionOpen_FetchFailureStillSeedsScorer(t *testing.T) {
	e := newConvictionTestEngine(func(ctx context.Context, cfg *config.Config, exchange, product string, limit int) ([]risk.OHLCV, error) {
		return nil, errors.New("boom")
	})

	ps := &PositionState{
		AccountID: "acc",
		Symbol:    "SOL-USD",
		Side:      "long",
		OpenedAt:  time.Now().UTC(),
	}
	e.onConvictionPositionOpen(context.Background(), ps)

	cs := e.conviction.getScorer(posKey("acc", "SOL-USD"))
	if cs == nil {
		t.Fatal("scorer must still be created on fetch failure")
	}
	if cs.exchange != "binance" {
		t.Fatalf("scorer must be bound to resolved exchange; got %q", cs.exchange)
	}
	if cs.scorer.IsWarm() {
		t.Fatal("scorer must be cold after fetch failure")
	}
	if cs.entryATR != 0 {
		t.Fatalf("entryATR must be 0 when pre-warm fails; got %v", cs.entryATR)
	}
}

// TestOnConvictionPositionOpen_ExchangeUnknown asserts that when a position's
// symbol has no allowlist entry, no scorer is created (rather than creating
// one with empty exchange that would then accept candles from any exchange).
func TestOnConvictionPositionOpen_ExchangeUnknown(t *testing.T) {
	e := newConvictionTestEngine(func(ctx context.Context, cfg *config.Config, exchange, product string, limit int) ([]risk.OHLCV, error) {
		t.Fatal("fetcher must not be called when exchange is unknown")
		return nil, nil
	})

	ps := &PositionState{AccountID: "acc", Symbol: "UNKNOWN-USD", Side: "long"}
	e.onConvictionPositionOpen(context.Background(), ps)

	if cs := e.conviction.getScorer(posKey("acc", "UNKNOWN-USD")); cs != nil {
		t.Fatalf("no scorer should be created when exchange unknown; got %+v", cs)
	}
}

// TestCreateScorer_BindsExchange asserts that createScorer records the
// exchange on the state so later candle filtering can use it.
func TestCreateScorer_BindsExchange(t *testing.T) {
	cm := newConvictionManager(0.35, 0.55)
	cs := cm.createScorer("acc:SOL-USD", "binance")
	if cs.exchange != "binance" {
		t.Fatalf("expected exchange=binance, got %q", cs.exchange)
	}
	// Round-trip via getScorer too.
	got := cm.getScorer("acc:SOL-USD")
	if got == nil || got.exchange != "binance" {
		t.Fatalf("getScorer should return scorer with exchange=binance; got %v", got)
	}
}
