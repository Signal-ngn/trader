## MODIFIED Requirements

### Requirement: Portfolio summary per account
The system SHALL provide a portfolio summary for a given account that includes: total number of open positions, total realized P&L across all positions, a list of all current holdings with their quantities and average entry prices, and the account's current balance when one has been set. The balance SHALL be fetched for the default currency `'USD'` and included as a `balance` field in the response only when a balance row exists; the field SHALL be omitted when no balance has been set.

#### Scenario: Account with multiple open positions
- **WHEN** portfolio summary is requested for an account with 3 open spot positions
- **THEN** the system SHALL return all 3 positions with their current quantities, average entry prices, and the aggregate realized P&L

#### Scenario: Account with no positions
- **WHEN** portfolio summary is requested for an account with no trades
- **THEN** the system SHALL return an empty positions list and zero realized P&L

#### Scenario: Portfolio summary includes balance when set
- **WHEN** portfolio summary is requested for an account that has a USD balance of 50000 set
- **THEN** the response SHALL include `"balance": 50000` alongside the positions and realized P&L

#### Scenario: Portfolio summary omits balance when not set
- **WHEN** portfolio summary is requested for an account that has no balance row
- **THEN** the response SHALL NOT include a `balance` field
