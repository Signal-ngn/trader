## ADDED Requirements

### Requirement: PlatformClient accessible from internal/engine
The `PlatformClient` SHALL be moved from `cmd/trader/` to `internal/platform/` so that the engine package (`internal/engine`) can import and use it directly. The CLI commands SHALL import it from the new location.

#### Scenario: Engine imports platform client
- **WHEN** the trader engine binary is compiled
- **THEN** `internal/engine` SHALL import `internal/platform` for all platform API calls with no circular dependency

#### Scenario: CLI commands unaffected
- **WHEN** CLI commands that previously used `PlatformClient` from `cmd/trader/` are compiled
- **THEN** they SHALL import the same client from `internal/platform/` and behave identically

---

### Requirement: Trade submission method
The `PlatformClient` SHALL expose a `SubmitTrade(ctx, trade) error` method that calls `POST /api/v1/trades` with `Authorization: Bearer <api_key>` and returns nil on 2xx or 409, and an error on any other response.

#### Scenario: Trade submitted successfully
- **WHEN** `SubmitTrade` is called with a valid trade payload
- **THEN** it SHALL POST to `/api/v1/trades`, and return nil on a 2xx response

#### Scenario: Duplicate trade returns nil
- **WHEN** `SubmitTrade` is called for a trade ID already recorded by the platform
- **THEN** the platform returns 409 and `SubmitTrade` SHALL return nil (idempotent)

#### Scenario: Platform error returned
- **WHEN** `SubmitTrade` receives a non-2xx, non-409 response
- **THEN** it SHALL return an error containing the status code and response body

---

### Requirement: Portfolio query method
The `PlatformClient` SHALL expose a `GetPortfolio(ctx, accountID) (*Portfolio, error)` method that calls `GET /api/v1/accounts/{id}/portfolio` and returns the open positions with all fields needed by the engine.

#### Scenario: Portfolio returned successfully
- **WHEN** `GetPortfolio` is called for an account with open positions
- **THEN** it SHALL return a `Portfolio` struct containing a slice of positions each with `symbol`, `market_type`, `side`, `quantity`, `avg_entry_price`, `stop_loss`, `take_profit`, `leverage`, and `opened_at`

#### Scenario: Empty portfolio returned
- **WHEN** `GetPortfolio` is called for an account with no open positions
- **THEN** it SHALL return a `Portfolio` with an empty positions slice and no error

---

### Requirement: Account list method
The `PlatformClient` SHALL expose a `ListAccounts(ctx) ([]Account, error)` method that calls `GET /api/v1/accounts` and returns all accounts with their current balance.

#### Scenario: Accounts returned with balance
- **WHEN** `ListAccounts` is called
- **THEN** it SHALL return all accounts, each including the `balance` field

---

### Requirement: Balance update method
The `PlatformClient` SHALL expose a `SetBalance(ctx, accountID string, balance float64) error` method that calls `PUT /api/v1/accounts/{id}/balance`.

#### Scenario: Balance updated successfully
- **WHEN** `SetBalance` is called with a valid account ID and balance amount
- **THEN** it SHALL PUT `{"balance": <amount>}` to `/api/v1/accounts/{id}/balance` and return nil on 2xx

#### Scenario: Balance update fails
- **WHEN** `SetBalance` receives a non-2xx response
- **THEN** it SHALL return an error containing the status code

---

### Requirement: Auth resolve method
The `PlatformClient` SHALL expose a `ResolveAuth(ctx) (tenantID string, error)` method that calls `GET /auth/resolve` and returns the tenant ID for the authenticated API key.

#### Scenario: Tenant ID resolved
- **WHEN** `ResolveAuth` is called with a valid API key
- **THEN** it SHALL return the `tenant_id` string from the response

#### Scenario: Unauthorised response returns error
- **WHEN** `ResolveAuth` receives a 401 response
- **THEN** it SHALL return an error indicating the API key is invalid or missing
