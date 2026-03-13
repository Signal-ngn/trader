## ADDED Requirements

### Requirement: EngineStore interface
The engine SHALL depend on a narrow `EngineStore` interface defined in `internal/engine`, covering only the store operations the engine calls. No engine code SHALL import `internal/store` or reference `store.Repository` directly.

#### Scenario: Engine compiled without store dependency
- **WHEN** the trader engine binary is compiled
- **THEN** it SHALL NOT require a database driver or connection pool import in the engine package

#### Scenario: EngineStore satisfied by APIEngineStore
- **WHEN** `APIEngineStore` is passed to the engine constructor
- **THEN** the engine SHALL operate correctly without any other store implementation present

---

### Requirement: Tenant ID resolved via platform API
On startup the engine SHALL resolve its `tenant_id` by calling `GET /auth/resolve` with `Authorization: Bearer <SN_API_KEY>`. The engine SHALL fail fast if the API key is absent or the call fails.

#### Scenario: Tenant resolved successfully
- **WHEN** the engine starts with a valid `SN_API_KEY`
- **THEN** it SHALL call `GET /auth/resolve`, extract `tenant_id` from the response, and use it for all subsequent platform API calls

#### Scenario: Missing API key aborts startup
- **WHEN** `SN_API_KEY` is not set in the environment
- **THEN** the engine SHALL log a clear error and exit non-zero before attempting any other initialisation

#### Scenario: Auth resolve call fails
- **WHEN** `GET /auth/resolve` returns a non-2xx response or a network error
- **THEN** the engine SHALL log the error and exit non-zero

---

### Requirement: Managed accounts loaded from platform API
On startup the engine SHALL load the list of managed accounts by calling `GET /api/v1/accounts` with `Authorization: Bearer <SN_API_KEY>`.

#### Scenario: Accounts loaded successfully
- **WHEN** the engine starts and `GET /api/v1/accounts` returns a list of accounts
- **THEN** the engine SHALL register each account for signal processing

#### Scenario: No accounts returned
- **WHEN** `GET /api/v1/accounts` returns an empty list
- **THEN** the engine SHALL start with no managed accounts and log a warning

---

### Requirement: Trade recorded via platform API
The engine SHALL record every executed trade by calling `POST /api/v1/trades` with `Authorization: Bearer <SN_API_KEY>`. The engine SHALL NOT write to any database table directly.

#### Scenario: Trade submitted successfully
- **WHEN** the engine executes a trade and calls `POST /api/v1/trades`
- **THEN** the platform SHALL return 2xx and the trade SHALL be recorded in `tenant_trades`

#### Scenario: Duplicate trade silently ignored
- **WHEN** the engine retries a trade submission and the platform returns 409
- **THEN** the engine SHALL treat the call as successful (idempotent) and continue

#### Scenario: Trade submission fails
- **WHEN** `POST /api/v1/trades` returns a non-2xx, non-409 response or a network error
- **THEN** the engine SHALL log the error and skip the trade rather than blocking the signal consumer

---

### Requirement: Account balance read and updated via platform API
The engine SHALL read the current available balance via `GET /api/v1/accounts` for position sizing, and update the balance via `PUT /api/v1/accounts/{id}/balance` after every balance-affecting trade event.

#### Scenario: Balance read for position sizing
- **WHEN** the engine evaluates a signal and needs to size a position
- **THEN** it SHALL use the `balance` field from the account returned by `GET /api/v1/accounts`

#### Scenario: Balance seeded on first boot
- **WHEN** the engine starts and no balance exists for the account on the platform
- **THEN** the engine SHALL call `PUT /api/v1/accounts/{id}/balance` with `PORTFOLIO_SIZE_USD` to initialise it

#### Scenario: Balance updated after trade open
- **WHEN** the engine opens a position
- **THEN** it SHALL call `PUT /api/v1/accounts/{id}/balance` with the new balance (previous balance minus margin/cost)

