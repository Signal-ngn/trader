## Requirements

### Requirement: per-account signal buffering
The engine SHALL buffer incoming signals per account rather than processing them immediately. Each signal that passes pre-filtering (allowlist, staleness, confidence floor, account routing) SHALL be appended to a per-account buffer instead of being passed directly to `processSignal`.

#### Scenario: Signal enters buffer instead of immediate processing
- **WHEN** a valid signal for account "acct-1" arrives from NATS
- **THEN** the engine SHALL append it to account "acct-1"'s buffer and SHALL NOT call `processSignal` immediately

#### Scenario: Signals for different accounts are buffered independently
- **WHEN** signals arrive for account "acct-1" and account "acct-2"
- **THEN** each account SHALL have its own independent buffer and flush timer

---

### Requirement: timer-based flush with sliding window
Each account buffer SHALL use a 5-second sliding-window timer. The timer SHALL reset every time a new signal is added to that account's buffer. When 5 seconds elapse with no new signals for an account, the buffer SHALL flush all accumulated signals for that account.

#### Scenario: Single signal flushes after quiet period
- **WHEN** a single signal arrives for account "acct-1" and no further signals arrive for 5 seconds
- **THEN** the buffer SHALL flush that signal for processing after the 5-second quiet period

#### Scenario: Timer resets on each new signal
- **WHEN** a signal arrives for account "acct-1" at t=0, another at t=3s, and another at t=4s
- **THEN** the buffer SHALL flush all three signals at t=9s (5 seconds after the last signal)

#### Scenario: Burst of signals within the window
- **WHEN** 6 signals arrive for account "acct-1" within 500ms and no further signals arrive
- **THEN** the buffer SHALL flush all 6 signals approximately 5 seconds after the last signal

---

### Requirement: close-before-open ordering on flush
When flushing a buffer, the engine SHALL process all close signals (SELL, COVER) before any open signals (BUY, SHORT). Close signals SHALL be processed in arrival order.

#### Scenario: Mixed close and open signals in burst
- **WHEN** a burst contains [BUY SOLUSDT, SELL BTCUSDT, SHORT AVAXUSDT, COVER ETHUSDT]
- **THEN** the flush SHALL process SELL BTCUSDT and COVER ETHUSDT first, then BUY SOLUSDT and SHORT AVAXUSDT

#### Scenario: Only close signals in burst
- **WHEN** a burst contains only SELL and COVER signals
- **THEN** the flush SHALL process all close signals in arrival order with no open phase

#### Scenario: Only open signals in burst
- **WHEN** a burst contains only BUY and SHORT signals
- **THEN** the flush SHALL skip the close phase and process opens directly

---

### Requirement: confidence-based priority for open signals
Within a flush, open signals (BUY, SHORT) SHALL be sorted by confidence descending before processing. When capital is limited, higher-confidence signals SHALL be filled first.

#### Scenario: Opens ordered by confidence
- **WHEN** a flush contains BUY SOLUSDT (confidence 0.85), SHORT BTCUSDT (confidence 0.92), BUY AVAXUSDT (confidence 0.78)
- **THEN** the flush SHALL process them in order: SHORT BTCUSDT (0.92), BUY SOLUSDT (0.85), BUY AVAXUSDT (0.78)

#### Scenario: Limited capital fills highest confidence first
- **WHEN** a flush contains 3 open signals requiring $300 each but only $700 is available
- **THEN** the two highest-confidence signals SHALL be filled and the lowest-confidence signal SHALL be skipped due to insufficient balance

#### Scenario: Equal confidence preserves arrival order
- **WHEN** two open signals have identical confidence values
- **THEN** they SHALL be processed in arrival order (stable sort)

---

### Requirement: batched balance read
During a flush, the engine SHALL read the account balance exactly once, after processing all close signals. The balance read SHALL occur before processing any open signals. The effective available balance SHALL be computed as the fetched balance plus the accumulated balance deltas from the close phase.

#### Scenario: Balance read once for multiple opens
- **WHEN** a flush contains 2 close signals and 3 open signals
- **THEN** `GetAccountBalance` SHALL be called exactly once, after the 2 closes complete and before the first open is processed

#### Scenario: Close deltas reflected in available balance
- **WHEN** a close returns $500 margin + $100 P&L, and the fetched balance is $200
- **THEN** the effective available balance for the open phase SHALL be $800 ($200 + $600)

---

### Requirement: batched balance write
During a flush, the engine SHALL write the account balance exactly once, after all signals (closes and opens) have been processed. The balance write SHALL use `AdjustBalance` with the net delta: sum of close returns minus sum of open margins.

#### Scenario: Single balance write for entire flush
- **WHEN** a flush processes 2 closes (returning $500 and $300) and 2 opens (costing $200 and $400)
- **THEN** `AdjustBalance` SHALL be called exactly once with delta = +$800 - $600 = +$200

#### Scenario: No balance write when flush is empty
- **WHEN** a flush contains no signals (edge case: all signals were filtered out between buffering and flushing)
- **THEN** no balance read or write SHALL occur

#### Scenario: Partial open failure only counts successful trades
- **WHEN** a flush processes 3 open signals but the 2nd trade insertion fails
- **THEN** the net balance delta SHALL include only the close returns and the margins of the 1st and 3rd opens (the successful ones)

---

### Requirement: trade insertion without balance side-effect
The engine SHALL provide a method to insert a trade via the platform API without triggering the per-trade `AdjustBalance` call. This method SHALL be used during buffered flushes. The existing `InsertTradeAndUpdatePosition` (which includes per-trade balance adjustment) SHALL remain unchanged for use by the risk loop.

#### Scenario: Buffered flush uses trade-only insertion
- **WHEN** a buffered flush processes a close or open signal
- **THEN** the trade SHALL be submitted via a method that does NOT call `AdjustBalance`

#### Scenario: Risk loop continues using existing method
- **WHEN** the risk loop closes a position (stop-loss, trailing-stop)
- **THEN** it SHALL continue using `InsertTradeAndUpdatePosition` with its per-trade balance adjustment

---

### Requirement: local balance tracking during open phase
During the open phase of a flush, the engine SHALL track the available balance locally in memory. After each successful open trade insertion, the engine SHALL deduct the trade's margin (futures) or full notional (spot) from the local balance. Open signals SHALL be skipped when the local balance is insufficient.

#### Scenario: Local balance deducted after each open
- **WHEN** effective balance is $1000 and the first open uses $400 margin
- **THEN** the local balance for the second open SHALL be $600

#### Scenario: Open skipped when local balance insufficient
- **WHEN** local balance is $100 and the next open signal requires $300 margin
- **THEN** that open signal SHALL be skipped and the engine SHALL log a warning

#### Scenario: Subsequent opens still evaluated after a skip
- **WHEN** an open is skipped due to insufficient balance
- **THEN** remaining opens SHALL still be evaluated (a later signal may require less capital)
