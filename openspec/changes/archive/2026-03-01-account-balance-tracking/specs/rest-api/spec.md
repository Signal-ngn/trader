## ADDED Requirements

### Requirement: Set account balance endpoint
The system SHALL expose a `PUT /api/v1/accounts/{accountId}/balance` endpoint that sets the cash balance for the specified account. The request body SHALL be JSON with required field `amount` (number) and optional field `currency` (string, default `"USD"`). On success the system SHALL return HTTP 200 with the stored balance object. This endpoint is the mechanism for setting an initial balance and for manual corrections — it overwrites any current value unconditionally. The endpoint SHALL be protected by the standard auth middleware.

#### Scenario: Set balance with default currency
- **WHEN** `PUT /api/v1/accounts/live/balance` is called with body `{"amount": 50000}`
- **THEN** the system SHALL store the balance as 50000 USD and return HTTP 200 with `{"account_id": "live", "currency": "USD", "amount": 50000}`

#### Scenario: Set balance with explicit currency
- **WHEN** `PUT /api/v1/accounts/live/balance` is called with body `{"amount": 40000, "currency": "EUR"}`
- **THEN** the system SHALL store the balance as 40000 EUR and return HTTP 200 with `{"account_id": "live", "currency": "EUR", "amount": 40000}`

#### Scenario: Overwrite existing balance
- **WHEN** `PUT /api/v1/accounts/live/balance` is called twice with amounts 50000 then 48000
- **THEN** after the second call the stored balance SHALL be 48000 and the response SHALL reflect the new value

#### Scenario: Overwrite after automatic adjustments
- **WHEN** ingestion has adjusted the balance to 47500 and then `PUT /api/v1/accounts/live/balance` is called with amount 50000
- **THEN** the balance SHALL be reset to 50000, discarding any previously accumulated adjustments

#### Scenario: Missing amount field
- **WHEN** `PUT /api/v1/accounts/live/balance` is called with an empty body or a body missing the `amount` field
- **THEN** the system SHALL return HTTP 400 with an error message

#### Scenario: Requires authentication
- **WHEN** `PUT /api/v1/accounts/live/balance` is called without an `Authorization` header
- **THEN** the system SHALL return HTTP 401

### Requirement: Get account balance endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/balance` endpoint that returns the current cash balance for the specified account. An optional query parameter `currency` (default `"USD"`) selects the currency. When no balance row exists for the given (account, currency) the system SHALL return HTTP 404.

#### Scenario: Balance exists
- **WHEN** `GET /api/v1/accounts/live/balance` is called and a USD balance of 50000 exists
- **THEN** the system SHALL return HTTP 200 with `{"account_id": "live", "currency": "USD", "amount": 50000}`

#### Scenario: Balance not set
- **WHEN** `GET /api/v1/accounts/live/balance` is called and no balance row exists for USD
- **THEN** the system SHALL return HTTP 404 with an error message

#### Scenario: Balance for non-default currency
- **WHEN** `GET /api/v1/accounts/live/balance?currency=EUR` is called and a EUR balance of 40000 exists
- **THEN** the system SHALL return HTTP 200 with `{"account_id": "live", "currency": "EUR", "amount": 40000}`

#### Scenario: Balance reflects automatic adjustments
- **WHEN** ingestion has deducted 2000 from a balance that was set to 50000
- **THEN** `GET /api/v1/accounts/live/balance` SHALL return `{"amount": 48000, ...}`

#### Scenario: Requires authentication
- **WHEN** `GET /api/v1/accounts/live/balance` is called without an `Authorization` header
- **THEN** the system SHALL return HTTP 401

## MODIFIED Requirements

### Requirement: Portfolio summary endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/portfolio` endpoint that returns the portfolio summary for the specified account. Each position in the summary SHALL include metadata fields when present: `stop_loss`, `take_profit`, and `confidence`. The response SHALL include a `balance` field with the account's current USD balance when one has been set; the field SHALL be omitted when no balance exists.

#### Scenario: Account with open positions
- **WHEN** `GET /api/v1/accounts/live/portfolio` is called and the account has open positions
- **THEN** the system SHALL return HTTP 200 with a JSON object containing open positions (symbol, quantity, average entry price, market type, and any metadata fields) and aggregate realized P&L

#### Scenario: Account not found
- **WHEN** `GET /api/v1/accounts/nonexistent/portfolio` is called
- **THEN** the system SHALL return HTTP 404 with an error message

#### Scenario: Portfolio response includes balance when set
- **WHEN** `GET /api/v1/accounts/live/portfolio` is called and the current USD balance is 48000
- **THEN** the response JSON object SHALL include `"balance": 48000`

#### Scenario: Portfolio response omits balance when not set
- **WHEN** `GET /api/v1/accounts/live/portfolio` is called and no balance has been set for USD
- **THEN** the response JSON object SHALL NOT include a `balance` field
