## ADDED Requirements

### Requirement: Map signal actions to trade sides
The engine SHALL map signal actions to ledger trade sides and position directions as follows: `BUY` → side=`buy`, opens/adds to long; `SELL` → side=`sell`, closes long; `SHORT` → side=`sell`, opens short; `COVER` → side=`buy`, closes short. Market type (spot or futures) SHALL be taken from the trading config for the product.

#### Scenario: BUY signal mapped to buy trade
- **WHEN** a BUY signal is received for a product configured as futures
- **THEN** the engine SHALL create a buy trade with market_type=`futures` and open a long position

#### Scenario: SHORT signal mapped to sell trade
- **WHEN** a SHORT signal is received
- **THEN** the engine SHALL create a sell trade that opens a short futures position

#### Scenario: SELL signal closes long position
- **WHEN** a SELL signal is received and a long position is open for that product
- **THEN** the engine SHALL create a sell trade that closes the long position

### Requirement: Calculate position size
The engine SHALL calculate position size as: `base_size = PORTFOLIO_SIZE × (POSITION_SIZE_PCT / 100)`, clamped to `[MIN_POSITION_SIZE, MAX_POSITION_SIZE]`. If the signal provides a `position_pct` field, it SHALL be used instead of `POSITION_SIZE_PCT`. Quantity SHALL be `size / price`. Margin for futures SHALL be `size / leverage` where leverage comes from the trading config.

#### Scenario: Default position sizing
- **WHEN** a BUY signal arrives with no `position_pct` field and defaults are PORTFOLIO_SIZE=10000, POSITION_SIZE_PCT=15, MIN=150, MAX=2000
- **THEN** the engine SHALL calculate size=1500, quantity=size/price

#### Scenario: Signal-provided position_pct used
- **WHEN** a BUY signal arrives with `position_pct=0.20`
- **THEN** the engine SHALL calculate size = PORTFOLIO_SIZE × 0.20, clamped to [MIN, MAX]

#### Scenario: Size clamped to maximum
- **WHEN** the calculated size exceeds MAX_POSITION_SIZE
- **THEN** the engine SHALL use MAX_POSITION_SIZE as the size

#### Scenario: Size clamped to minimum
- **WHEN** the calculated size is below MIN_POSITION_SIZE
- **THEN** the engine SHALL use MIN_POSITION_SIZE as the size

### Requirement: Check account balance before opening a position
Before executing any opening trade, the engine SHALL call `GetAccountBalance` for the account. If a balance exists and is less than the required margin (futures) or cost basis (spot), the engine SHALL skip the trade and log the reason. If no balance row exists for the account, the check SHALL be bypassed.

#### Scenario: Sufficient balance — trade proceeds
- **WHEN** a BUY signal is received, required margin is $150, and account balance is $500
- **THEN** the engine SHALL proceed with the trade

#### Scenario: Insufficient balance — trade skipped
- **WHEN** a BUY signal is received, required margin is $300, and account balance is $200
- **THEN** the engine SHALL skip the trade and log "insufficient balance: need $300, have $200"

#### Scenario: No balance set — check bypassed
- **WHEN** a BUY signal is received and no balance row exists for the account
- **THEN** the engine SHALL proceed with the trade without a balance check

### Requirement: Direction conflict guard
The engine SHALL prevent opening a long position if a short is already open for the same product, and vice versa. On startup the engine SHALL seed the conflict guard by loading open positions from `ledger_positions`. The guard SHALL be updated in memory on every open and close.

#### Scenario: Conflict detected — trade skipped
- **WHEN** a BUY (long) signal is received and a short position is already open for that product
- **THEN** the engine SHALL skip the trade and log the conflict

#### Scenario: No conflict — trade proceeds
- **WHEN** a BUY signal is received and no position is open for that product
- **THEN** the engine SHALL proceed with the trade

#### Scenario: Conflict guard seeded on startup
- **WHEN** the engine starts and `ledger_positions` contains an open short for BTC-USD
- **THEN** the engine SHALL block BUY signals for BTC-USD until the short is closed

### Requirement: Execute paper trades directly via store
In paper mode, the engine SHALL construct a `domain.Trade` and call `repo.InsertTradeAndUpdatePosition` directly. No exchange API call is made. The trade SHALL include all strategy metadata fields from the signal (strategy, confidence, stop_loss, take_profit, entry_reason).

#### Scenario: Paper BUY trade recorded
- **WHEN** a BUY signal is processed in paper mode
- **THEN** the engine SHALL insert a buy trade and update the position in a single transaction

#### Scenario: Paper trade includes strategy metadata
- **WHEN** a signal with strategy=`ml_xgboost+trend` and confidence=0.78 is processed
- **THEN** the recorded trade SHALL have strategy=`ml_xgboost+trend` and confidence=0.78

### Requirement: Execute live trades via exchange then ledger
In live mode, the engine SHALL call the exchange adapter to open or close the position first. If the exchange call succeeds, the engine SHALL record the trade in the ledger using the actual fill price and quantity from the order result. If the exchange call fails, the engine SHALL abort and NOT record the trade.

#### Scenario: Live trade executed and recorded
- **WHEN** a BUY signal is processed in live mode and the exchange order succeeds
- **THEN** the engine SHALL record the trade in the ledger using the actual fill price

#### Scenario: Exchange call fails — trade not recorded
- **WHEN** a BUY signal is processed in live mode and the exchange returns an error
- **THEN** the engine SHALL log the error and NOT record a trade in the ledger

#### Scenario: Ledger write fails after successful exchange order
- **WHEN** the exchange order succeeds but `InsertTradeAndUpdatePosition` fails
- **THEN** the engine SHALL log a critical error with full order details for manual recovery

### Requirement: Persist position risk state
After opening a position, the engine SHALL persist risk metadata to the `engine_position_state` table: entry price, stop_loss, take_profit, leverage, market_type, strategy, and opened_at. This state SHALL survive service restarts and be loaded on startup.

#### Scenario: Risk state written on position open
- **WHEN** a position is opened
- **THEN** a row SHALL be inserted into `engine_position_state` with the signal's SL/TP values and strategy

#### Scenario: Risk state loaded on startup
- **WHEN** the engine starts and `engine_position_state` contains rows
- **THEN** the engine SHALL load all rows into its in-memory risk state cache

#### Scenario: Risk state pruned on position close
- **WHEN** a position is closed
- **THEN** the corresponding row in `engine_position_state` SHALL be deleted

### Requirement: Enforce max concurrent positions limit
When `MAX_POSITIONS` is set to a value greater than 0, the engine SHALL refuse to open new positions when the number of open positions for the account meets or exceeds the limit.

#### Scenario: Max positions not reached — trade allowed
- **WHEN** MAX_POSITIONS=3 and 2 positions are currently open
- **THEN** the engine SHALL allow the new position to open

#### Scenario: Max positions reached — trade skipped
- **WHEN** MAX_POSITIONS=3 and 3 positions are currently open
- **THEN** the engine SHALL skip the trade and log the reason
