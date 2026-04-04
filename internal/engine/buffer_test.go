package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

// collectFlush returns a flush callback and a function to retrieve the flushed signals.
func collectFlush() (func(ctx context.Context, signals []bufferedSignal), func() [][]bufferedSignal) {
	var mu sync.Mutex
	var batches [][]bufferedSignal

	flush := func(ctx context.Context, signals []bufferedSignal) {
		cp := make([]bufferedSignal, len(signals))
		copy(cp, signals)
		mu.Lock()
		batches = append(batches, cp)
		mu.Unlock()
	}

	get := func() [][]bufferedSignal {
		mu.Lock()
		defer mu.Unlock()
		return batches
	}

	return flush, get
}

func makeSig(action, product, accountID string, confidence float64) bufferedSignal {
	return bufferedSignal{
		signal: SignalPayload{
			Action:     action,
			Product:    product,
			Confidence: confidence,
		},
		product:   product,
		strategy:  "test_strategy",
		accountID: accountID,
	}
}

func TestBuffer_SingleSignalFlushesAfterQuietPeriod(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 100*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.9))

	// Should NOT have flushed yet.
	time.Sleep(50 * time.Millisecond)
	if len(get()) != 0 {
		t.Fatal("flushed too early")
	}

	// Wait for quiet period to expire.
	time.Sleep(100 * time.Millisecond)

	batches := get()
	if len(batches) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(batches))
	}
	if len(batches[0]) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(batches[0]))
	}
	if batches[0][0].signal.Action != "BUY" {
		t.Fatalf("expected BUY, got %s", batches[0][0].signal.Action)
	}
}

func TestBuffer_TimerResetsOnNewSignal(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 150*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.9))
	time.Sleep(100 * time.Millisecond)

	// Add another signal — should reset the timer.
	buf.Add(makeSig("SELL", "ETHUSDT", "acct-1", 0.8))
	time.Sleep(100 * time.Millisecond)

	// Timer was reset, should NOT have flushed yet.
	if len(get()) != 0 {
		t.Fatal("flushed too early — timer should have been reset")
	}

	// Wait for the reset timer to fire.
	time.Sleep(100 * time.Millisecond)

	batches := get()
	if len(batches) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("expected 2 signals in single flush, got %d", len(batches[0]))
	}
}

func TestBuffer_BurstCollectsIntoSingleFlush(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 100*time.Millisecond, flush)

	// Simulate burst: 6 signals within a few ms.
	for i := 0; i < 6; i++ {
		buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.5+float64(i)*0.05))
	}

	time.Sleep(200 * time.Millisecond)

	batches := get()
	if len(batches) != 1 {
		t.Fatalf("expected 1 flush for burst, got %d", len(batches))
	}
	if len(batches[0]) != 6 {
		t.Fatalf("expected 6 signals in flush, got %d", len(batches[0]))
	}
}

func TestBuffer_CloseBeforeOpenOrdering(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 50*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "SOLUSDT", "acct-1", 0.9))
	buf.Add(makeSig("SELL", "BTCUSDT", "acct-1", 0.8))
	buf.Add(makeSig("SHORT", "AVAXUSDT", "acct-1", 0.7))
	buf.Add(makeSig("COVER", "ETHUSDT", "acct-1", 0.6))

	time.Sleep(150 * time.Millisecond)

	batches := get()
	if len(batches) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(batches))
	}
	signals := batches[0]
	if len(signals) != 4 {
		t.Fatalf("expected 4 signals, got %d", len(signals))
	}
	// First two should be closes (SELL, COVER) in arrival order.
	if signals[0].signal.Action != "SELL" {
		t.Errorf("position 0: expected SELL, got %s", signals[0].signal.Action)
	}
	if signals[1].signal.Action != "COVER" {
		t.Errorf("position 1: expected COVER, got %s", signals[1].signal.Action)
	}
	// Last two should be opens (BUY, SHORT) — BUY has higher confidence (0.9 > 0.7).
	if signals[2].signal.Action != "BUY" {
		t.Errorf("position 2: expected BUY, got %s", signals[2].signal.Action)
	}
	if signals[3].signal.Action != "SHORT" {
		t.Errorf("position 3: expected SHORT, got %s", signals[3].signal.Action)
	}
}

func TestBuffer_OpensSortedByConfidenceDesc(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 50*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "SOLUSDT", "acct-1", 0.78))
	buf.Add(makeSig("SHORT", "BTCUSDT", "acct-1", 0.92))
	buf.Add(makeSig("BUY", "AVAXUSDT", "acct-1", 0.85))

	time.Sleep(150 * time.Millisecond)

	signals := get()[0]
	if signals[0].signal.Confidence != 0.92 {
		t.Errorf("position 0: expected confidence 0.92, got %f", signals[0].signal.Confidence)
	}
	if signals[1].signal.Confidence != 0.85 {
		t.Errorf("position 1: expected confidence 0.85, got %f", signals[1].signal.Confidence)
	}
	if signals[2].signal.Confidence != 0.78 {
		t.Errorf("position 2: expected confidence 0.78, got %f", signals[2].signal.Confidence)
	}
}

func TestBuffer_EqualConfidencePreservesArrivalOrder(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 50*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "SOLUSDT", "acct-1", 0.85))
	buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.85))
	buf.Add(makeSig("BUY", "ETHUSDT", "acct-1", 0.85))

	time.Sleep(150 * time.Millisecond)

	signals := get()[0]
	if signals[0].product != "SOLUSDT" {
		t.Errorf("position 0: expected SOLUSDT, got %s", signals[0].product)
	}
	if signals[1].product != "BTCUSDT" {
		t.Errorf("position 1: expected BTCUSDT, got %s", signals[1].product)
	}
	if signals[2].product != "ETHUSDT" {
		t.Errorf("position 2: expected ETHUSDT, got %s", signals[2].product)
	}
}

func TestBuffer_DifferentAccountsIndependent(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 100*time.Millisecond, flush)

	buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.9))
	buf.Add(makeSig("SELL", "ETHUSDT", "acct-2", 0.8))

	// After acct-1's timer fires but before adding more to acct-2.
	time.Sleep(200 * time.Millisecond)

	batches := get()
	if len(batches) != 2 {
		t.Fatalf("expected 2 independent flushes, got %d", len(batches))
	}
	// Each flush should have exactly 1 signal.
	for i, batch := range batches {
		if len(batch) != 1 {
			t.Errorf("batch %d: expected 1 signal, got %d", i, len(batch))
		}
	}
	// Verify different accounts.
	accounts := map[string]bool{}
	for _, batch := range batches {
		accounts[batch[0].accountID] = true
	}
	if !accounts["acct-1"] || !accounts["acct-2"] {
		t.Error("expected both acct-1 and acct-2 in separate flushes")
	}
}

func TestBuffer_StopFlushesRemaining(t *testing.T) {
	flush, get := collectFlush()
	ctx := context.Background()
	buf := newSignalBuffer(ctx, 10*time.Second, flush) // long timeout — won't fire naturally

	buf.Add(makeSig("BUY", "BTCUSDT", "acct-1", 0.9))
	buf.Add(makeSig("SELL", "ETHUSDT", "acct-2", 0.8))

	// Nothing should have flushed.
	if len(get()) != 0 {
		t.Fatal("should not have flushed before Stop()")
	}

	buf.Stop()

	batches := get()
	if len(batches) != 2 {
		t.Fatalf("expected 2 flushes after Stop(), got %d", len(batches))
	}
}
