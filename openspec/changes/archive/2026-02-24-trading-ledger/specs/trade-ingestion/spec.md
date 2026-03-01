## ADDED Requirements

### Requirement: NATS JetStream subscription for trade events
The system SHALL subscribe to NATS JetStream subject `ledger.trades.>` using a durable consumer to receive trade events published by the trading bot. The subscription SHALL use at-least-once delivery semantics.

#### Scenario: Service starts and subscribes
- **WHEN** the ledger service starts
- **THEN** it SHALL create a JetStream durable consumer on subject `ledger.trades.>` and begin receiving trade events

#### Scenario: Service restarts after downtime
- **WHEN** the ledger service restarts after being offline
- **THEN** the durable consumer SHALL resume from the last acknowledged message and process any queued trades

### Requirement: Trade event validation
The system SHALL validate each incoming trade event before persisting it. A valid trade event MUST contain: trade ID, account ID, symbol, side (buy/sell), quantity, price, fee, fee currency, timestamp, and market type (spot/futures).

#### Scenario: Valid trade event received
- **WHEN** a trade event with all required fields and valid values is received
- **THEN** the system SHALL persist it to the `ledger_trades` table and acknowledge the message

#### Scenario: Trade event missing required fields
- **WHEN** a trade event is missing one or more required fields
- **THEN** the system SHALL log the validation error, reject the message, and NOT persist it

#### Scenario: Trade event with invalid market type
- **WHEN** a trade event has a market type other than "spot" or "futures"
- **THEN** the system SHALL log the validation error, reject the message, and NOT persist it

### Requirement: Idempotent trade ingestion
The system SHALL process trade events idempotently using the trade ID as the deduplication key. Redelivered messages with the same trade ID SHALL NOT create duplicate records.

#### Scenario: Duplicate trade event received
- **WHEN** a trade event with a trade ID that already exists in the database is received
- **THEN** the system SHALL skip the insert (ON CONFLICT DO NOTHING), acknowledge the message, and NOT return an error

#### Scenario: First delivery of trade event
- **WHEN** a trade event with a new trade ID is received
- **THEN** the system SHALL insert the trade and acknowledge the message

### Requirement: Multi-account trade routing
The system SHALL support receiving trades for multiple accounts via the NATS subject hierarchy `ledger.trades.<account>.<market_type>`. The account identifier in the subject SHALL match the account ID in the trade event payload.

#### Scenario: Trade for live spot account
- **WHEN** a trade event is published on `ledger.trades.live.spot`
- **THEN** the system SHALL persist it with the corresponding account ID

#### Scenario: Trade for paper futures account
- **WHEN** a trade event is published on `ledger.trades.paper.futures`
- **THEN** the system SHALL persist it with the corresponding account ID

### Requirement: Futures trade fields
The system SHALL accept additional fields for futures trades: leverage, margin amount, liquidation price, and funding fees. These fields SHALL be nullable for spot trades.

#### Scenario: Futures trade with leverage
- **WHEN** a futures trade event includes leverage, margin, and liquidation price
- **THEN** the system SHALL persist all futures-specific fields alongside the base trade fields

#### Scenario: Spot trade without futures fields
- **WHEN** a spot trade event is received without futures-specific fields
- **THEN** the system SHALL persist the trade with futures fields set to NULL

### Requirement: Transactional trade and position update
The system SHALL update the corresponding position in `ledger_positions` within the same database transaction as the trade insert. If either the trade insert or position update fails, the entire transaction SHALL be rolled back and the NATS message SHALL NOT be acknowledged.

#### Scenario: Successful trade and position update
- **WHEN** a valid trade event is received and both the trade insert and position update succeed
- **THEN** the system SHALL commit the transaction and acknowledge the NATS message

#### Scenario: Position update fails
- **WHEN** a valid trade event is received but the position update fails
- **THEN** the system SHALL roll back the transaction (including the trade insert) and NOT acknowledge the NATS message, allowing redelivery
