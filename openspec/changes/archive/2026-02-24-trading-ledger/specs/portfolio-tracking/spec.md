## ADDED Requirements

### Requirement: Account management
The system SHALL maintain a `ledger_accounts` table that stores trading accounts. Each account MUST have a unique ID, name, type (live/paper), and creation timestamp.

#### Scenario: Account referenced by trade
- **WHEN** a trade references an account ID that exists in `ledger_accounts`
- **THEN** the trade SHALL be associated with that account

#### Scenario: Trade for unknown account
- **WHEN** a trade references an account ID that does not exist in `ledger_accounts`
- **THEN** the system SHALL auto-create the account with the ID derived from the NATS subject and type inferred from the account name

### Requirement: Position tracking for spot trades
The system SHALL maintain positions in `ledger_positions` for each unique combination of account ID and symbol. For spot trades, a position SHALL track: symbol, total quantity held, average entry price (cost-basis weighted), total cost basis, and realized P&L.

#### Scenario: First buy of a symbol
- **WHEN** a spot buy trade is ingested for a symbol with no existing position
- **THEN** the system SHALL create a new position with quantity equal to the trade quantity, average entry price equal to the trade price, and cost basis equal to quantity × price

#### Scenario: Additional buy of existing position
- **WHEN** a spot buy trade is ingested for a symbol with an existing position
- **THEN** the system SHALL increase the position quantity and recalculate the average entry price as the weighted average of existing and new cost basis

#### Scenario: Partial sell of existing position
- **WHEN** a spot sell trade is ingested for a quantity less than the current position
- **THEN** the system SHALL decrease the position quantity, calculate realized P&L as (sell price − average entry price) × sell quantity, and add it to the cumulative realized P&L

#### Scenario: Full sell closing a position
- **WHEN** a spot sell trade is ingested for a quantity equal to the current position
- **THEN** the system SHALL set the position quantity to zero, calculate and record the final realized P&L, and mark the position as closed

### Requirement: Position tracking for leveraged futures
The system SHALL track futures positions with additional fields: leverage, margin, unrealized P&L, liquidation price, and direction (long/short). Futures positions SHALL support both long and short sides.

#### Scenario: Open a long futures position
- **WHEN** a futures buy trade is ingested for a symbol with no existing futures position
- **THEN** the system SHALL create a new position with direction "long", the specified leverage, and calculate the margin amount

#### Scenario: Open a short futures position
- **WHEN** a futures sell trade is ingested for a symbol with no existing futures position
- **THEN** the system SHALL create a new position with direction "short", the specified leverage, and calculate the margin amount

#### Scenario: Close a futures position
- **WHEN** a futures trade is ingested that fully offsets an existing futures position (sell for long, buy for short)
- **THEN** the system SHALL calculate realized P&L including leverage, release the margin, and mark the position as closed

### Requirement: Portfolio summary per account
The system SHALL provide a portfolio summary for a given account that includes: total number of open positions, total realized P&L across all positions, and a list of all current holdings with their quantities and average entry prices.

#### Scenario: Account with multiple open positions
- **WHEN** portfolio summary is requested for an account with 3 open spot positions
- **THEN** the system SHALL return all 3 positions with their current quantities, average entry prices, and the aggregate realized P&L

#### Scenario: Account with no positions
- **WHEN** portfolio summary is requested for an account with no trades
- **THEN** the system SHALL return an empty positions list and zero realized P&L

### Requirement: Cross-account isolation
The system SHALL ensure that positions are isolated per account. A trade in one account SHALL NOT affect positions in another account, even for the same symbol.

#### Scenario: Same symbol in different accounts
- **WHEN** a buy trade for BTC-USD is ingested for the "live" account and a separate buy trade for BTC-USD is ingested for the "paper" account
- **THEN** the system SHALL maintain two independent positions, one per account, each with its own quantity and cost basis

### Requirement: Position rebuild from trade history
The system SHALL support rebuilding all positions for an account by replaying the full trade history in chronological order. The rebuilt positions MUST match the current materialized positions.

#### Scenario: Rebuild positions after data repair
- **WHEN** a position rebuild is triggered for an account
- **THEN** the system SHALL delete all existing positions for that account, replay all trades in timestamp order, and produce positions identical to what incremental processing would have created
