## ADDED Requirements

### Requirement: Health check endpoint
The system SHALL expose a `GET /health` endpoint that returns HTTP 200 when the service is healthy (database and NATS connections are up).

#### Scenario: All dependencies healthy
- **WHEN** `GET /health` is called and both database and NATS connections are active
- **THEN** the system SHALL return HTTP 200 with body `{"status": "ok"}`

#### Scenario: Database connection down
- **WHEN** `GET /health` is called and the database connection has failed
- **THEN** the system SHALL return HTTP 503 with body indicating the database is unreachable

### Requirement: List accounts endpoint
The system SHALL expose a `GET /api/v1/accounts` endpoint that returns all trading accounts.

#### Scenario: Multiple accounts exist
- **WHEN** `GET /api/v1/accounts` is called and accounts "live" and "paper" exist
- **THEN** the system SHALL return HTTP 200 with a JSON array containing both accounts with their ID, name, type, and creation timestamp

#### Scenario: No accounts exist
- **WHEN** `GET /api/v1/accounts` is called and no accounts exist
- **THEN** the system SHALL return HTTP 200 with an empty JSON array

### Requirement: Portfolio summary endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/portfolio` endpoint that returns the portfolio summary for the specified account.

#### Scenario: Account with open positions
- **WHEN** `GET /api/v1/accounts/live/portfolio` is called and the account has open positions
- **THEN** the system SHALL return HTTP 200 with a JSON object containing open positions (symbol, quantity, average entry price, market type) and aggregate realized P&L

#### Scenario: Account not found
- **WHEN** `GET /api/v1/accounts/nonexistent/portfolio` is called
- **THEN** the system SHALL return HTTP 404 with an error message

### Requirement: List positions endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/positions` endpoint that returns all positions for the specified account. An optional query parameter `status` SHALL filter by open or closed positions (default: open).

#### Scenario: List open positions
- **WHEN** `GET /api/v1/accounts/live/positions?status=open` is called
- **THEN** the system SHALL return HTTP 200 with only open positions for the account

#### Scenario: List all positions including closed
- **WHEN** `GET /api/v1/accounts/live/positions?status=all` is called
- **THEN** the system SHALL return HTTP 200 with both open and closed positions

### Requirement: List trades endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/trades` endpoint that returns trades for the specified account. It SHALL support query parameters: `symbol`, `side`, `market_type`, `start`, `end`, `cursor`, and `limit`.

#### Scenario: List trades with filters
- **WHEN** `GET /api/v1/accounts/live/trades?symbol=BTC-USD&limit=10` is called
- **THEN** the system SHALL return HTTP 200 with up to 10 BTC-USD trades for the account, ordered by timestamp descending, with a next-page cursor if more results exist

#### Scenario: Paginate through trades
- **WHEN** `GET /api/v1/accounts/live/trades?cursor=abc123` is called with a valid cursor
- **THEN** the system SHALL return HTTP 200 with the next page of trades

#### Scenario: Invalid cursor
- **WHEN** `GET /api/v1/accounts/live/trades?cursor=invalid` is called
- **THEN** the system SHALL return HTTP 400 with an error message

### Requirement: List orders endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/orders` endpoint that returns orders for the specified account. It SHALL support query parameters: `status`, `symbol`, `cursor`, and `limit`.

#### Scenario: List open orders
- **WHEN** `GET /api/v1/accounts/live/orders?status=open` is called
- **THEN** the system SHALL return HTTP 200 with only open orders for the account

#### Scenario: List all orders
- **WHEN** `GET /api/v1/accounts/live/orders` is called with no status filter
- **THEN** the system SHALL return HTTP 200 with all orders ordered by created timestamp descending

### Requirement: JSON response format
All API endpoints SHALL return JSON responses with `Content-Type: application/json`. Error responses SHALL use the format `{"error": "<message>"}`.

#### Scenario: Successful response
- **WHEN** any API endpoint returns data successfully
- **THEN** the response SHALL have `Content-Type: application/json` and a valid JSON body

#### Scenario: Error response
- **WHEN** any API endpoint encounters an error
- **THEN** the response SHALL have an appropriate HTTP status code and body `{"error": "<descriptive message>"}`

### Requirement: Read-only API
All API endpoints SHALL be read-only (GET requests only). The system SHALL NOT expose any endpoints that create, update, or delete data. All writes SHALL come exclusively through NATS trade ingestion.

#### Scenario: Non-GET request to API
- **WHEN** a POST, PUT, or DELETE request is made to any `/api/v1/` endpoint
- **THEN** the system SHALL return HTTP 405 Method Not Allowed
