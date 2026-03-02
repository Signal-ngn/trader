## Why

The trader service is a ledger — it records trades but doesn't make them. To close the loop, it needs to subscribe to trading signals from SignalNGN, decide whether to act on them, execute trades (paper or live), and manage open positions with risk rules. The Python `nats_trader.py` bot does this today but relies on subprocess-spawning of CLI tools, has no position-state persistence, and runs outside the service boundary. This change brings trading execution natively into the Go service as an opt-in background worker — activated by configuration, running as a goroutine alongside the existing NATS consumer, with no new binary or entrypoint required.

## What Changes

- **New background worker:** `internal/exec/` package — signal-driven trading engine that starts as a goroutine in `traderd` when `TRADING_ENABLED=true`
- **New NATS connection to Synadia NGS:** signals live on `tls://connect.ngs.global` under `signals.<exchange>.<product>.<granularity>.<strategy>` — a separate cluster from the ledger's own NATS. The engine reuses the same subject structure, allowlist logic, and embedded subscriber-only credentials as `sn signals`
- **Signal filtering:** validates signals against a trading config fetched from the SignalNGN ingestion API; filters by product, exchange, strategy prefix, and confidence
- **Position sizing:** calculates position size from portfolio/balance config with min/max clamps
- **Paper trade execution:** records trades directly into the ledger DB (no subprocess, no HTTP round-trip)
- **Live trade execution:** calls Binance Futures API, then records in ledger; aborts ledger write if exchange call fails
- **Risk management goroutine:** runs every 5 minutes, enforces stop-loss, take-profit, trailing stop (activates at +3%, trails 2%), and max hold time (48h)
- **Position state persistence:** in-memory cache backed by a JSON file (or DB table), survives restarts
- **Direction conflict guard:** prevents opening LONG when SHORT is already open on the same product
- **Config refresh:** re-fetches trading config from SignalNGN API every 5 minutes
- **Kill switch:** halts live trading when a designated file is present on disk
- **Removes:** `scripts/nats_trader.py`, `scripts/mexc_futures.py`, `.position_state.json`, `.exit_reasons.json`
- **Read-only API remains unchanged** — trades produced by the engine enter the ledger through the same internal `InsertTradeAndUpdatePosition` path used by the ingest consumer

## Capabilities

### New Capabilities

- `signal-execution`: Subscribe to SignalNGN signal NATS subject, filter signals against trading config, enforce cooldowns, and route valid signals to the position engine
- `position-engine`: Open and close positions (paper and live), calculate position sizing, enforce direction conflict guard, persist position state across restarts, and check account balance before opening a position (skips trade if balance is set and insufficient)
- `risk-management`: Per-position stop-loss/take-profit enforcement, trailing stop, max hold time, kill switch, daily loss limit, and max concurrent positions limit
- `exchange-adapter`: Abstraction over exchange APIs; initial implementation for Binance Futures (open/close position, get balance)
- `trade-watch`: A `trader trades watch <account-id>` CLI subcommand that streams trade events to stdout as they happen, one JSON object per line (JSONL). The service exposes a `GET /api/v1/accounts/{accountId}/trades/stream` SSE endpoint; the CLI connects over standard HTTP using the existing API key — no NATS credentials required. Designed to be piped into bot scripts (e.g. Telegram notifier).

### Modified Capabilities

- `rest-api`: New `GET /api/v1/accounts/{accountId}/trades/stream` SSE endpoint added. Existing endpoints unchanged.
- `trade-ingestion`: No requirement changes. The new engine writes trades through the same internal DB path — no spec-level behavior changes.

## Impact

- **`cmd/traderd/main.go`**: Start trading engine goroutine when `TRADING_ENABLED=true`
- **`internal/config/config.go`**: New trading config fields (mode, account, strategy filter, sizing params, risk params)
- **`internal/exec/`**: New package — signal handler, position engine, risk manager, exchange adapter (Binance), position state store
- **`internal/store/`**: Possibly new DB table for position state (alternative to JSON file); no changes to existing tables
- **NATS**: Second NATS connection added — to Synadia NGS (`tls://connect.ngs.global`) using the embedded subscriber-only credentials from the `sn` tool. The existing ledger NATS connection (`trader.trades.*`) is unchanged
- **New env vars**: `TRADING_ENABLED`, `TRADING_MODE`, `TRADER_ACCOUNT`, `STRATEGY_FILTER`, `PORTFOLIO_SIZE`, `POSITION_SIZE_PCT`, `MAX_POSITION_SIZE`, `MIN_POSITION_SIZE`, `MAX_POSITIONS`, `DAILY_LOSS_LIMIT`, `SN_API_URL`, `BINANCE_API_KEY`, `BINANCE_API_SECRET`
- **Exchange credentials**: `BINANCE_API_KEY` and `BINANCE_API_SECRET` (and equivalent keys for future exchanges) are stored in GCP Secret Manager and mounted as environment variables by Cloud Run at deploy time. The service reads them as plain env vars — it has no direct dependency on the Secret Manager SDK. Provisioning and rotation of these secrets is the tenant's responsibility; a guided deployment flow is **out of scope** and will be addressed in a future change.
- **External deps**: Binance Go SDK (live mode only), SignalNGN ingestion API (trading config endpoint)
- **Cloud Run**: Single service instance = single trading account. Multiple strategies/accounts = multiple Cloud Run services, each with its own env config
- **`cmd/trader/cmd_trades.go`**: New `trader trades watch <account-id>` subcommand — connects to `GET /api/v1/accounts/{accountId}/trades/stream` (SSE) and writes JSONL to stdout
- **`internal/api/`**: New SSE endpoint `GET /api/v1/accounts/{accountId}/trades/stream` — fans out internal NATS trade notifications to HTTP subscribers
