## Context

The trader engine binary today has two storage concerns:

1. **Trade + position persistence** — `internal/store` (`trades.go`, `positions.go`) writes every executed trade to `ledger_trades` and updates `ledger_positions` inside a single DB transaction via `InsertTradeAndUpdatePosition`.
2. **Engine position state** — `engine_positions.go` keeps a `engine_position_state` table as a durable risk-management cache (stop-loss, take-profit, trailing stop, peak price). The engine reads this on startup to restore in-flight positions and writes it on every state change.

Both concerns require a live Postgres connection via `DATABASE_URL`. This is incompatible with the tenant installer model, where the trader runs as a stateless Cloud Run container with no DB of its own — only NATS and a Signal ngn API key.

The platform already has `tenant_trades` and `tenant_positions` tables written by the spot-canvas-app `ledger-rest-ingest` service. The goal is to redirect the engine's trade/position write path there via the platform API, while keeping risk-management state (stop-loss, take-profit, trailing stop, peak price, daily P&L) durable and self-contained in the trader module using GCP Firestore.

## Goals / Non-Goals

**Goals:**
- Engine records trades by calling `POST /api/v1/trades` on the platform API instead of inserting into `ledger_trades`
- Engine reads open positions / portfolio state from `GET /api/v1/accounts/{id}/portfolio` on startup, instead of querying `ledger_positions`
- Engine reads account balance from `GET /api/v1/accounts` and updates it via `PUT /api/v1/accounts/{id}/balance` after each trade, instead of reading/writing `ledger_account_balances`
- Engine risk-management state (stop-loss, take-profit, trailing stop, peak price, hard stop, daily P&L) is stored in GCP Firestore — durable across restarts, owned entirely by the trader module
- `DATABASE_URL` / Cloud SQL is not required to start or run the engine
- `ledger_trades`, `ledger_positions`, `ledger_accounts`, `ledger_account_balances`, `ledger_orders`, `ledger_schema_migrations`, and `engine_position_state` become safe to drop

**Non-Goals:**
- Changing the signal pipeline, NATS subscription, strategy execution, or CLI commands
- Modifying the platform API (spot-canvas-app) schema or endpoints
- Supporting a hybrid mode where some accounts use DB and others use the API
- Migrating historical `ledger_*` data to `tenant_*` tables (out of scope; data is paper-mode test data)

## Decisions

### 1. Introduce a `EngineStore` interface; replace `*store.Repository` in the engine

**Decision:** Define a narrow `EngineStore` interface in `internal/engine` covering only the methods the engine actually calls. Implement it once as `APIEngineStore` (platform-API-backed). Wire the engine to the interface, not the concrete type.

**Why:** The current engine takes `*store.Repository` directly. Introducing an interface keeps engine logic unchanged while the concrete store is replaced. `store.Repository` continues to exist unchanged for `internal/api` and `internal/ingest` — the engine simply stops using it.

**Tenant ID resolution:** The engine currently resolves `tenant_id` by calling `store.NewUserRepository(pool).GetByAPIKey(ctx, key)` — a direct DB query against the `users` table. With no DB access this must move to `GET /auth/resolve` (already exists on the platform API), which returns `{"tenant_id": "..."}` for the authenticated Bearer token. This call is made once at engine startup.

