## MODIFIED Requirements

### Requirement: Trade history storage
The system SHALL persist all ingested trades in the `ledger_trades` table with the following fields: trade ID (unique), account ID, symbol, side (buy/sell), quantity, price, fee, fee currency, market type (spot/futures), timestamp, and ingestion timestamp. Futures trades SHALL additionally store leverage, margin, and liquidation price. All trades SHALL additionally store optional metadata fields: strategy (string), entry_reason (string), exit_reason (string), confidence (float64), stop_loss (float64), and take_profit (float64).

#### Scenario: Trade persisted with all fields
- **WHEN** a valid trade event is ingested
- **THEN** the system SHALL store all trade fields including any provided metadata fields and set the ingestion timestamp to the current time

#### Scenario: Trade fields queryable
- **WHEN** a trade has been persisted
- **THEN** all stored fields including metadata fields SHALL be retrievable via the query interface

#### Scenario: Trade persisted without metadata fields
- **WHEN** a valid trade event is ingested without metadata fields
- **THEN** the system SHALL store the trade with metadata fields set to NULL
