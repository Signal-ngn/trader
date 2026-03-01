## Why

A crypto trading bot runs externally and needs a dedicated ledger service to track its portfolio, order history, and store all data required for tax reporting. The bot currently handles position tracking itself, but this responsibility needs to move to a standalone Go service that ingests trades via NATS and exposes data through a REST API.

## What Changes

- New Go service that acts as a trading ledger
- NATS subscription to ingest trades published by the trading bot in real-time
- Portfolio tracking across multiple trading accounts (live and paper trading)
- Support for both spot and leveraged futures trading
- Persistent order/trade history
- REST API for querying portfolio state, open positions, and order history
- Data model designed to support Finnish tax reporting in a later phase

## Capabilities

### New Capabilities

- `trade-ingestion`: NATS subscription that receives trade events from the bot, validates them, and persists them to the ledger. Supports multiple accounts.
- `portfolio-tracking`: Maintains current portfolio state (balances, open positions, unrealized P&L) derived from ingested trades. Supports spot and leveraged futures across multiple accounts.
- `order-history`: Stores and indexes all orders and trade fills for historical querying with filtering and pagination.
- `rest-api`: HTTP REST endpoints for querying portfolio state, open positions, and order history. Read-only — all writes come through NATS.
- `tax-data`: Data storage and model design ensuring all information needed for Finnish tax reporting (cost basis, gains/losses, holding periods) is captured. Reporting functionality deferred to a later phase.

### Modified Capabilities

_None — this is a greenfield service._

## Impact

- **New Go module**: Entire service is new, no existing code affected
- **Dependencies**: NATS client library, HTTP router, database driver (e.g., SQLite or PostgreSQL)
- **Infrastructure**: Requires a running NATS server; the trading bot must publish trades in an agreed-upon format
- **Systems**: The trading bot will stop tracking positions internally and rely on this ledger instead
- **Data**: Dashboard generation (currently done by the bot) will consume this service's REST API for its data
