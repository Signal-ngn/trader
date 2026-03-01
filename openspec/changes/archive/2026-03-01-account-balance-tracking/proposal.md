## Why

Trading agents need to track available capital per account so that position sizing can reflect real purchasing power and the ledger can remain the single source of truth for account state. Without balance tracking, the ledger records trades but has no awareness of how much capital is available, making it impossible to audit fund utilisation.

## What Changes

- **NEW** `ledger_account_balances` table stores a cash balance per (tenant, account, currency).
- **NEW** REST endpoints to set and query account balance (`PUT /api/v1/accounts/{accountId}/balance`, `GET /api/v1/accounts/{accountId}/balance`).
- **MODIFIED** Trade ingestion: opening a position deducts the cost from the balance; closing a position credits the realised P&L back. Both happen within the same DB transaction as the position update. Balance adjustment is skipped (not an error) when no balance row exists for the account.
- **MODIFIED** Portfolio summary response: include current balance alongside open positions.
- **MODIFIED** Account stats response: include current balance.
- **NEW** `ledger accounts balance set <account-id> <amount> [--currency USD]` CLI subcommand — for setting initial balance and manual corrections.
- **NEW** `ledger accounts balance get <account-id> [--currency USD]` CLI subcommand (nested under `accounts`).

## Capabilities

### New Capabilities
- `account-balance`: Per-account cash balance store with REST API to set/get balance, and automatic deduction/credit during trade ingestion.

### Modified Capabilities
- `portfolio-tracking`: Portfolio summary and account stats responses now include the current balance field.
- `trade-ingestion`: Trade processing now adjusts account balance within the same transaction as the position update — deduct cost on open, credit realised P&L on close. Adjustment is a no-op when no balance row exists.
- `rest-api`: Two new endpoints (`PUT` and `GET` `/api/v1/accounts/{accountId}/balance`); portfolio and stats responses gain a `balance` field.
- `ledger-cli`: New `ledger accounts balance set` and `ledger accounts balance get` subcommands under the existing `accounts` command group.

## Impact

- **DB**: New migration adding `ledger_account_balances` table (tenant_id, account_id, currency, amount, updated_at).
- **`internal/store`**: New `SetAccountBalance`, `GetAccountBalance`, and `AdjustBalance` methods on `Repository`. `AdjustBalance` applies a signed delta atomically (used by ingestion).
- **`internal/store/positions.go`**: `upsertSpotPosition` and `upsertFuturesPosition` extended to call `AdjustBalance` within the existing transaction — deduct cost on open, credit realised P&L on full or partial close.
- **`internal/api/handlers.go`**: Two new handler functions; `handlePortfolioSummary` and `handleAccountStats` fetch and attach balance.
- **`internal/api/router.go`**: Register new routes (`PUT`/`GET` `/api/v1/accounts/{accountId}/balance`).
- **`internal/domain/types.go`**: New `AccountBalance` domain struct.
- **`cmd/ledger/cmd_accounts.go`**: New `accounts balance` sub-command group with `set` and `get` child commands.
- No new external dependencies required.
