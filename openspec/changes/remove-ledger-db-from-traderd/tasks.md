## 1. Remove internal/ingest

- [x] 1.1 Delete the entire `internal/ingest/` directory (consumer.go, event.go, and all test files)
- [x] 2.2 Remove `ingest.ConnectNATS`, `ingest.NewConsumer`, and the NATS goroutine from `cmd/traderd/main.go`
- [x] 1.3 Remove NATS-related imports (`github.com/nats-io/nats.go`) from `cmd/traderd/main.go`

## 2. Remove DB from traderd

- [x] 2.1 Remove `store.NewRepository`, `repo.Ping`, `store.RunMigrationsWithReport`, `repo.RebuildAllPositions`, and `store.NewUserRepository` from `cmd/traderd/main.go`
- [x] 2.2 Remove DB-related imports (`internal/store`, `jackc/pgx`) from `cmd/traderd/main.go`
- [x] 2.3 Update `api.NewServer` call to remove `repo` and `userRepo` arguments
- [x] 2.4 Verify `cmd/traderd/main.go` compiles cleanly with no DB or NATS env vars set

## 3. Slim internal/api

- [x] 3.1 Remove `*store.Repository` and `*store.UserRepository` fields from the `Server` struct in `internal/api/router.go`; update `NewServer` signature accordingly
- [x] 3.2 Delete all handlers that read from `ledger_*`: `handleListAccounts`, `handleAccountStats`, `handlePortfolioSummary`, `handleListPositions`, `handleListTrades`, `handleListOrders`, `handleSetBalance`, `handleGetBalance`, `handleDeleteTrade`, `handleImportTrades`
- [x] 3.3 Remove the corresponding routes from the router in `internal/api/router.go`
- [x] 3.4 Simplify `handleHealth` to return `{"status": "ok"}` unconditionally (no DB ping)
- [x] 3.5 Delete `internal/api/import.go` and `internal/api/import_test.go`
- [x] 3.6 Delete or clean up `internal/api/handlers_test.go`, `internal/api/api_integration_test.go`, `internal/api/metadata_integration_test.go`, and any other test files that reference `store.Repository`
- [x] 3.7 Verify `internal/api` builds cleanly and the SSE stream endpoint and auth-resolve endpoint are intact

## 4. Delete internal/store

- [x] 4.1 Delete the entire `internal/store/` directory (all `.go` files and test files)
- [x] 4.2 Run `go build ./...` and resolve any remaining import errors caused by the deletion
- [x] 4.3 Confirm no binary-linked package imports `internal/store`

## 5. Redirect CLI commands to platform API

- [x] 5.1 Update `cmd/trader/cmd_accounts.go` to use `newPlatformClient()` for accounts list and stats commands
- [x] 5.2 Update `cmd/trader/cmd_trades.go` to use `newPlatformClient()` for trades list and delete commands
- [x] 5.3 Update `cmd/trader/cmd_positions.go` to use `newPlatformClient()` for positions list command
- [x] 5.4 Update `cmd/trader/cmd_portfolio.go` to use `newPlatformClient()` for portfolio command
- [x] 5.5 Update balance commands (`cmd_accounts.go` or equivalent) to use `newPlatformClient()` for balance get/set
- [x] 5.6 Update `cmd/trader/cmd_import.go` to POST trades individually to `POST {api_url}/api/v1/trades` via `PlatformClient.SubmitTrade`, skipping 409s, and printing a submitted/skipped/failed summary
- [x] 5.7 Confirm `trader watch` still uses `newClient()` and `trader_url` — no change needed

## 6. Drop ledger_* tables migration

- [x] 6.1 Create `migrations/drop_ledger_tables.sql` at the repo root with DROP TABLE statements for `ledger_trades`, `ledger_positions`, `ledger_accounts`, `ledger_account_balances`, `ledger_orders`, `ledger_schema_migrations`, and `engine_position_state`
- [x] 6.2 Add a `-- Run manually against the platform DB after deploying this version` comment header to the migration file

## 7. Build and verify

- [x] 7.1 Run `go build ./...` — must succeed
- [x] 7.2 Run `go test ./...` — all remaining tests must pass
- [x] 7.3 Verify `traderd` starts without `DATABASE_URL`, `NATS_URLS`, or `NATS_CREDS_FILE` set
- [x] 7.4 Smoke test: `trader accounts list`, `trader trades list`, `trader portfolio`, `trader watch`