#### Scenario: Balance updated after trade close
- **WHEN** the engine closes a position
- **THEN** it SHALL call `PUT /api/v1/accounts/{id}/balance` with the new balance (previous balance plus returned margin plus realised P&L)

#### Scenario: Balance update failure is non-fatal
- **WHEN** `PUT /api/v1/accounts/{id}/balance` fails after a successful trade write
- **THEN** the engine SHALL log a warning and continue — the trade is already recorded

---

### Requirement: Open positions read from platform API on startup
On startup the engine SHALL call `GET /api/v1/accounts/{id}/portfolio` to load open positions for the conflict guard and average entry price lookups.

#### Scenario: Open positions loaded on startup
- **WHEN** the engine starts and `GET /api/v1/accounts/{id}/portfolio` returns open positions
- **THEN** the engine SHALL seed its conflict guard with those positions so it does not attempt to open duplicate positions

#### Scenario: Average entry price from portfolio
- **WHEN** the engine needs the average entry price for an open position (e.g. to compute exit trade cost basis)
- **THEN** it SHALL read `avg_entry_price` from the position returned by `GET /api/v1/accounts/{id}/portfolio`

---

### Requirement: Risk state persisted in Firestore
The engine SHALL persist all risk-management state for open positions — stop-loss, take-profit, hard stop, trailing stop, peak price, leverage, strategy, granularity, and opened-at — in GCP Firestore under the collection path `engine-state/{accountID}/positions/{symbol}-{marketType}`.

#### Scenario: Position state written on open
- **WHEN** the engine opens a new position
- **THEN** it SHALL write a Firestore document with all risk fields for that position

#### Scenario: Position state updated on risk change
- **WHEN** the engine updates trailing stop or peak price
- **THEN** it SHALL update the corresponding Firestore document

#### Scenario: Position state deleted on close
- **WHEN** the engine closes a position
- **THEN** it SHALL delete the corresponding Firestore document

#### Scenario: Risk state restored on restart
- **WHEN** the engine restarts with open positions
- **THEN** it SHALL read all Firestore documents under `engine-state/{accountID}/positions/` and fully restore stop-loss, take-profit, hard stop, trailing stop, and peak price for each open position

---

### Requirement: Daily P&L persisted in Firestore
The engine SHALL persist the daily realised P&L accumulator in Firestore under `engine-state/{accountID}/daily-pnl/{accountID}-{YYYY-MM-DD}`, using atomic increments to avoid lost updates.

#### Scenario: Daily P&L incremented after close
- **WHEN** the engine closes a position and records realised P&L
- **THEN** it SHALL atomically increment the Firestore daily P&L document for today's UTC date

#### Scenario: Daily P&L loaded on restart
- **WHEN** the engine restarts mid-day
- **THEN** it SHALL read today's Firestore daily P&L document and restore the accumulated value so the daily loss limit is correctly enforced

#### Scenario: No document for today
- **WHEN** the engine starts and no daily P&L document exists for today
- **THEN** it SHALL treat daily P&L as zero

---

### Requirement: Firestore and platform API configuration
The engine SHALL read `SN_API_KEY` and `TRADER_API_URL` (default: `https://signalngn-api-potbdcvufa-ew.a.run.app`) from the environment for platform API access, and `FIRESTORE_PROJECT_ID` for Firestore access. The engine SHALL use Application Default Credentials (ADC) for Firestore authentication — no explicit credentials file is needed when running on Cloud Run with a service account that has `roles/datastore.user`.

#### Scenario: Missing FIRESTORE_PROJECT_ID aborts startup
- **WHEN** `FIRESTORE_PROJECT_ID` is not set
- **THEN** the engine SHALL log a clear error and exit non-zero

#### Scenario: Cloud Run uses service account credentials
- **WHEN** the engine runs on Cloud Run with the trader service account
- **THEN** it SHALL authenticate to Firestore via ADC without any explicit credentials configuration
