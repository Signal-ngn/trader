## 1. Database Migration

- [x] 1.1 Create `internal/store/migrations/007_add_account_balances.up.sql` — `ledger_account_balances` table with `(tenant_id, account_id, currency)` primary key, `amount DOUBLE PRECISION NOT NULL DEFAULT 0`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`

## 2. Domain Types

- [x] 2.1 Add `AccountBalance` struct to `internal/domain/types.go` with fields `AccountID string`, `Currency string`, `Amount float64`

## 3. Store Methods

- [x] 3.1 Add `SetAccountBalance(ctx, tenantID, accountID, currency, amount)` to `internal/store/accounts.go` — upsert row, update `updated_at`
- [x] 3.2 Add `GetAccountBalance(ctx, tenantID, accountID, currency) (*float64, error)` — return nil when no row exists
- [x] 3.3 Add `AdjustBalance(ctx, tx, tenantID, accountID, currency, delta)` — atomic `UPDATE amount = amount + delta`, no-op when row absent

## 4. Balance Adjustment in Trade Ingestion

- [x] 4.1 In `upsertSpotPosition` (new position, buy): call `AdjustBalance` with delta `-(quantity × price + fee)` after the INSERT
- [x] 4.2 In `upsertSpotPosition` (add to existing, buy): call `AdjustBalance` with delta `-(quantity × price + fee)` after the UPDATE
- [x] 4.3 In `upsertSpotPosition` (partial close, sell): call `AdjustBalance` with delta `+realizedPnL` after the UPDATE
- [x] 4.4 In `upsertSpotPosition` (full close, sell): call `AdjustBalance` with delta `+realizedPnL` after the UPDATE
- [x] 4.5 In `upsertFuturesPosition` (new position): call `AdjustBalance` with delta `-margin` (use `trade.Margin` if set, else `costBasis/leverage`, else skip) after the INSERT
- [x] 4.6 In `upsertFuturesPosition` (partial close): call `AdjustBalance` with delta `+realizedPnL` after the UPDATE
- [x] 4.7 In `upsertFuturesPosition` (full close): call `AdjustBalance` with delta `+realizedPnL` after the UPDATE
- [x] 4.8 Verify `RebuildPositions` does NOT call `AdjustBalance` — confirm the rebuild path uses `UpsertPosition` without triggering balance changes (introduce a bool param or separate method if needed)

## 5. Portfolio Summary — Balance Field

- [x] 5.1 Add `Balance *float64 \`json:"balance,omitempty"\`` to `PortfolioSummary` struct in `internal/store/positions.go`
- [x] 5.2 In `GetPortfolioSummary`, call `GetAccountBalance` for USD and set `Balance` when non-nil

## 6. Account Stats — Balance Field

- [x] 6.1 Add `Balance *float64 \`json:"balance,omitempty"\`` to `AccountStats` struct in `internal/store/accounts.go`
- [x] 6.2 In `GetAccountStats`, call `GetAccountBalance` for USD and set `Balance` when non-nil

## 7. REST API Handlers and Routes

- [x] 7.1 Add `handleSetBalance` to `internal/api/handlers.go` — parse body, call `SetAccountBalance`, return 200 with `AccountBalance` JSON; return 400 on missing `amount`
- [x] 7.2 Add `handleGetBalance` to `internal/api/handlers.go` — read `?currency` param (default `USD`), call `GetAccountBalance`, return 200 or 404
- [x] 7.3 Register `PUT /api/v1/accounts/{accountId}/balance` and `GET /api/v1/accounts/{accountId}/balance` in `internal/api/router.go`

## 8. CLI — accounts balance subcommands

- [x] 8.1 Add `accountsBalanceCmd` cobra command group (`ledger accounts balance`) in `cmd/ledger/cmd_accounts.go`
- [x] 8.2 Add `accountsBalanceSetCmd` (`set <account-id> <amount>`) — parse amount as float64, send PUT, print confirmation table or raw JSON with `--json`; `--currency` flag defaulting to `USD`
- [x] 8.3 Add `accountsBalanceGetCmd` (`get <account-id>`) — send GET, print table or raw JSON with `--json`; print `no balance set for <account-id>` and exit non-zero on 404; `--currency` flag defaulting to `USD`
- [x] 8.4 Wire `accountsBalanceCmd` into `accountsCmd.AddCommand` and add `set`/`get` as its children

## 9. CLI — accounts show balance display

- [x] 9.1 Add `Balance *float64 \`json:"balance,omitempty"\`` to the `AccountStats` struct in `cmd/ledger/cmd_accounts.go`
- [x] 9.2 In `accountsShowCmd` table output, append a `Balance` row when non-nil; display `not set` when nil
