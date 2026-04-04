package engine

import (
	"context"
	"sort"
	"sync"
	"time"
)

// bufferedSignal holds a pre-validated signal and its routing metadata,
// ready to be processed when the per-account buffer flushes.
type bufferedSignal struct {
	signal    SignalPayload
	product   string
	strategy  string
	accountID string
	seq       int // arrival order within the buffer (for stable sort)
}

// accountBuffer holds the pending signals and flush timer for a single account.
type accountBuffer struct {
	signals []bufferedSignal
	timer   *time.Timer
}

// signalBuffer collects incoming signals per account and flushes them after a
// quiet period with no new signals. On flush, signals are sorted so that close
// actions (SELL/COVER) are processed before open actions (BUY/SHORT), and opens
// are ordered by confidence descending.
type signalBuffer struct {
	mu      sync.Mutex
	buffers map[string]*accountBuffer // keyed by accountID
	flush   func(ctx context.Context, signals []bufferedSignal)
	timeout time.Duration
	ctx     context.Context
	seq     int // global sequence counter for stable sort
}

// newSignalBuffer creates a signal buffer. The flush callback is invoked with
// the sorted signals for a single account when its quiet-period timer fires.
func newSignalBuffer(ctx context.Context, timeout time.Duration, flush func(ctx context.Context, signals []bufferedSignal)) *signalBuffer {
	return &signalBuffer{
		buffers: make(map[string]*accountBuffer),
		flush:   flush,
		timeout: timeout,
		ctx:     ctx,
	}
}

// Add appends a signal to the per-account buffer and resets the flush timer.
func (sb *signalBuffer) Add(sig bufferedSignal) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	sb.seq++
	sig.seq = sb.seq

	ab, exists := sb.buffers[sig.accountID]
	if !exists {
		ab = &accountBuffer{}
		sb.buffers[sig.accountID] = ab
	}

	ab.signals = append(ab.signals, sig)

	// Reset (or start) the sliding-window timer.
	if ab.timer != nil {
		ab.timer.Stop()
	}
	accountID := sig.accountID
	ab.timer = time.AfterFunc(sb.timeout, func() {
		sb.flushAccount(accountID)
	})
}

// flushAccount sorts and flushes all buffered signals for a single account.
func (sb *signalBuffer) flushAccount(accountID string) {
	sb.mu.Lock()
	ab, exists := sb.buffers[accountID]
	if !exists || len(ab.signals) == 0 {
		sb.mu.Unlock()
		return
	}
	signals := ab.signals
	ab.signals = nil
	if ab.timer != nil {
		ab.timer.Stop()
		ab.timer = nil
	}
	delete(sb.buffers, accountID)
	sb.mu.Unlock()

	// Sort: closes first (arrival order), then opens (confidence desc, stable).
	sortSignals(signals)

	sb.flush(sb.ctx, signals)
}

// Stop stops all timers and flushes any remaining buffered signals. Called on
// engine shutdown.
func (sb *signalBuffer) Stop() {
	sb.mu.Lock()
	accounts := make([]string, 0, len(sb.buffers))
	for accountID, ab := range sb.buffers {
		if ab.timer != nil {
			ab.timer.Stop()
		}
		accounts = append(accounts, accountID)
	}
	sb.mu.Unlock()

	for _, accountID := range accounts {
		sb.flushAccount(accountID)
	}
}

// isCloseAction returns true for SELL and COVER actions.
func isCloseAction(action string) bool {
	return action == "SELL" || action == "COVER"
}

// sortSignals sorts signals so closes come first (in arrival order), then opens
// ordered by confidence descending. Uses stable sort so equal-confidence opens
// preserve arrival order.
func sortSignals(signals []bufferedSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		iClose := isCloseAction(signals[i].signal.Action)
		jClose := isCloseAction(signals[j].signal.Action)

		if iClose != jClose {
			return iClose // closes before opens
		}
		if iClose {
			// Both closes: preserve arrival order.
			return signals[i].seq < signals[j].seq
		}
		// Both opens: higher confidence first.
		if signals[i].signal.Confidence != signals[j].signal.Confidence {
			return signals[i].signal.Confidence > signals[j].signal.Confidence
		}
		// Equal confidence: preserve arrival order.
		return signals[i].seq < signals[j].seq
	})
}
