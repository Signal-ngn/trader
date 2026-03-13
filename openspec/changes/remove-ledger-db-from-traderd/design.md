## Context

`trader-platform-api-only` moved the engine's trade writes to the platform API and risk state to Firestore. The `traderd` binary still opens a Postgres connection on startup, runs migrations, and serves a REST API whose handlers read from `ledger_*` tables. Since the engine no longer writes to those tables, the REST API returns stale or empty data for any trades executed after the migration.

Two things remain coupled to the DB:
1. **`internal/ingest`** — NATS JetStream consumer that persists externally-ingested trades to `ledger_trades`. Not needed any more; the platform API is the single trade store.
2. **`internal/api`** — REST API server whose handlers read `ledger_trades`, `ledger_positions`, `ledger_account_balances`, etc. via `store.Repository`.

The `trader` CLI currently calls the traderd REST API for all data commands. Redirecting CLI commands to call the platform API directly (using the already-present `internal/platform.PlatformClient`) removes the need for traderd to proxy or serve that data at all.

## Goals / Non-Goals

**Goals:**
- Remove `internal/ingest` and all NATS wiring from `cmd/traderd/main.go`
- Remove `store.Repository` and the DB connection from `cmd/traderd/main.go`
- Slim `internal/api` to three endpoints: `/health`, `/auth/resolve`, `/accounts/{id}/trades/stream`
- Redirect all CLI query/mutate commands to call the platform API directly
- `trader watch` continues to work (traderd SSE endpoint is unchanged)
- `traderd` starts cleanly without `DATABASE_URL` or any NATS config

**Non-Goals:**
- Keeping `ledger_*` tables — they are dropped in this change
- Keeping `internal/store` — the package is deleted in this change
- Changing the platform API schema or adding new platform endpoints
- Modifying the engine, risk loop, or Firestore wiring

## Decisions

### 1. CLI calls platform API directly — no forwarding layer in traderd

**Decision:** CLI data commands (`trades`, `positions`, `portfolio`, `accounts`, `balance`, `import`, `delete`) switch from the local `Client` (ledger URL) to `PlatformClient` (`internal/platform`) with the same API key already used for platform calls.

**Why:** A forwarding layer in traderd would add a hop, a failure point, and code to maintain with no benefit. The CLI already has `PlatformClient` and the `api_url` config key. Direct calls are simpler and remove traderd as a required dependency for data reads.

**Alternative considered:** Keep traderd as a proxy and have handlers call `PlatformClient` internally. Rejected — unnecessary complexity; the CLI is already capable of calling the platform API directly.

---

### 2. traderd HTTP server is kept but gutted — not removed

**Decision:** `internal/api` keeps its `Server` struct and router, but removes `*store.Repository` from the struct. Only three routes survive: `/health`, `/auth/resolve`, and `/accounts/{id}/trades/stream`.

**Why:** `traderd` must still run as a long-lived process to host the SSE stream that `trader watch` subscribes to. Keeping a minimal HTTP server is the natural container for that endpoint. Removing the package entirely would require moving SSE to a new home.

**`/health`:** Simplified to return `{"status": "ok"}` unconditionally — no DB to ping. The existing spec requirement for a DB-aware health check is superseded: traderd has no DB.

**`/auth/resolve`:** Already a pass-through to the platform API; unchanged.

**`/accounts/{id}/trades/stream`:** SSE endpoint fed by the engine's in-process publisher. No DB or NATS involvement; unchanged.

---

### 3. NATS connection removed from traderd entirely

**Decision:** `cmd/traderd/main.go` no longer creates a NATS connection. `internal/ingest` is deleted.

**Why:** The only consumer of traderd's NATS connection was `ingest.NewConsumer`. The engine's Synadia NGS connection is self-contained within the engine goroutine and is unaffected. The `trader watch` SSE stream is fed by the engine's in-process `publisher.Publish` call — no NATS involved on the traderd side.

**`trader watch` unaffected:** The watch flow is: engine fill → `e.publisher.Publish(accountID, trade)` → in-process SSE registry → HTTP SSE response to CLI. Zero NATS involvement. Confirmed by tracing `srv.StreamRegistry()` through `internal/api/stream.go`.

---

### 4. CLI commands use PlatformClient for all platform data

**Decision:** Each CLI command file (`cmd_trades.go`, `cmd_positions.go`, `cmd_portfolio.go`, `cmd_accounts.go`) that previously called `newClient()` (ledger) is updated to call `newPlatformClient()` instead, mapping to the equivalent platform API endpoints.

**Platform API endpoint mapping:**