**Methods the engine calls on the store (from audit of `internal/engine`):**
- `InsertTradeAndUpdatePosition(ctx, tenantID, trade)` — record a trade → `POST /api/v1/trades`
- `GetAccountBalance(ctx, tenantID, accountID, currency)` — read balance for position sizing → `GET /api/v1/accounts`
- `AdjustBalance(ctx, tenantID, accountID, currency, delta)` — update balance after a trade → `PUT /api/v1/accounts/{id}/balance`
- `GetAvgEntryPrice(ctx, tenantID, accountID, symbol, marketType)` — exit trade cost-basis → open position `avg_entry_price` from `GET /api/v1/accounts/{id}/portfolio`
- `CountOpenPositionStates(ctx, accountID)` — slot-limit check before opening → count of Firestore position state documents
- `ListOpenPositionsForAccount(ctx, accountID)` — startup conflict guard → `GET /api/v1/accounts/{id}/portfolio`
- `ListAccounts(ctx, tenantID)` — load managed accounts on startup → `GET /api/v1/accounts`
- `LoadPositionStates(ctx, accountID)` — startup: load risk state → Firestore collection query
- `InsertPositionState(ctx, tenantID, state)` — open position: persist risk state → Firestore document write
- `UpdatePositionState(ctx, tenantID, state)` — update trailing stop / peak price → Firestore document update
- `DeletePositionState(ctx, tenantID, symbol, marketType, accountID)` — close position → Firestore document delete
- `DailyRealizedPnL(ctx, accountID)` — daily loss limit → Firestore document (daily P&L accumulator, keyed by account + UTC date)

**Alternative considered:** Refactor `store.Repository` to accept a pluggable HTTP client. Rejected — it mixes DB and API concerns in one struct and the abstraction boundary is already clear.

---

### 2. `APIEngineStore`: trade writes → `POST /api/v1/trades`

**Decision:** `InsertTradeAndUpdatePosition` on the `APIEngineStore` calls `POST /api/v1/trades` with the trade payload. The platform API is responsible for recording the trade and updating `tenant_positions`. The engine does not attempt its own position arithmetic — that becomes the platform's concern.

**Idempotency:** The trade ID is included in the payload. The platform API is already idempotent on `trade_id` (ON CONFLICT DO NOTHING). If the engine retries after a timeout the duplicate is silently ignored — same semantics as the current DB path.

**Return value:** The method returns `(inserted bool, error)`. For the API store, `inserted = true` if the HTTP call returns 201 or 200, `false` if it returns 409 (duplicate).

---

### 3. Risk-management state → GCP Firestore

**Decision:** Engine risk-management state — stop-loss, take-profit, hard stop, trailing stop, peak price, leverage, strategy, granularity, opened-at — is stored in GCP Firestore (Native mode) and owned entirely by the trader module. The platform API is not involved.

**Firestore document layout:**
```
engine-state/
  {accountID}/
    positions/
      {symbol}-{marketType}   ← one document per open position
    daily-pnl/
      {accountID}-{YYYY-MM-DD} ← one document per account per day
```

**Startup:** `LoadPositionStates` queries the `positions` sub-collection for the account and reconstructs the in-memory risk map used during normal operation. The Firestore documents are the durable source of truth; the in-memory map is a runtime cache.

**Writes:** `InsertPositionState`, `UpdatePositionState`, and `DeletePositionState` write through to Firestore synchronously. Firestore write latency is ~10ms in the same GCP region — acceptable on the trade execution path.

**Why Firestore over a sidecar Redis/Valkey:** No sidecar configuration or memory allocation needed. Firestore is serverless, survives container restarts naturally, and requires only an IAM role (`roles/datastore.user`) on the trader service account. The tenant installer enables the Firestore API and grants the role as part of setup.

**Why not ephemeral disk / SQLite:** Cloud Run disk is lost on every restart. Mounting a GCS FUSE volume adds operational complexity and latency comparable to Firestore with less tooling support.

**Risk management stays in the trader module:** Open position fields (entry price, stop-loss, take-profit) come from Firestore on startup — the engine does not rely on the platform API for any risk-management decisions. This keeps the safety guarantees self-contained.

---

### 4. Startup position hydration → `GET /api/v1/accounts/{id}/portfolio`

**Decision:** On engine startup, `LoadPositionStates` (or a new `HydrateFromPortfolio` method) calls `GET /api/v1/accounts/{id}/portfolio`, maps open positions to `EnginePositionState` entries using stored `stop_loss`, `take_profit`, `leverage`, and `entry_price` fields, and seeds the in-memory map.

