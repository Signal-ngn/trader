## ADDED Requirements

### Requirement: Account balance storage
The system SHALL maintain a `ledger_account_balances` table that stores one cash balance row per (tenant_id, account_id, currency). The table SHALL have columns: `tenant_id` (UUID), `account_id` (TEXT), `currency` (TEXT, NOT NULL, default `'USD'`), `amount` (DOUBLE PRECISION, NOT NULL, default 0), and `updated_at` (TIMESTAMPTZ, updated on every write). The primary key SHALL be `(tenant_id, account_id, currency)`.

#### Scenario: First balance set for an account
- **WHEN** a balance is set for a (tenant, account, currency) combination that has no existing row
- **THEN** the system SHALL insert a new row with the given amount and set updated_at to the current timestamp

#### Scenario: Balance updated for an existing account
- **WHEN** a balance is set for a (tenant, account, currency) combination that already has a row
- **THEN** the system SHALL overwrite the amount and update updated_at to the current timestamp

#### Scenario: Multiple currencies per account
- **WHEN** balances are set for the same (tenant, account) with currencies `'USD'` and `'EUR'`
- **THEN** the system SHALL maintain two independent rows, one per currency

#### Scenario: Tenant isolation
- **WHEN** two tenants each set a balance for an account named `'live'`
- **THEN** each tenant SHALL have their own independent balance row and reads SHALL return only the requesting tenant's value

### Requirement: Set account balance
The system SHALL expose a store method `SetAccountBalance(ctx, tenantID, accountID, currency, amount)` that upserts the balance row for the given (tenant, account, currency) key, setting amount and updated_at unconditionally. The method SHALL be idempotent — calling it twice with the same amount SHALL result in a single row with that amount.

#### Scenario: Set balance succeeds
- **WHEN** `SetAccountBalance` is called with a valid tenant, account, currency, and amount
- **THEN** the balance row SHALL be created or overwritten and the method SHALL return no error

#### Scenario: Set balance with zero amount
- **WHEN** `SetAccountBalance` is called with amount 0
- **THEN** the system SHALL store 0 without error

### Requirement: Get account balance
The system SHALL expose a store method `GetAccountBalance(ctx, tenantID, accountID, currency)` that returns a pointer to the current balance amount for the given key, or `nil` when no balance row exists.

#### Scenario: Balance exists
- **WHEN** `GetAccountBalance` is called for a (tenant, account, currency) that has a balance row
- **THEN** the method SHALL return a non-nil pointer to the amount value

#### Scenario: Balance not set
- **WHEN** `GetAccountBalance` is called for a (tenant, account, currency) with no balance row
- **THEN** the method SHALL return `nil` and no error

### Requirement: Adjust account balance
The system SHALL expose a store method `AdjustBalance(ctx, tx, tenantID, accountID, currency, delta)` that atomically applies a signed delta to the balance amount using `UPDATE ... SET amount = amount + delta`. The method MUST execute within the provided database transaction. When no balance row exists for the given key the method SHALL do nothing (no-op) and return no error.

#### Scenario: Positive delta credits balance
- **WHEN** `AdjustBalance` is called with delta +1000 on an account with balance 5000
- **THEN** the balance SHALL become 6000

#### Scenario: Negative delta debits balance
- **WHEN** `AdjustBalance` is called with delta −2000 on an account with balance 5000
- **THEN** the balance SHALL become 3000

#### Scenario: No-op when balance row does not exist
- **WHEN** `AdjustBalance` is called for a (tenant, account, currency) with no balance row
- **THEN** the method SHALL return no error and no row SHALL be created

#### Scenario: Adjustment rolls back with transaction
- **WHEN** `AdjustBalance` is called within a transaction that is subsequently rolled back
- **THEN** the balance amount SHALL remain unchanged from before the transaction began

### Requirement: Balance auto-adjustment on trade ingestion — spot
The system SHALL automatically adjust the USD balance within the trade ingestion transaction when a spot position is opened or closed, provided a balance row already exists for the account.

- **Opening** (spot buy, new or added-to position): deduct `quantity × price + fee` from balance.
- **Partial close** (spot sell, position remains open): credit the computed realised P&L (`(sell_price − avg_entry) × qty − fee`) to balance.
- **Full close** (spot sell, position fully closed): credit the computed realised P&L to balance.

When no balance row exists the adjustment SHALL be silently skipped.

#### Scenario: Spot buy deducts cost from balance
- **WHEN** a spot buy trade is ingested for an account with a USD balance of 10000, buying quantity 1 at price 5000 with fee 5
- **THEN** the balance SHALL become 4995 after ingestion

#### Scenario: Spot sell full close credits P&L
- **WHEN** a spot sell trade fully closes a position and the realised P&L is +500
- **THEN** the balance SHALL increase by 500

#### Scenario: Spot sell partial close credits P&L
- **WHEN** a spot sell trade partially closes a position with realised P&L of +200
- **THEN** the balance SHALL increase by 200

#### Scenario: Spot ingestion skips adjustment when no balance set
- **WHEN** a spot trade is ingested for an account that has no balance row
- **THEN** the trade and position SHALL be persisted normally and no balance row SHALL be created

### Requirement: Balance auto-adjustment on trade ingestion — futures
The system SHALL automatically adjust the USD balance within the trade ingestion transaction when a futures position is opened or closed, provided a balance row already exists for the account.

- **Opening** (new futures position): deduct margin amount from balance. When `margin` is present on the trade use it directly; otherwise derive margin as `cost_basis / leverage` using the trade's leverage (or default leverage of 2 if absent). If neither margin nor leverage is available the adjustment SHALL be skipped.
- **Partial close**: credit the computed realised P&L (already leverage- and fee-adjusted) to balance.
- **Full close**: credit the computed realised P&L to balance.

When no balance row exists the adjustment SHALL be silently skipped.

#### Scenario: Futures open deducts margin
- **WHEN** a futures buy trade opens a new position with margin 1000
- **THEN** the balance SHALL decrease by 1000

#### Scenario: Futures open deducts derived margin when margin field absent
- **WHEN** a futures buy trade opens a position with cost_basis 10000 and leverage 10 but no margin field
- **THEN** the balance SHALL decrease by 1000 (10000 / 10)

#### Scenario: Futures close credits realised P&L
- **WHEN** a futures closing trade results in realised P&L of +300
- **THEN** the balance SHALL increase by 300

#### Scenario: Futures open skips adjustment when margin unavailable
- **WHEN** a futures trade has no margin field and no leverage field
- **THEN** the position SHALL be opened normally and the balance SHALL not be adjusted

#### Scenario: Futures ingestion skips adjustment when no balance set
- **WHEN** a futures trade is ingested for an account that has no balance row
- **THEN** the trade and position SHALL be persisted normally and no balance row SHALL be created

### Requirement: Balance not adjusted during position rebuild
The system SHALL NOT adjust account balances when rebuilding positions from trade history via `RebuildPositions`. Position rebuild replays trades to reconstruct position state only; balance is managed independently and SHALL retain its current value through a rebuild.

#### Scenario: Rebuild does not alter balance
- **WHEN** `RebuildPositions` is triggered for an account with a balance of 8000
- **THEN** after the rebuild completes the balance SHALL still be 8000
