## Context

A crypto trading bot publishes trades to NATS. We need a Go service that subscribes to these trade events, maintains a ledger of all trades and positions, and serves portfolio data via a REST API. This is a greenfield service — no existing code to migrate.

The bot currently tracks positions itself and produces an S3-hosted dashboard page. After this service is live, the bot will offload position tracking entirely and the dashboard will pull data from this service's REST API instead.

The service must handle multiple trading accounts (live, paper), spot trading, and leveraged futures. It must also capture enough data to support Finnish tax reporting in a later phase.

This service is part of the **spot-canvas ecosystem** and shares infrastructure with `spot-canvas-app`, which already runs on GCP Cloud Run with Cloud SQL (PostgreSQL) and NATS. The ledger will share the same Cloud SQL database instance and follow the same deployment patterns.

## Goals / Non-Goals

**Goals:**
- Go service deployed to Cloud Run, sharing the existing Cloud SQL PostgreSQL database
- Consistent patterns with spot-canvas-app (pgx/v5, chi/v5, zerolog, NATS client, config structure)
- Reliable trade ingestion from NATS with no data loss
- Accurate portfolio state derived from trade history
- Clean REST API for external consumers (dashboard, tooling)
- Data model that captures all fields needed for future tax reporting
- Support for multiple accounts, spot, and leveraged futures

**Non-Goals:**
- Tax report generation (later phase — we only store the data now)
- UI, HTML views, or dashboard rendering
- Real-time price feeds or market data integration
- Trade execution or order placement
- High-frequency trading support (this is daily/swing trading)
- Separate database instance — we share spot-canvas's Cloud SQL

## Decisions

### 1. Shared Cloud SQL PostgreSQL database

**Choice:** Use the existing Cloud SQL PostgreSQL instance (`spot_canvas` database, `spot` user) that spot-canvas-app already uses.

**Rationale:** The ecosystem already has a Cloud SQL instance provisioned and configured. Sharing it avoids extra cost and operational overhead. The ledger tables will live alongside the existing candle/trading_pairs tables. The workload is low-volume (daily/swing trades) and won't impact the existing service.

**Alternatives considered:**
- Separate Cloud SQL instance: Unnecessary cost and complexity for low-volume writes
- SQLite: Incompatible with Cloud Run (ephemeral filesystem) and doesn't allow sharing with spot-canvas-app

### 2. pgx/v5 for database access

**Choice:** Use `jackc/pgx/v5` with connection pooling (`pgxpool`), matching spot-canvas-app.

**Rationale:** Consistency with the existing codebase. pgx is the most performant PostgreSQL driver for Go and is already a proven dependency in the ecosystem.

### 3. Shared NATS instance with JetStream

**Choice:** Use the same NATS instance as spot-canvas-app with JetStream durable consumers for reliable message delivery. Follow the identical NATS configuration pattern (NATS_URLS, NATS_CREDS_FILE, NATS_CREDS env vars).

**Rationale:** The NATS instance is already running and shared across the spot-canvas ecosystem. JetStream provides at-least-once delivery with durable consumers, ensuring no trades are lost if the service restarts. The service will use idempotent processing (dedup by trade ID) to handle redeliveries safely. Existing spot-canvas-app subjects use the `candles.` prefix (e.g., `candles.coinbase.BTC-USD.ONE_HOUR`); the ledger will use a `ledger.` prefix to coexist cleanly.

**Alternatives considered:**
- Separate NATS instance: Unnecessary — the shared instance handles both workloads easily
- Core NATS (no JetStream): No persistence or replay — trades published while the service is down would be lost

### 4. Subject hierarchy for NATS messages

**Choice:** `ledger.trades.<account>.<market_type>` (e.g., `ledger.trades.live.spot`, `ledger.trades.paper.futures`)

**Rationale:** Allows the service to subscribe to `ledger.trades.>` for all trades, while enabling future per-account or per-market filtering. The `ledger.` prefix avoids collision with existing spot-canvas subjects (`candles.>`, `alerts.>`).