**Risk state recovery:** All risk fields — `stop_loss`, `take_profit`, `hard_stop`, `trailing_stop`, `peak_price`, `leverage`, `strategy`, `granularity`, `opened_at` — are persisted in Firestore (decision 3). On startup `LoadPositionStates` reads the Firestore `positions` sub-collection and fully reconstructs the in-memory risk map. No risk state is lost across restarts.

---

### 5. Account balance → `GET /api/v1/accounts` + `PUT /api/v1/accounts/{id}/balance`

**Decision:** The engine reads the current available balance via `GET /api/v1/accounts` (which now includes `balance` per account) at position-sizing time, and writes the updated balance via `PUT /api/v1/accounts/{id}/balance` after every balance-affecting trade event (open, close, partial close).

**Why not in-memory?** Cloud Run instances restart unpredictably. An in-memory balance resets to `PORTFOLIO_SIZE_USD` on every cold start, causing the engine to oversize positions immediately after a restart — unacceptable when real capital is at stake.

**Initialisation:** On first boot, if the platform returns no balance for the account, the engine calls `PUT /api/v1/accounts/{id}/balance` with `PORTFOLIO_SIZE_USD` to seed it. Subsequent restarts read the persisted balance and do not reset it.

**Sequencing:** The engine calls `PUT /api/v1/accounts/{id}/balance` after a successful `POST /api/v1/trades`. If the balance update fails (network error), the engine logs a warning and continues — the trade is already recorded idempotently and the balance can be reconciled from trade history. This is the same eventual-consistency trade-off as the current `AdjustBalance` (which is inside the same DB transaction but that transaction can also fail after the trade is written in edge cases).

**`ledger_account_balances` removal:** The platform's `tenant_accounts` table now carries `balance` + `balance_updated_at` (migration 024). `ledger_account_balances` is no longer the source of truth and is included in the tables safe to drop.

---

### 6. `DailyRealizedPnL` → Firestore daily accumulator

**Decision:** Daily realized P&L is stored as a Firestore document keyed by `{accountID}-{YYYY-MM-DD}` under `engine-state/{accountID}/daily-pnl/`. The engine increments the document after each closed trade and reads it on the `isDailyLossLimitReached` check. The document is naturally scoped to the current UTC day — no explicit reset needed.

**Startup behaviour:** On engine start, the engine reads today's document. If it doesn't exist, daily P&L is 0. If the engine restarts mid-day the accumulated P&L is correctly restored.

**Why not platform API?** The platform portfolio endpoint returns `total_realized_pnl` (all-time), not today's P&L. Adding a date-filtered endpoint is out of scope for this change. Daily P&L is a risk-management concern — keeping it in Firestore alongside position state is consistent with the principle that risk management is owned by the trader module.

---

### 7. No `DBEngineStore` — one implementation only

**Decision:** There is no DB-backed fallback for the engine store. `APIEngineStore` is the only implementation of `EngineStore`. `store.Repository` continues to exist and serve `internal/api` (the REST API server) and `internal/ingest` (the NATS trade consumer) — both are platform-side concerns that run where a DB exists. The engine is entirely decoupled from it.

**Local development:** Developers run the engine with `SN_API_KEY` + `TRADER_API_URL` pointing at the staging platform API — exactly the same as production. No special DB-backed mode is needed.

**Why not keep a fallback?** Keeping `DBEngineStore` would mean two code paths, two test surfaces, and an ongoing risk of drift between them. The purpose of this change is to guarantee the engine has no DB dependency — an optional DB path undermines that guarantee.

---

### 8. Configuration: `SN_API_KEY` and `TRADER_API_URL` required; `DATABASE_URL` removed from engine

