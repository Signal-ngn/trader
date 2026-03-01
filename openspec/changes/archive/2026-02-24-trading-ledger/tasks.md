## 1. Project Scaffolding

- [x] 1.1 Initialize Go module (`go.mod`) with dependencies: pgx/v5, chi/v5, nats.go, zerolog, godotenv
- [x] 1.2 Create project directory structure: `cmd/ledger/`, `internal/{domain,store,ingest,api,config}`, `migrations/`
- [x] 1.3 Implement `internal/config/config.go` — load DATABASE_URL (or Cloud SQL components), NATS_URLS, NATS_CREDS_FILE, NATS_CREDS, LOG_LEVEL, ENVIRONMENT, HTTP_PORT from env vars with `.env` support
- [x] 1.4 Create `cmd/ledger/main.go` — wire up config, database pool, NATS connection, HTTP server, and graceful shutdown

## 2. Domain Types

- [x] 2.1 Define `Account` struct: ID, Name, Type (live/paper), CreatedAt
- [x] 2.2 Define `Trade` struct: TradeID, AccountID, Symbol, Side (buy/sell), Quantity, Price, Fee, FeeCurrency, MarketType (spot/futures), Timestamp, IngestedAt; futures fields: Leverage, Margin, LiquidationPrice, FundingFee (nullable)
- [x] 2.3 Define `Position` struct: ID, AccountID, Symbol, MarketType, Side (long/short), Quantity, AvgEntryPrice, CostBasis, RealizedPnL, Leverage, Margin, LiquidationPrice, Status (open/closed), OpenedAt, ClosedAt
- [x] 2.4 Define `Order` struct: OrderID, AccountID, Symbol, Side, OrderType (market/limit), RequestedQty, FilledQty, AvgFillPrice, Status (open/filled/partially_filled/cancelled), MarketType, CreatedAt, UpdatedAt

## 3. Database Schema & Migrations

- [x] 3.1 Create migration `001_ledger_initial_schema.up.sql`: `ledger_accounts` table with id, name, type, created_at
- [x] 3.2 Add `ledger_trades` table to migration: trade_id (PK), account_id (FK), symbol, side, quantity, price, fee, fee_currency, market_type, timestamp, ingested_at, cost_basis, realized_pnl; futures columns: leverage, margin, liquidation_price, funding_fee (nullable). Index on (account_id, timestamp DESC), (account_id, symbol, timestamp DESC)
- [x] 3.3 Add `ledger_positions` table to migration: id, account_id, symbol, market_type, side, quantity, avg_entry_price, cost_basis, realized_pnl, leverage, margin, liquidation_price, status, opened_at, closed_at. Unique constraint on (account_id, symbol, market_type) for open positions. Index on (account_id, status)
- [x] 3.4 Add `ledger_orders` table to migration: order_id (PK), account_id (FK), symbol, side, order_type, requested_qty, filled_qty, avg_fill_price, status, market_type, created_at, updated_at. Index on (account_id, status, created_at DESC)
- [x] 3.5 Create corresponding `001_ledger_initial_schema.down.sql` drop migration
- [x] 3.6 Implement migration runner in `internal/store/` that applies migrations on startup using `golang-migrate` or manual SQL execution

## 4. Database Repository

- [x] 4.1 Implement `internal/store/postgres.go` — NewRepository with pgxpool, Ping, Close
- [x] 4.2 Implement `InsertTrade` — insert trade with ON CONFLICT DO NOTHING on trade_id, return whether inserted
- [x] 4.3 Implement `UpsertPosition` — create or update position within a transaction, handling spot buy/sell and futures open/close logic (avg entry price recalculation, realized P&L)
- [x] 4.4 Implement `InsertTradeAndUpdatePosition` — single transaction wrapping InsertTrade + UpsertPosition with rollback on failure
- [x] 4.5 Implement `GetOrCreateAccount` — lookup by ID, auto-create if missing
- [x] 4.6 Implement `ListAccounts` — return all accounts
- [x] 4.7 Implement `GetPortfolioSummary` — return open positions and aggregate realized P&L for an account
- [x] 4.8 Implement `ListPositions` — query positions by account with optional status filter (open/closed/all)
- [x] 4.9 Implement `ListTrades` — query trades by account with filters (symbol, side, market_type, start/end time) and cursor-based pagination (default 50, max 200)
- [x] 4.10 Implement `ListOrders` — query orders by account with optional status/symbol filters and cursor-based pagination
- [x] 4.11 Implement `UpsertOrder` — insert or update order, recalculate filled_qty and avg_fill_price from associated trades
- [x] 4.12 Implement `RebuildPositions` — delete all positions for an account and replay trades in timestamp order to rebuild

## 5. Trade Ingestion (NATS)

- [x] 5.1 Define trade event JSON schema/struct for NATS messages (matching domain Trade fields)
- [x] 5.2 Implement `internal/ingest/consumer.go` — JetStream durable consumer subscribing to `ledger.trades.>`, message acknowledgement
- [x] 5.3 Implement trade event validation: required fields check, market_type validation (spot/futures), account ID matches subject
- [x] 5.4 Implement message handler: validate → GetOrCreateAccount → InsertTradeAndUpdatePosition → ack; on validation error log and reject; on DB error log and nack for redelivery
- [x] 5.5 Add unit tests for trade event validation (valid, missing fields, invalid market type)
- [x] 5.6 Add integration test for full ingestion flow: publish trade to NATS → verify trade in DB → verify position updated

## 6. REST API

- [x] 6.1 Implement `internal/api/router.go` — chi router setup with logging and CORS middleware, method-not-allowed handler for non-GET requests on /api/v1/
- [x] 6.2 Implement `GET /health` — check DB ping and NATS connection status
- [x] 6.3 Implement `GET /api/v1/accounts` — list all accounts
- [x] 6.4 Implement `GET /api/v1/accounts/{accountId}/portfolio` — portfolio summary (positions + aggregate P&L), 404 if account not found
- [x] 6.5 Implement `GET /api/v1/accounts/{accountId}/positions` — list positions with `status` query param (open/closed/all, default: open)
- [x] 6.6 Implement `GET /api/v1/accounts/{accountId}/trades` — list trades with filter and pagination query params (symbol, side, market_type, start, end, cursor, limit)
- [x] 6.7 Implement `GET /api/v1/accounts/{accountId}/orders` — list orders with filter and pagination query params (status, symbol, cursor, limit)
- [x] 6.8 Add unit tests for API handlers using httptest (mock repository)
- [x] 6.9 Add integration test: ingest trades via NATS, then query via REST API and verify responses

## 7. Deployment

- [x] 7.1 Create `Dockerfile` — multi-stage build (Go build → minimal runtime image), matching spot-canvas-app patterns
- [x] 7.2 Create `cloudbuild.yaml` — build image, push to Artifact Registry, deploy to Cloud Run (europe-west3) with Cloud SQL and NATS secrets
- [x] 7.3 Create `docker-compose.yml` for local development — postgres and NATS services, matching spot-canvas-app's local setup
- [x] 7.4 Create `.env.example` with all required environment variables documented
- [x] 7.5 Add README.md with setup instructions, local development guide, and API documentation
