## MODIFIED Requirements

### Requirement: List positions endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/positions` endpoint that returns positions for the specified account. An optional query parameter `status` SHALL filter by open or closed positions (default: open). The endpoint SHALL support cursor pagination via `limit` (default 50, max 200) and `cursor` query parameters, returning a `next_cursor` field in the response when more results exist. The response body SHALL be a JSON object with a `positions` array and an optional `next_cursor` string. Each position in the response SHALL include metadata fields when present: `exit_price`, `exit_reason`, `stop_loss`, `take_profit`, and `confidence`. Metadata fields SHALL be omitted from the JSON response when NULL.

#### Scenario: List open positions
- **WHEN** `GET /api/v1/accounts/live/positions?status=open` is called
- **THEN** the system SHALL return HTTP 200 with a JSON object `{"positions": [...], "next_cursor": "..."}` containing only open positions for the account

#### Scenario: List all positions including closed
- **WHEN** `GET /api/v1/accounts/live/positions?status=all` is called
- **THEN** the system SHALL return HTTP 200 with both open and closed positions ordered by `opened_at` descending

#### Scenario: Paginate through closed positions
- **WHEN** `GET /api/v1/accounts/live/positions?status=closed&limit=50` is called and the account has 120 closed positions
- **THEN** the system SHALL return 50 positions and a non-empty `next_cursor`
- **AND** calling with `?cursor=<next_cursor>` SHALL return the next page of positions

#### Scenario: Last page has no next_cursor
- **WHEN** a cursor page returns fewer positions than the requested limit
- **THEN** `next_cursor` SHALL be absent or empty in the response

#### Scenario: Closed position with exit metadata in response
- **WHEN** a closed position has exit_price 55000 and exit_reason "take profit reached"
- **THEN** the position JSON object SHALL include `"exit_price": 55000` and `"exit_reason": "take profit reached"`

#### Scenario: Open position with stop loss and take profit in response
- **WHEN** an open position has stop_loss 48000 and take_profit 55000
- **THEN** the position JSON object SHALL include `"stop_loss": 48000` and `"take_profit": 55000`

#### Scenario: Existing callers without limit/cursor still work
- **WHEN** `GET /api/v1/accounts/live/positions` is called without any pagination parameters
- **THEN** the system SHALL return HTTP 200 with up to 50 positions (default limit) and a `next_cursor` if more exist
