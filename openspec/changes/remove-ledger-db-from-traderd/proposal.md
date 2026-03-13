## Why

Following `trader-platform-api-only`, the engine no longer writes to `ledger_*` tables — all trade and position data now lives in the platform API (spot-canvas-app). The `traderd` binary and trader CLI still read from the now-stale `ledger_*` tables via a local Postgres connection, making them inoperative for any data written after the engine migration. The database dependency must be removed from `traderd` entirely.

## What Changes

- **Remove `internal/ingest`** — the NATS trade consumer is no longer needed; trade ingestion is now handled exclusively by the platform API
- **Remove the NATS connection from `cmd/traderd/main.go`** — the only consumer of it was `internal/ingest`; the engine's NGS connection is self-contained and unaffected
- **Remove `store.Repository` from `traderd`** — no DB pool, no migrations, no Cloud SQL wiring in `main.go`
- **Redirect CLI commands to call the platform API directly** — `trader trades`, `trader positions`, `trader portfolio`, `trader accounts`, `trader balance`, `trader import`, `trader delete` all switch from the local ledger client to `PlatformClient` (`internal/platform`)
- **Slim down `internal/api`** — remove all `ledger_*`-backed handlers; keep only `/health`, `/auth/resolve`, and `/accounts/{id}/trades/stream` (SSE for `trader watch`)
- **`trader watch` is unchanged** — it connects to traderd's SSE endpoint which is fed by the engine's in-process publisher; no NATS involvement

## Capabilities

### New Capabilities
- `cli-platform-direct`: CLI commands (`trades`, `positions`, `portfolio`, `accounts`, `balance`, `import`, `delete`) call the platform API directly via `PlatformClient` instead of routing through the traderd REST API

### Modified Capabilities
- `ledger-cli`: CLI commands that previously read from the traderd REST API now read from the platform API; the observable behaviour (fields returned, pagination, filters) must stay compatible
- `rest-api`: traderd's REST API is reduced to health, auth-resolve, and the SSE stream endpoint; all other endpoints are removed
- `trade-ingestion`: the NATS-based ingestion consumer is removed; trade ingestion now happens exclusively via the platform API path established in `trader-platform-api-only`

## Impact

- **Removed packages**: `internal/ingest` deleted entirely
- **Removed dependency**: `store.Repository` removed from `cmd/traderd/main.go`; `DATABASE_URL` / Cloud SQL not required to start `traderd`
- **Affected CLI commands**: all query/mutate commands in `cmd/trader/` switch from `Client` (ledger) to `PlatformClient`; `trader watch` unchanged
- **`internal/api`**: most handlers deleted; `Server` struct no longer holds `*store.Repository`
- **`internal/store`**: package deleted entirely; a standalone SQL migration drops `ledger_trades`, `ledger_positions`, `ledger_accounts`, `ledger_account_balances`, `ledger_orders`, `ledger_schema_migrations`, and `engine_position_state` from the platform DB
- **No change** to the engine, the risk loop, Firestore wiring, or platform client methods
