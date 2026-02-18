## MODIFIED Requirements

### Requirement: List trades endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/trades` endpoint that returns trades for the specified account. It SHALL support query parameters: `symbol`, `side`, `market_type`, `start`, `end`, `cursor`, and `limit`. Each trade in the response SHALL include metadata fields when present: `strategy`, `entry_reason`, `exit_reason`, `confidence`, `stop_loss`, and `take_profit`. Metadata fields SHALL be omitted from the JSON response when NULL.

#### Scenario: List trades with filters
- **WHEN** `GET /api/v1/accounts/live/trades?symbol=BTC-USD&limit=10` is called
- **THEN** the system SHALL return HTTP 200 with up to 10 BTC-USD trades for the account, ordered by timestamp descending, with a next-page cursor if more results exist

#### Scenario: Paginate through trades
- **WHEN** `GET /api/v1/accounts/live/trades?cursor=abc123` is called with a valid cursor
- **THEN** the system SHALL return HTTP 200 with the next page of trades

#### Scenario: Invalid cursor
- **WHEN** `GET /api/v1/accounts/live/trades?cursor=invalid` is called
- **THEN** the system SHALL return HTTP 400 with an error message

#### Scenario: Trade with metadata fields in response
- **WHEN** a trade has strategy "macd-rsi-v2" and confidence 0.85
- **THEN** the trade JSON object SHALL include `"strategy": "macd-rsi-v2"` and `"confidence": 0.85`

#### Scenario: Trade without metadata fields in response
- **WHEN** a trade has no metadata fields (all NULL)
- **THEN** the trade JSON object SHALL omit the metadata fields

### Requirement: List positions endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/positions` endpoint that returns all positions for the specified account. An optional query parameter `status` SHALL filter by open or closed positions (default: open). Each position in the response SHALL include metadata fields when present: `exit_price`, `exit_reason`, `stop_loss`, `take_profit`, and `confidence`. Metadata fields SHALL be omitted from the JSON response when NULL.

#### Scenario: List open positions
- **WHEN** `GET /api/v1/accounts/live/positions?status=open` is called
- **THEN** the system SHALL return HTTP 200 with only open positions for the account

#### Scenario: List all positions including closed
- **WHEN** `GET /api/v1/accounts/live/positions?status=all` is called
- **THEN** the system SHALL return HTTP 200 with both open and closed positions

#### Scenario: Closed position with exit metadata in response
- **WHEN** a closed position has exit_price 55000 and exit_reason "take profit reached"
- **THEN** the position JSON object SHALL include `"exit_price": 55000` and `"exit_reason": "take profit reached"`

#### Scenario: Open position with stop loss and take profit in response
- **WHEN** an open position has stop_loss 48000 and take_profit 55000
- **THEN** the position JSON object SHALL include `"stop_loss": 48000` and `"take_profit": 55000`

### Requirement: Portfolio summary endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/portfolio` endpoint that returns the portfolio summary for the specified account. Each position in the summary SHALL include metadata fields when present: `stop_loss`, `take_profit`, and `confidence`.

#### Scenario: Account with open positions
- **WHEN** `GET /api/v1/accounts/live/portfolio` is called and the account has open positions
- **THEN** the system SHALL return HTTP 200 with a JSON object containing open positions (symbol, quantity, average entry price, market type, and any metadata fields) and aggregate realized P&L

#### Scenario: Account not found
- **WHEN** `GET /api/v1/accounts/nonexistent/portfolio` is called
- **THEN** the system SHALL return HTTP 404 with an error message
