## MODIFIED Requirements

### Requirement: Account management
The engine SHALL load managed accounts at startup by calling `GET /api/v1/accounts` on the platform API. The engine SHALL NOT query `ledger_accounts` directly. Account auto-creation (for unknown account IDs) is the platform's responsibility when trades are submitted via `POST /api/v1/trades`.

#### Scenario: Account referenced by trade
- **WHEN** the engine submits a trade for an account via `POST /api/v1/trades`
- **THEN** the platform SHALL associate the trade with the account, auto-creating it if it does not exist

#### Scenario: Trade for unknown account
- **WHEN** the engine submits a trade for an account ID not yet present on the platform
- **THEN** the platform SHALL auto-create the account and record the trade

### Requirement: Portfolio summary per account
The engine SHALL read open position state at startup by calling `GET /api/v1/accounts/{id}/portfolio`. This endpoint SHALL return all open positions with sufficient fields for the engine to seed its conflict guard and compute exit trade cost basis: `symbol`, `market_type`, `side`, `quantity`, `avg_entry_price`, `stop_loss`, `take_profit`, `leverage`, `opened_at`.

#### Scenario: Account with multiple open positions
- **WHEN** the engine calls `GET /api/v1/accounts/{id}/portfolio` for an account with 3 open positions
- **THEN** the response SHALL include all 3 positions with `avg_entry_price`, `side`, `market_type`, and `quantity`

#### Scenario: Account with no positions
- **WHEN** the engine calls `GET /api/v1/accounts/{id}/portfolio` for an account with no open positions
- **THEN** the response SHALL return an empty positions list

## REMOVED Requirements

### Requirement: Position tracking for spot trades
**Reason**: The engine no longer maintains `ledger_positions` directly. Spot position tracking is performed by the platform when trades are submitted via `POST /api/v1/trades` and stored in `tenant_positions`.
**Migration**: Open and closed positions are available via `GET /api/v1/accounts/{id}/portfolio` and `GET /api/v1/accounts/{id}/positions`.

### Requirement: Position tracking for leveraged futures
**Reason**: The engine no longer maintains `ledger_positions` directly. Futures position tracking is performed by the platform when trades are submitted via `POST /api/v1/trades` and stored in `tenant_positions`.
**Migration**: Open and closed futures positions are available via `GET /api/v1/accounts/{id}/portfolio`.

### Requirement: Position rebuild from trade history
**Reason**: Position rebuild was a recovery mechanism for the engine's local `ledger_positions` table. With the platform owning position state, rebuild is the platform's concern and outside the engine's scope.
**Migration**: The platform API may provide a rebuild endpoint in future if needed.
