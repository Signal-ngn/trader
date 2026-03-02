## ADDED Requirements

### Requirement: Periodic risk loop
The engine SHALL run a risk management loop every 5 minutes that evaluates all open positions for the account. The loop SHALL be a goroutine started inside `engine.Start()` and stopped via context cancellation.

#### Scenario: Risk loop runs on interval
- **WHEN** the engine is running and 5 minutes have elapsed
- **THEN** the risk loop SHALL evaluate all open positions in `engine_position_state`

#### Scenario: Risk loop stops on shutdown
- **WHEN** the context is cancelled (SIGTERM)
- **THEN** the risk loop goroutine SHALL exit cleanly

### Requirement: Reconcile position state with ledger on each tick
On each risk loop tick, the engine SHALL query `ledger_positions` for open positions and prune any rows from `engine_position_state` that no longer have a corresponding open position.

#### Scenario: Stale risk state pruned
- **WHEN** a position was closed manually (via the API or CLI) and the risk loop ticks
- **THEN** the engine SHALL delete the orphaned `engine_position_state` row

### Requirement: Stop-loss enforcement
The engine SHALL close a position when the current price breaches the stop-loss level. For a long position, close when price ≤ stop_loss. For a short position, close when price ≥ stop_loss. If the stored stop_loss is within 0.1% of entry price, the engine SHALL use a default of -4% from entry price instead.

#### Scenario: Long position stop-loss hit
- **WHEN** a long position has stop_loss=65000 and current price is 64900
- **THEN** the engine SHALL close the position with exit_reason=`stop loss`

#### Scenario: Short position stop-loss hit
- **WHEN** a short position has stop_loss=68000 and current price is 68100
- **THEN** the engine SHALL close the position with exit_reason=`stop loss`

#### Scenario: Stop-loss too close to entry — default used
- **WHEN** a position has entry_price=66850 and stop_loss=66900 (within 0.1%)
- **THEN** the engine SHALL use stop_loss = entry_price × 0.96 instead

### Requirement: Take-profit enforcement
The engine SHALL close a position when the current price reaches the take-profit level. For a long position, close when price ≥ take_profit. For a short position, close when price ≤ take_profit. If the stored take_profit is within 0.1% of entry price, the engine SHALL use a default of +10% from entry price instead.

#### Scenario: Long position take-profit hit
- **WHEN** a long position has take_profit=70000 and current price is 70100
- **THEN** the engine SHALL close the position with exit_reason=`take profit`

#### Scenario: Take-profit too close to entry — default used
- **WHEN** a position has entry_price=66850 and take_profit=66900 (within 0.1%)
- **THEN** the engine SHALL use take_profit = entry_price × 1.10 instead

### Requirement: Trailing stop
The engine SHALL activate a trailing stop when a position's unrealised P&L reaches +3% (post-leverage). The trailing stop SHALL be set 2% behind the peak price seen since activation. The trailing stop SHALL only tighten — it SHALL never move away from the current price. The position SHALL be closed when price breaches the trailing stop.

#### Scenario: Trailing stop activates at +3%
- **WHEN** a long position's unrealised P&L reaches +3%
- **THEN** the engine SHALL record the peak price and set trailing_stop = peak_price × 0.98

#### Scenario: Trailing stop tightens as price rises
- **WHEN** the price rises above the previous peak after trailing stop activation
- **THEN** the engine SHALL update peak_price and recalculate trailing_stop = new_peak × 0.98

#### Scenario: Trailing stop never loosens
- **WHEN** the price falls below the previous peak
- **THEN** the trailing stop level SHALL remain unchanged

#### Scenario: Position closed when trailing stop hit
- **WHEN** price falls below the trailing stop level
- **THEN** the engine SHALL close the position with exit_reason=`trailing stop`

### Requirement: Maximum hold time enforcement
The engine SHALL close positions that have been open longer than 48 hours, measured from the `opened_at` timestamp in `engine_position_state`.

#### Scenario: Position held too long — closed
- **WHEN** a position has been open for more than 48 hours at risk loop tick time
- **THEN** the engine SHALL close the position with exit_reason=`max hold time`

#### Scenario: Position within hold time — not closed
- **WHEN** a position has been open for 24 hours
- **THEN** the engine SHALL not close it based on hold time alone

### Requirement: Kill switch for live mode
In live mode, the engine SHALL halt opening new positions when the kill switch file exists on disk (`KILL_SWITCH_FILE`, defaulting to `/tmp/trader.kill`). Existing open positions SHALL continue to be monitored by the risk loop. The kill switch SHALL be checked on every signal and every risk loop tick.

#### Scenario: Kill switch active — new trade blocked
- **WHEN** the kill switch file exists and a BUY signal is received
- **THEN** the engine SHALL skip the trade and log "kill switch active"

#### Scenario: Kill switch inactive — trade allowed
- **WHEN** the kill switch file does not exist
- **THEN** the engine SHALL process signals normally

#### Scenario: Kill switch does not affect risk loop closes
- **WHEN** the kill switch file exists and a position hits its stop-loss
- **THEN** the engine SHALL still close the position

### Requirement: Daily loss limit
When `DAILY_LOSS_LIMIT` is set to a value greater than 0, the engine SHALL stop opening new positions for the remainder of the calendar day (UTC) once realised losses for that day exceed the limit. The daily loss is calculated from trades recorded by the engine since midnight UTC.

#### Scenario: Daily loss limit reached — new trades blocked
- **WHEN** DAILY_LOSS_LIMIT=500 and realised losses today total $520
- **THEN** the engine SHALL skip new opening trades and log "daily loss limit reached"

#### Scenario: Daily loss limit resets at midnight UTC
- **WHEN** the calendar day rolls over to a new UTC day
- **THEN** the engine SHALL reset the daily loss counter and resume normal trading
