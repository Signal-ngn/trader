---
name: prod-db
description: Query the production PostgreSQL database (signalngn-prod / signalngn-db). Use when you need to inspect live candle data, row counts, product coverage, tenant records, or any other production DB state. Covers proxy setup, connection string, and common query patterns.
allowed-tools: Bash
---

# Production Database

The production database is a PostgreSQL 15 instance on Cloud SQL in `signalngn-prod` (europe-west1).

## Connection Details

| Item | Value |
|------|-------|
| GCP project | `signalngn-prod` |
| Cloud SQL instance | `signalngn-prod:europe-west1:signalngn-db` |
| Database | `spot_canvas` |
| User | `spot` |
| Local proxy port | **5434** |
| Staging proxy port | 5433 (different instance — `spotcanvas-staging`) |

The password is in Secret Manager:

```bash
DB_PASSWORD=$(gcloud secrets versions access latest \
  --secret=signalngn-prod-db-password \
  --project=signalngn-prod \
  --account=anssip@gmail.com)
```

Full connection string:

```bash
PROD_DB="postgres://spot:${DB_PASSWORD}@localhost:5434/spot_canvas?sslmode=disable"
```

## Step 1 — Check if the proxy is already running

```bash
pg_isready -h localhost -p 5434
```

If it says `accepting connections`, skip to Step 3.

## Step 2 — Start the Cloud SQL Auth Proxy

Open a dedicated terminal and run:

```bash
cd /Users/anssi/Documents/projects/spot-canvas/spot-canvas-app
./cloud-sql-proxy signalngn-prod:europe-west1:signalngn-db --port 5434
```

Or via task:

```bash
task proxy:prod
```

Leave it running; the proxy keeps the tunnel alive.

## Step 3 — Run queries

```bash
DB_PASSWORD=$(gcloud secrets versions access latest \
  --secret=signalngn-prod-db-password \
  --project=signalngn-prod \
  --account=anssip@gmail.com)
PROD_DB="postgres://spot:${DB_PASSWORD}@localhost:5434/spot_canvas?sslmode=disable"

psql "$PROD_DB" -c "<SQL here>"
```

## Common Queries

### Candle coverage by exchange

```sql
SELECT exchange, COUNT(*) AS total, MAX(last_update) AS last_written
FROM live_candles
GROUP BY exchange
ORDER BY last_written DESC;
```

### Binance candle counts by product + granularity

```sql
SELECT product_id, granularity, COUNT(*) AS candle_count,
       MIN(timestamp) AS earliest, MAX(timestamp) AS latest
FROM live_candles
WHERE exchange = 'binance'
GROUP BY product_id, granularity
ORDER BY product_id, granularity;
```

### Recent writes (last N minutes)

```sql
SELECT exchange, COUNT(*) AS rows_updated
FROM live_candles
WHERE last_update > NOW() - INTERVAL '5 minutes'
GROUP BY exchange
ORDER BY rows_updated DESC;
```

### Ingestion products registered per exchange

```sql
SELECT exchange, enabled, COUNT(*) AS product_count
FROM ingestion_products
GROUP BY exchange, enabled
ORDER BY exchange, enabled;
```

### Active DB connections (useful to verify ingestion is connected)

```sql
SELECT application_name, state, COUNT(*)
FROM pg_stat_activity
WHERE datname = 'spot_canvas'
GROUP BY application_name, state
ORDER BY count DESC;
```

### List all tables

```bash
psql "$PROD_DB" -c "\dt"
```

## Key Tables

| Table | Description |
|-------|-------------|
| `live_candles` | Primary candle store — OHLCV per exchange/product/granularity/timestamp |
| `ingestion_products` | Products registered for ingestion (exchange, product_id, enabled) |
| `tenants` | Tenant accounts with tier, ts_url, plan info |
| `trading_config` | Per-tenant trading configuration |
| `backtest_results` | Backtest run results |
| `jobs` | Backtest + backfill job tracking |
| `user_strategies` | User-written Starlark strategies |

## Notes

- **Don't confuse staging and prod.** Staging proxy runs on port **5433** and connects to `spotcanvas-staging:europe-west3:spot-canvas-db`. Production is port **5434**.
- The `sn` CLI (`sn products list`, `sn metrics`, etc.) points at production (`signalngn-prod`) by default — check with `sn config get api_url`.
- `live_candles` has a composite primary key on `(exchange, product_id, granularity, timestamp)`. Writes use UPSERT — the `last_update` column is updated on every write, making it a good proxy for "is ingestion alive?".
- Binance candles started flowing after `release-1.0.88` (2026-02-26). There is **no historical Binance data** before that date — run a backfill job to populate history.