**Decision:** The engine binary reads `SN_API_KEY` (already present in the tenant installer) and `TRADER_API_URL` (defaults to `https://signalngn-api-potbdcvufa-ew.a.run.app`) from the environment. If `SN_API_KEY` is absent the engine fails fast at startup with a clear error. `DATABASE_URL` is removed from the engine's required config entirely — `cmd/traderd/main.go` no longer passes a DB pool to the engine.

---

### 9. `ledger_*` table removal is a separate follow-up migration

**Decision:** This change removes the engine's DB writes. The `ledger_*` tables are **not dropped in this change**. A follow-up DB migration (in spot-canvas-app or a standalone script) drops them once the new engine is confirmed stable in production.

**Rationale:** Decouples deploy risk. If the new engine has a bug the tables still exist and can be re-enabled. Tables cost nothing to keep short-term.

## Risks / Trade-offs

- **Platform API latency on the trade write path** → Every executed trade now makes two HTTP calls (trade write + balance update) before the engine acknowledges success. Mitigation: the platform API is in the same GCP region; p99 latency is ~50ms per call. The engine should set a per-request timeout (5s). Trade write is idempotent; balance update failure is logged and tolerated (see decision 5).
- **Balance drift on balance update failure** → If `PUT /api/v1/accounts/{id}/balance` fails after a successful trade write, the stored balance will be stale until the next successful update. Mitigation: the next trade's balance read will use the stale value — conservative for sizing, not dangerous. Balance reconciles automatically on the next successful write.
- **Firestore write latency on the trade path** → Each position state change makes a Firestore write (~10ms in-region). For the daily P&L accumulator, Firestore transactions ensure no lost increments under concurrent writes. Mitigation: use Firestore's atomic increment (`FieldTransform`) for P&L updates rather than read-modify-write.
- **Platform API down → engine cannot record trades** → If `POST /api/v1/trades` fails the engine must not double-execute on retry. Mitigation: idempotent trade IDs on the API side (already in place). Engine logs the failure and skips the trade rather than blocking the signal consumer.


## Migration Plan

1. Move `PlatformClient` from `cmd/trader/` to `internal/platform/` so the engine can import it
2. Implement `EngineStore` interface + `APIEngineStore` in `internal/engine/apistore.go`, backed by platform API (trades, balance, accounts) and Firestore (position risk state, daily P&L)
3. Update `internal/engine/engine.go` to accept `EngineStore` instead of `*store.Repository`
4. Update `cmd/traderd/main.go` to construct `APIEngineStore` and pass it to the engine; remove the DB pool wiring for the engine (DB pool is still created for `internal/api` and `internal/ingest`); replace `store.NewUserRepository` tenant lookup with `GET /auth/resolve`
5. Remove `DATABASE_URL` from the engine's required config and fail fast if `SN_API_KEY` is absent
6. Update `scripts/install-tenant.sh`: enable `firestore.googleapis.com` API, create a Firestore Native mode database, grant `roles/datastore.user` to the trader service account, add `FIRESTORE_PROJECT_ID` env var to the Cloud Run deploy command
7. Update `docs/tenant-install.md` to document the Firestore setup step
8. Deploy to production; verify trades appear in `tenant_trades` and position state persists across restarts in Firestore
9. Follow-up: drop `ledger_*` and `engine_position_state` tables in a DB migration

**Rollback:** Re-deploy the previous image (which has the DB store). No data migration needed — the `ledger_*` tables are untouched by this change.

## Open Questions

- Does `POST /api/v1/trades` accept all fields the engine currently records (strategy, confidence, stop_loss, take_profit, entry_reason, exit_reason, leverage, margin, funding_fee)? Needs verification against the `ledger-rest-ingest` API schema before implementation.
- What is the exact shape of `GET /api/v1/accounts/{id}/portfolio` — does it return enough position fields (entry_price, stop_loss, take_profit, leverage, side) for the engine to reconstruct `EnginePositionState`?
- Does `GET /api/v1/accounts` return balance in the same response, or does the engine need a separate call to `GET /api/v1/accounts/{id}/stats`?
