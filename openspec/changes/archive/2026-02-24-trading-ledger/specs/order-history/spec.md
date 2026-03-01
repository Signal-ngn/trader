## ADDED Requirements

### Requirement: Trade history storage
The system SHALL persist all ingested trades in the `ledger_trades` table with the following fields: trade ID (unique), account ID, symbol, side (buy/sell), quantity, price, fee, fee currency, market type (spot/futures), timestamp, and ingestion timestamp. Futures trades SHALL additionally store leverage, margin, and liquidation price.

#### Scenario: Trade persisted with all fields
- **WHEN** a valid trade event is ingested
- **THEN** the system SHALL store all trade fields and set the ingestion timestamp to the current time

#### Scenario: Trade fields queryable
- **WHEN** a trade has been persisted
- **THEN** all stored fields SHALL be retrievable via the query interface

### Requirement: Order history storage
The system SHALL persist orders in the `ledger_orders` table with the following fields: order ID (unique), account ID, symbol, side (buy/sell), order type (market/limit), requested quantity, filled quantity, average fill price, status (open/filled/partially_filled/cancelled), market type, created timestamp, and updated timestamp. An order MAY be associated with one or more trades via the order ID.

#### Scenario: Order with multiple fills
- **WHEN** an order has been filled by 3 separate trades
- **THEN** the system SHALL store the order with filled quantity as the sum of all trade quantities and average fill price as the volume-weighted average

#### Scenario: Partially filled order
- **WHEN** an order has been partially filled
- **THEN** the order status SHALL be "partially_filled" with filled quantity reflecting only the executed portion

### Requirement: Query trades by account
The system SHALL support querying trades filtered by account ID, with results ordered by timestamp descending (most recent first).

#### Scenario: Query all trades for an account
- **WHEN** trades are queried for account "live" with no additional filters
- **THEN** the system SHALL return all trades for that account ordered by timestamp descending

#### Scenario: Query trades for account with no trades
- **WHEN** trades are queried for an account that has no trades
- **THEN** the system SHALL return an empty list

### Requirement: Query trades with filters
The system SHALL support filtering trades by: symbol, side (buy/sell), market type (spot/futures), and time range (start/end timestamps). Filters SHALL be combinable.

#### Scenario: Filter trades by symbol and time range
- **WHEN** trades are queried for account "live" with symbol "BTC-USD" and a time range of the last 7 days
- **THEN** the system SHALL return only BTC-USD trades within that time range

#### Scenario: Filter by market type
- **WHEN** trades are queried for account "live" with market type "futures"
- **THEN** the system SHALL return only futures trades for that account

### Requirement: Pagination for trade queries
The system SHALL support cursor-based pagination for trade queries. Each page SHALL return a configurable number of results (default 50, max 200) and a cursor for the next page.

#### Scenario: First page of trades
- **WHEN** trades are queried without a cursor
- **THEN** the system SHALL return the first page of results and a next-page cursor if more results exist

#### Scenario: Subsequent page of trades
- **WHEN** trades are queried with a valid next-page cursor
- **THEN** the system SHALL return the next page of results starting after the cursor position

#### Scenario: Last page of trades
- **WHEN** a page is requested and fewer results remain than the page size
- **THEN** the system SHALL return the remaining results with no next-page cursor

### Requirement: Query orders by account
The system SHALL support querying orders filtered by account ID and status, with results ordered by created timestamp descending.

#### Scenario: Query open orders
- **WHEN** orders are queried for account "live" with status "open"
- **THEN** the system SHALL return only open orders for that account

#### Scenario: Query all orders for an account
- **WHEN** orders are queried for account "live" with no status filter
- **THEN** the system SHALL return all orders for that account ordered by created timestamp descending
