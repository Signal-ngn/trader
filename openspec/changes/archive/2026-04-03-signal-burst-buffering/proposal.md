## Why

The ingestion server evaluates all products simultaneously at each 4-hour candle boundary and publishes trading signals in a burst — typically a mix of SELL/COVER (close existing positions) and BUY/SHORT (open new positions). NATS does not guarantee delivery order across different subjects, so the engine may receive BUY/SHORT signals before SELL/COVER signals. When this happens, the balance check rejects the open signals because capital is still locked in positions that are about to be closed moments later. This causes missed trades that would have been valid.

## What Changes

- Add a per-account signal buffer in the engine that collects incoming signals and flushes them after a 5-second quiet period (no new signals received for that account).
- On flush, process close signals (SELL/COVER) before open signals (BUY/SHORT) so that freed capital is available for subsequent opens.
- Within the open signals, process highest-confidence signals first so that when capital is limited, the best opportunities are filled.
- Add tests verifying burst buffering, close-before-open ordering, and confidence-based priority.

## Capabilities

### New Capabilities
- `signal-burst-buffer`: Per-account signal buffering with timer-based flush, close-before-open ordering, and confidence-priority for opens.

### Modified Capabilities
_(none — this is a new internal engine layer between signal reception and processing; no existing spec-level behaviour changes)_

## Impact

- **Code**: `internal/engine/` — new buffer component inserted between `handleSignal` and `processSignal`. The existing `processSignal` logic is unchanged.
- **Latency**: Signals are delayed up to 5 seconds (the quiet-period window) before processing. This is acceptable for 4-hour candle strategies.
- **Risk**: Non-burst signals (isolated signals outside of a burst) are also delayed by up to 5 seconds. This is a minor tradeoff for correctness.
