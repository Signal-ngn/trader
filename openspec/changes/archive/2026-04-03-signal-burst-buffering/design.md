## Context

The trading engine subscribes to NATS signals via a single `signals.>` subscription. The NATS Go client delivers messages sequentially to the callback, so `handleSignal` processes one message at a time. Each signal goes through allowlist filtering, staleness checks, and account routing before calling `processSignal`, which fetches trading configs and balance via HTTP, then opens or closes positions.

The current flow is: `NATS msg → handleSignal() → processSignal()` — synchronous, one signal at a time, no buffering.

When the ingestion server evaluates a 4-hour candle, it publishes signals for all products within milliseconds. NATS delivers these in non-deterministic order. If a BUY arrives before a SELL that would have freed capital, the BUY is rejected for insufficient balance.

## Goals / Non-Goals

**Goals:**
- Buffer signals per-account and flush after a 5-second quiet period
- Process SELL/COVER signals before BUY/SHORT signals within each flush
- Order BUY/SHORT signals by descending confidence so highest-quality trades get capital first
- Preserve all existing signal validation (allowlist, staleness, cooldown, confidence floor) — these checks remain in `handleSignal` before buffering
- Add unit tests for the buffer component

**Non-Goals:**
- Changing the ingestion server's signal publishing behaviour
- Reducing the 5-second latency for isolated (non-burst) signals
- Buffering across accounts (each account's buffer is independent)

## Decisions

### Decision 1: Buffer sits between handleSignal and processSignal, with batched balance management

The buffer intercepts after `handleSignal` has done all its pre-filtering (allowlist, staleness, confidence floor, account routing) but before `processSignal` is called. This means:

- `handleSignal` continues to validate and drop invalid signals immediately
- Only valid, routed signals enter the buffer
- On flush, balance is read once and written once for the entire batch

**Rationale**: Minimises changes to existing code and eliminates redundant HTTP calls. Currently each open signal triggers 2 balance reads (`GetAccountBalance` in `handleOpenSignal` + `checkBalance`) and 1 balance write (`AdjustBalance` inside `InsertTradeAndUpdatePosition`), and each close also triggers a balance write. With N closes and M opens, that's N + 2M reads and N + M writes. Batching reduces this to 1 read and 1 write total.

```
  CURRENT (per signal, no buffering):
  ══════════════════════════════════════════════════════════
  NATS msg → handleSignal() → [per account] → processSignal()
                                                    │
                                              handleOpenSignal()
                                                    │
                                              GetBalance (HTTP)
                                              calculatePositionSize()
                                              checkBalance (HTTP)    ← 2nd read!
                                              InsertTrade (HTTP)
                                              AdjustBalance (HTTP)   ← per-trade write

  NEW (buffered flush):
  ══════════════════════════════════════════════════════════
  NATS msg → handleSignal() → [per account] → buffer.Add(signal)
                                                     │
                                              5s quiet period
                                                     │
                                                     ▼
                                              buffer.Flush()
                                                     │
                              ┌──────────────────────┴──────────────────────┐
                              ▼                                             ▼
                        SELL/COVER                                     BUY/SHORT
                        (any order)                               (by confidence desc)
                              │                                             │
                              ▼                                             ▼
                     InsertTrade (HTTP)                          GetBalance ONCE (HTTP)
                     accumulate delta locally                           │
                              │                                         ▼
                              │                              add close deltas to balance
                              │                                         │
                              │                                         ▼
                              │                              for each open:
                              │                                size from local balance
                              │                                InsertTrade (HTTP)
                              │                                localBalance -= margin
                              │                                         │
                              └────────────────┬────────────────────────┘
                                               ▼
                                    AdjustBalance ONCE (HTTP)
                                    delta = sum(close returns) - sum(open margins)

  HTTP calls: 2 closes + 3 opens = 16 before → 7 after (56% reduction)
```

### Decision 2: New `signalBuffer` struct in `internal/engine/buffer.go`

A standalone struct with its own goroutine per account (lazily started). Interface:

```go
type bufferedSignal struct {
    signal    SignalPayload
    product   string
    strategy  string
    accountID string
}

type signalBuffer struct {
    mu       sync.Mutex
    buffers  map[string]*accountBuffer  // keyed by accountID
    flush    func(ctx context.Context, signals []bufferedSignal)
    timeout  time.Duration
}

type accountBuffer struct {
    signals []bufferedSignal
    timer   *time.Timer
}
```

- `Add(signal)` appends to the account's buffer and resets the 5-second timer
- When the timer fires, all buffered signals for that account are sorted (closes first, then opens by confidence desc) and flushed via the callback
- The flush callback orchestrates the batched balance flow (see Decision 6)

**Alternative considered**: A single global buffer with periodic flush. Rejected because it couples unrelated accounts — a slow close on account A would delay opens on account B.

### Decision 3: Timer reset on each signal (sliding window)

Each new signal for an account resets the 5-second timer. This means the quiet period is measured from the *last* signal, not the first. A burst of 10 signals arriving over 2 seconds will flush 5 seconds after the 10th signal.

**Rationale**: The ingestion server publishes all signals within milliseconds, so the timer reset adds negligible delay. A fixed window from the first signal would risk flushing before the burst is complete if there's slight latency variation.

**Safeguard**: No maximum buffer duration cap is needed because the existing 10-minute signal staleness check in `handleSignal` naturally bounds how long signals can accumulate. If signals somehow trickle in for minutes, the stale ones are dropped before reaching the buffer.

### Decision 4: Flush ordering — closes first, then opens by confidence

Within a flush:
1. All SELL/COVER signals are processed first (in arrival order — no particular priority among closes)
2. All BUY/SHORT signals are processed second, sorted by `signal.Confidence` descending

**Rationale**: Closes free capital. Processing them first maximises the balance available for opens. Among opens, higher-confidence signals represent better opportunities and should get capital priority when it's scarce.

### Decision 5: The buffer is testable in isolation

`signalBuffer` takes a `flush` callback function, making it easy to test without the full engine. Tests can:
- Verify timer-based flushing
- Verify close-before-open ordering
- Verify confidence sorting
- Verify per-account isolation
- Verify batched balance read/write behaviour

### Decision 6: Batched balance management during flush

The platform API's `POST /trades` does not update the account balance — only the explicit `PATCH /accounts/{id}/balance` (`AdjustBalanceDelta`) does. This means balance management is fully client-controlled, which enables batching.

The flush callback implements a 4-phase flow:

1. **Phase 1 — Process closes**: Call `InsertTradeAndUpdatePosition` for each SELL/COVER signal, but **skip the per-trade `AdjustBalance` call**. Instead, accumulate the balance delta locally (margin returned + realised P&L for each close).

2. **Phase 2 — Read balance once**: Call `GetAccountBalance` a single time. Add the accumulated close deltas to get the effective available balance.

3. **Phase 3 — Process opens**: For each BUY/SHORT signal (sorted by confidence desc), calculate position size against the local balance, call `InsertTradeAndUpdatePosition` (again skipping per-trade `AdjustBalance`), and deduct the margin from the local balance. If local balance is insufficient, skip that signal.

4. **Phase 4 — Write balance once**: Call `AdjustBalance` with the net delta (sum of close returns minus sum of open margins).

To skip the per-trade `AdjustBalance` inside `InsertTradeAndUpdatePosition`, we add a `skipBalanceAdjust` flag to the trade submission or introduce a separate `InsertTradeOnly` method on `EngineStore` that submits the trade without the balance side-effect. The latter is cleaner — it keeps `InsertTradeAndUpdatePosition` unchanged for the non-buffered path (risk loop closes).

**Why not remove AdjustBalance from InsertTradeAndUpdatePosition entirely?** The risk loop's `executeCloseTrade` also calls `InsertTradeAndUpdatePosition` for stop-loss and trailing-stop exits. These are single trades, not batches, and the existing per-trade balance adjustment is correct and simpler for that path. Keeping both paths avoids a risky refactor of the risk loop.

## Risks / Trade-offs

- **[5-second latency on all signals]** → Acceptable for 4-hour candle strategies. Not suitable if sub-second signal latency becomes a requirement in the future. Could be mitigated with a configurable timeout or by detecting isolated signals vs bursts.
- **[Goroutine per active account]** → Accounts that receive even one signal get a goroutine with a timer. These are short-lived (fire after 5s and clean up). With the current scale (single-digit accounts), this is negligible.
- **[Signal staleness edge case]** → A signal at 9m50s age enters the buffer, waits 5s, and is now 9m55s old when processed. It's still within the 10-minute window so it proceeds. This is fine — the staleness check is a sanity guard, not a precision requirement.
- **[Balance drift on partial failure]** → If phase 3 (opens) partially fails (e.g., 2 of 3 trades succeed), phase 4 still writes the net delta for all attempted trades. The `InsertTradeOnly` calls that failed won't have recorded trades, so the balance write would over-deduct. Mitigation: only include successfully inserted trades in the net delta calculation.
- **[Risk loop and buffer concurrency]** → The risk loop can trigger a close via `executeCloseTrade` at any time (stop-loss, trailing-stop). This close uses the existing `InsertTradeAndUpdatePosition` with per-trade balance adjustment, which could race with a buffered flush's single balance write. Mitigation: this is the same race that exists today between the risk loop and sequential signal processing — `AdjustBalanceDelta` is atomic (server-side), so concurrent adjustments are safe. The local balance snapshot in the flush may be slightly stale, but the final `AdjustBalance` call uses a delta, not an absolute set.
