## 1. Signal buffer core

- [x] 1.1 Create `internal/engine/buffer.go` with `bufferedSignal`, `accountBuffer`, and `signalBuffer` structs as defined in design Decision 2
- [x] 1.2 Implement `signalBuffer.Add()` — appends signal to per-account buffer, lazily creates `accountBuffer`, resets 5-second sliding-window timer
- [x] 1.3 Implement `accountBuffer` flush — on timer fire, sort signals (closes first in arrival order, then opens by confidence desc using stable sort), invoke flush callback, clean up buffer entry
- [x] 1.4 Implement `signalBuffer.Stop()` — stops all timers and flushes remaining signals on engine shutdown (context cancellation)

## 2. Signal buffer tests

- [x] 2.1 Test: single signal flushes after 5-second quiet period
- [x] 2.2 Test: timer resets on each new signal (sliding window)
- [x] 2.3 Test: burst of signals collects into single flush
- [x] 2.4 Test: close signals (SELL/COVER) ordered before open signals (BUY/SHORT) on flush
- [x] 2.5 Test: open signals sorted by confidence descending (stable sort — equal confidence preserves arrival order)
- [x] 2.6 Test: signals for different accounts buffered and flushed independently
- [x] 2.7 Test: Stop() flushes remaining buffered signals

## 3. Trade insertion without balance side-effect

- [x] 3.1 Add `InsertTradeOnly(ctx, tenantID, trade)` method to `EngineStore` interface — submits trade and increments daily P&L but skips `AdjustBalance`
- [x] 3.2 Implement `InsertTradeOnly` on `APIEngineStore` — extract shared trade submission logic from `InsertTradeAndUpdatePosition`, call without balance adjustment
- [x] 3.3 Test: `InsertTradeOnly` submits trade successfully without calling `AdjustBalance`
- [x] 3.4 Test: `InsertTradeAndUpdatePosition` still calls `AdjustBalance` (regression)

## 4. Batched flush handler

- [x] 4.1 Create `engine.flushAccountSignals(ctx, signals []bufferedSignal)` — the flush callback wired into the signal buffer
- [x] 4.2 Implement Phase 1 (close processing): for each SELL/COVER signal, call `processSignal` variant that uses `InsertTradeOnly`, accumulate balance delta from `costDeltaForTrade`
- [x] 4.3 Implement Phase 2 (balance read): call `GetAccountBalance` once, add accumulated close deltas to get effective available balance
- [x] 4.4 Implement Phase 3 (open processing): for each BUY/SHORT signal (already sorted by confidence), calculate position size against local balance, call `processSignal` variant using `InsertTradeOnly`, deduct margin from local balance, skip signal if insufficient balance
- [x] 4.5 Implement Phase 4 (balance write): call `AdjustBalance` once with net delta (sum of close returns minus sum of successful open margins)
- [x] 4.6 Handle edge case: empty flush (no signals) — skip balance read/write entirely
- [x] 4.7 Handle edge case: only closes or only opens in flush — skip inapplicable phases

## 5. Batched flush handler tests

- [x] 5.1 Test: closes processed before opens, balance read once between phases, balance written once at end
- [x] 5.2 Test: close deltas reflected in effective available balance for opens
- [x] 5.3 Test: limited capital fills highest-confidence opens first, skips lowest
- [x] 5.4 Test: partial open failure — net delta only includes successful trades
- [x] 5.5 Test: opens still evaluated after a skip (smaller position may fit)
- [x] 5.6 Test: flush with only closes — no balance read needed for opens, single balance write
- [x] 5.7 Test: flush with only opens — no close delta, balance read + opens + write
- [x] 5.8 Test: empty flush — no balance read or write

## 6. Engine integration

- [x] 6.1 Add `signalBuffer` field to `Engine` struct, initialise in `New()` with 5-second timeout and `flushAccountSignals` as flush callback
- [x] 6.2 Wire `handleSignal` to call `buffer.Add()` instead of `processSignal()` for each target account
- [x] 6.3 Call `buffer.Stop()` on engine shutdown (context cancellation in `Start`)
- [x] 6.4 Verify existing pre-filtering (allowlist, staleness, confidence floor, cooldown, account routing) still runs in `handleSignal` before buffering — no changes to filtering logic

## 7. Integration tests

- [x] 7.1 Test: end-to-end burst scenario — mixed SELL/COVER and BUY/SHORT signals arrive in arbitrary order, all close positions freed before opens attempted, opens filled by confidence priority
- [x] 7.2 Test: risk loop close during buffered flush — `executeCloseTrade` uses `InsertTradeAndUpdatePosition` (with balance adjust) concurrently with flush's batched balance — verify no balance corruption (atomic deltas)
