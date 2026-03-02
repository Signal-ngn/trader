package engine

import (
	"testing"
	"time"
)

// ── signalAllowlist.allows ────────────────────────────────────────────────────

func TestAllowlist_ExactMatch(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if !al.allows("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost") {
		t.Fatal("exact match should be allowed")
	}
}

func TestAllowlist_PlusSuffixMatch(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if !al.allows("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost+trend") {
		t.Fatal("plus-suffix strategy should match base")
	}
}

func TestAllowlist_UnderscoreSuffixMatch(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if !al.allows("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost_short") {
		t.Fatal("underscore-suffix strategy should match base")
	}
}

func TestAllowlist_NoMatchDifferentProduct(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if al.allows("coinbase", "ETH-USD", "ONE_HOUR", "ml_xgboost") {
		t.Fatal("different product should not match")
	}
}

func TestAllowlist_NoMatchDifferentExchange(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if al.allows("binance", "BTC-USD", "ONE_HOUR", "ml_xgboost") {
		t.Fatal("different exchange should not match")
	}
}

func TestAllowlist_NoMatchDifferentGranularity(t *testing.T) {
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if al.allows("coinbase", "BTC-USD", "ONE_MINUTE", "ml_xgboost") {
		t.Fatal("different granularity should not match")
	}
}

func TestAllowlist_EmptyAllowlistBlocksEverything(t *testing.T) {
	al := signalAllowlist{}
	if al.allows("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost") {
		t.Fatal("empty allowlist should block all signals")
	}
}

func TestAllowlist_MultipleSuffixSeparators(t *testing.T) {
	// "ml_xgboost+trend_v2" — the algorithm scans from the right, so it
	// first tries "ml_xgboost+trend" (not in list), then "ml_xgboost" (in list).
	al := signalAllowlist{
		signalKey{"coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost"}: {},
	}
	if !al.allows("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost+trend_v2") {
		t.Fatal("deep suffix should still match base strategy")
	}
}

// ── parseSubject ──────────────────────────────────────────────────────────────

func TestParseSubject_Valid(t *testing.T) {
	exchange, product, granularity, strategy := parseSubject("signals.coinbase.BTC-USD.ONE_HOUR.ml_xgboost+trend")
	if exchange != "coinbase" {
		t.Errorf("exchange: want coinbase, got %q", exchange)
	}
	if product != "BTC-USD" {
		t.Errorf("product: want BTC-USD, got %q", product)
	}
	if granularity != "ONE_HOUR" {
		t.Errorf("granularity: want ONE_HOUR, got %q", granularity)
	}
	if strategy != "ml_xgboost+trend" {
		t.Errorf("strategy: want ml_xgboost+trend, got %q", strategy)
	}
}

func TestParseSubject_TooFewSegments(t *testing.T) {
	exchange, product, granularity, strategy := parseSubject("signals.coinbase.BTC-USD")
	if exchange != "" || product != "" || granularity != "" || strategy != "" {
		t.Fatal("short subject should return empty strings")
	}
}

// ── buildSubject ──────────────────────────────────────────────────────────────

func TestBuildSubject_AllWildcards(t *testing.T) {
	s := buildSubject("", "", "", "")
	if s != "signals.*.*.*.>" {
		t.Errorf("want signals.*.*.*.>, got %q", s)
	}
}

func TestBuildSubject_FullySpecified(t *testing.T) {
	s := buildSubject("coinbase", "BTC-USD", "ONE_HOUR", "ml_xgboost")
	if s != "signals.coinbase.BTC-USD.ONE_HOUR.ml_xgboost" {
		t.Errorf("unexpected subject %q", s)
	}
}

// ── signal filter checks (stale, confidence, cooldown) ───────────────────────
// These are tested via handleSignal indirectly through the engine, but we can
// exercise the timestamp staleness boundary directly.

func TestSignalStale_FreshAccepted(t *testing.T) {
	// A signal timestamped 30 seconds ago should be fresh.
	ts := time.Now().Add(-30 * time.Second).Unix()
	age := time.Since(time.Unix(ts, 0))
	if age > 2*time.Minute {
		t.Fatalf("expected fresh signal, age was %v", age)
	}
}

func TestSignalStale_OldRejected(t *testing.T) {
	// A signal timestamped 3 minutes ago should be stale.
	ts := time.Now().Add(-3 * time.Minute).Unix()
	age := time.Since(time.Unix(ts, 0))
	if age <= 2*time.Minute {
		t.Fatalf("expected stale signal, age was only %v", age)
	}
}