| CLI command | Old traderd endpoint | New platform API endpoint |
|---|---|---|
| `trader trades list` | `GET /api/v1/accounts/{id}/trades` | `GET /api/v1/accounts/{id}/trades` |
| `trader positions list` | `GET /api/v1/accounts/{id}/positions` | `GET /api/v1/accounts/{id}/positions` |
| `trader portfolio` | `GET /api/v1/accounts/{id}/portfolio` | `GET /api/v1/accounts/{id}/portfolio` |
| `trader accounts list` | `GET /api/v1/accounts` | `GET /api/v1/accounts` |
| `trader balance get` | `GET /api/v1/accounts/{id}/balance` | `GET /api/v1/accounts/{id}/balance` |
| `trader balance set` | `PUT /api/v1/accounts/{id}/balance` | `PUT /api/v1/accounts/{id}/balance` |
| `trader import` | `POST /import` (traderd) | `POST /api/v1/trades` (bulk or per-trade) |
| `trader trades delete` | `DELETE /trades/{id}` (traderd) | `DELETE /api/v1/trades/{id}` |
| `trader accounts stats` | `GET /api/v1/accounts/{id}/stats` | `GET /api/v1/accounts/{id}/stats` |

The URL paths are largely identical between traderd and the platform API — the main change is the base URL (from `trader_url` to `api_url` in config).

**`trader watch`:** Still calls traderd's SSE endpoint at `trader_url`. Unchanged.

---

### 5. `internal/store` deleted and `ledger_*` tables dropped

**Decision:** Delete the entire `internal/store` package and add a DB migration to drop `ledger_trades`, `ledger_positions`, `ledger_accounts`, `ledger_account_balances`, `ledger_orders`, `ledger_schema_migrations`, and `engine_position_state` from the platform DB.

**Why now?** After removing `internal/ingest` and gutting `internal/api`, nothing in the binary import chain references `internal/store`. Keeping it as dead code creates confusion about whether those tables are still in use. Dropping them definitively closes the `trader-platform-api-only` migration.

**Migration:** A new SQL migration file is added to `internal/store/migrations/` (e.g. `NNN_drop_ledger_tables.sql`) that DROPs all `ledger_*` tables and `engine_position_state`. The migration runs on the next `traderd` deploy — but since `traderd` no longer connects to the DB, the migration must be run as a standalone step (e.g. `psql` or a one-off Cloud Run job against the platform DB) rather than auto-applied at startup. The migration SQL file is kept in the repo for auditability even after `internal/store` is deleted.

**Test cleanup:** Any test files under `internal/store/`, `internal/api/`, or `internal/ingest/` that reference `store.Repository` are deleted along with the package.

## Risks / Trade-offs

- **CLI `trader_url` config becomes only needed for `watch`** → Users with stale `trader_url` pointing at an unreachable traderd will see errors only on `trader watch`, not on data commands. Acceptable — the error message is clear.
- **Platform API pagination differences** → The platform API may return pagination tokens or page sizes that differ from traderd's. The CLI output formatting code may need minor updates for field name differences. Low risk — the fields are the same by design.
- **`trader import` mapping** → The current import command POSTs a bulk CSV/JSON to traderd's `/import` endpoint. The platform API may not have a direct bulk import equivalent — the importer may need to loop and POST individual trades. Needs verification during implementation.
- **`/health` no longer checks DB** → External health check probes (Cloud Run, uptime monitors) will now always get 200 from traderd. This is correct — traderd has no DB to check. If a DB health check is needed it belongs on the platform API, not traderd.

## Migration Plan

1. Delete `internal/ingest` package and all its test files
2. Update `cmd/traderd/main.go`: remove NATS wiring, DB pool, migrations, `store.Repository`, `userRepo`
3. Update `internal/api`: remove `*store.Repository` from `Server`, delete all handlers except health/auth-resolve/stream, simplify health to unconditional 200, delete all associated test files that use `store.Repository`
4. Update CLI command files to call `newPlatformClient()` instead of `newClient()` for data commands
5. Delete `internal/store` package entirely (all `.go` files and test files)
6. Add `NNN_drop_ledger_tables.sql` to the repo root (or a `migrations/` folder) for manual execution against the platform DB
7. Build: `go build ./...` — must succeed with no DB or NATS env vars
8. Run the drop migration manually against the platform DB: `psql $DATABASE_URL -f NNN_drop_ledger_tables.sql`
9. Smoke test: `trader trades list`, `trader portfolio`, `trader watch`

**Rollback:** Re-deploy the previous image before the migration runs. If the drop migration has already been applied, restoring the tables requires a restore from backup — confirm timing before applying.