### 5. chi/v5 router for REST API

**Choice:** `go-chi/chi/v5` for routing, `encoding/json` for serialization, matching spot-canvas-app.

**Rationale:** Consistency with the existing codebase. chi is lightweight, composable (middleware for logging, CORS), and already proven in the ecosystem.

### 6. Event-sourced position calculation

**Choice:** Positions and balances are derived from the trade history (source of truth). Portfolio state is materialized in a `ledger_positions` table and updated transactionally when trades are ingested.

**Rationale:** Trade history is the immutable source of truth. Materialized positions give fast reads without recalculating from scratch. Positions can be rebuilt from trade history if needed (repair/audit).

**Alternatives considered:**
- Pure event sourcing with on-the-fly calculation: Too slow for queries as trade history grows
- Separate position tracking independent of trades: Risks drift between trades and positions

### 7. Table naming with `ledger_` prefix

**Choice:** All ledger tables use a `ledger_` prefix (e.g., `ledger_accounts`, `ledger_trades`, `ledger_positions`, `ledger_orders`).

**Rationale:** Since we share the database with spot-canvas-app, prefixing avoids naming collisions with existing or future tables (e.g., a generic `trades` table). Makes it clear which tables belong to the ledger module.

### 8. Cloud Run deployment

**Choice:** Deploy as a Cloud Run service in the same region (europe-west3) using the same patterns as spot-canvas-app (Cloud Build, Artifact Registry, Secret Manager).

**Rationale:** Consistent deployment pipeline. Cloud Run handles scaling, TLS, and health checks. The service connects to Cloud SQL via Unix socket proxy, same as spot-canvas-app.

### 9. Configuration pattern

**Choice:** Follow spot-canvas-app's config pattern — `DATABASE_URL` or Cloud SQL components (`CLOUDSQL_INSTANCE`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`), NATS URLs/creds from env vars, `.env` for local dev.

**Rationale:** Identical config approach across the ecosystem makes operations simpler and secrets management consistent.

### 10. Project structure

```
ledger/
├── cmd/ledger/          # main.go — binary entry point
├── internal/
│   ├── domain/          # Core types: Trade, Position, Account, Order
│   ├── store/           # PostgreSQL repository (queries, migrations)
│   ├── ingest/          # NATS subscription, trade processing
│   ├── api/             # REST handlers, routes, middleware
│   └── config/          # Configuration loading
├── migrations/          # SQL migration files (ledger_ prefixed tables)
├── Dockerfile
├── cloudbuild.yaml
├── go.mod
└── go.sum
```

**Rationale:** Standard Go project layout matching spot-canvas-app conventions. `internal/` prevents external imports. Domain types are separate from storage and transport layers.

## Risks / Trade-offs

**[Shared database contention]** → The ledger workload is very low volume (daily/swing trades). Reads and writes won't impact spot-canvas-app's candle ingestion. WAL mode and connection pooling handle concurrent access well.

**[At-least-once delivery means duplicate trades]** → Idempotent ingestion: each trade has a unique ID, and inserts use `ON CONFLICT DO NOTHING`. Duplicates are safely discarded.

**[Service downtime causes trade backlog]** → JetStream durable consumers retain undelivered messages. On restart, the service catches up automatically.

**[Schema evolution for tax reporting]** → The initial schema captures comprehensive trade data (timestamps, fees, quantities, prices, side). Additional tax-specific columns or tables can be added via migrations when reporting is implemented.

**[Migration coordination with spot-canvas-app]** → Ledger migrations are independent (separate numbered files, `ledger_` prefixed tables). No risk of conflicting with spot-canvas-app migrations since they operate on different tables.

**[Cloud Run cold starts]** → The service needs to be responsive for NATS ingestion. Setting min-instances to 1 in production ensures no cold start delays for trade processing.
