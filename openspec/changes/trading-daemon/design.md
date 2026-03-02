## Context

The trader service is a Go Cloud Run service (`cmd/traderd`) with two existing goroutines: an HTTP server and a NATS JetStream consumer that ingests trade events from `trader.trades.*` into PostgreSQL. The service already has the full domain model (`domain.Trade`, `domain.Position`), the transactional write path (`InsertTradeAndUpdatePosition`), and a position store — all the plumbing a trading engine needs.

The trading engine is a new third goroutine, started when `TRADING_ENABLED=true`. It connects to Synadia NGS to receive signals, filters them, and executes trades by calling the existing store layer directly — no HTTP, no subprocess, no CLI tools.

The existing ingest consumer and HTTP API are completely untouched.

## Goals / Non-Goals

**Goals:**
- Add a trading engine goroutine to `traderd` activated by `TRADING_ENABLED=true`
- Reuse the existing `InsertTradeAndUpdatePosition` store path for all trade writes
- Connect to Synadia NGS using the same subject structure and credentials as `sn signals`
- Filter signals against the SignalNGN trading config API (allowlist by exchange/product/granularity/strategy)
- Execute paper trades directly; live trades via Binance Futures API first, then ledger
- Maintain per-position risk state (SL/TP, trailing stop, peak P&L, hold time) in a PostgreSQL table
- Run a risk management goroutine every 5 minutes to enforce SL/TP, trailing stop, and max hold time
- Graceful shutdown on SIGTERM (already handled in `traderd/main.go`)

**Non-Goals:**
- New binary or entrypoint — same `traderd` process
- Multi-account per instance — one Cloud Run service = one account
- Multi-exchange per instance — one daemon = one exchange
- Secret Manager SDK — secrets come in as env vars via Cloud Run secret bindings
- CLI subcommand (`trader exec`) — not in this change; the engine is a goroutine, not a command
- Backtesting, web dashboard, risk module CLI — all future changes

## Decisions

### D1: Package name `internal/exec` → renamed to `internal/engine`

`exec` is a reserved stdlib package name and creates confusion. `engine` clearly describes the trading engine concept without collision.

**Alternative:** `trading`, `bot`, `runner`. `engine` wins — it's domain-neutral, unambiguous, and matches how the SignalNGN side refers to its own component.

### D2: Position risk state in a new PostgreSQL table, not a JSON file

The proposal listed a JSON file as an option. PostgreSQL is already the persistence layer and Cloud Run instances are ephemeral — a JSON file on the filesystem would be lost on redeploy. A new table `engine_position_state` stores trailing stop, peak P&L, strategy, opened-at, and other risk metadata per (account, symbol, market_type).

This table is separate from `ledger_positions` (which is owned by the ingest path). The engine reads open positions from `ledger_positions` and overlays risk state from `engine_position_state`.

**Alternative:** Extend `ledger_positions` with engine columns. Rejected — it couples the ledger schema to the engine's internal bookkeeping and would require a rebuild migration on every engine schema change.

### D3: Signal subscription reuses `sn signals` logic verbatim — extracted into `internal/engine/signals.go`

The `SignalPayload` struct, `signalAllowlist`, `buildSubject`, `parseSubject`, and `resolveNATSCreds` functions from `sn/cmd/sn/signals.go` are copied into the engine package and adapted (drop CLI-specific viper/cobra dependencies, accept config struct instead). The allowlist build (`buildSignalAllowlist`) becomes an HTTP call to the SignalNGN API using the tenant's `SN_API_KEY`.

**Alternative:** Import `sn` as a Go module. Rejected — `sn` is a CLI tool, not a library. Copying the ~100 lines of signal logic is cleaner and avoids pulling in cobra/viper as engine dependencies.

### D4: Separate NATS connection for Synadia NGS signals

The existing `ingest.ConnectNATS` connects to the NATS server at `NATS_URLS` using the ledger's own credentials. Signals come from a different server entirely — Synadia NGS (`tls://connect.ngs.global`) — with separate credentials (the embedded subscribe-only JWT from the `sn` tool, overridable via `SN_NATS_CREDS_FILE`). Two connections are required regardless of how the ledger's NATS is deployed.

The engine manages its own NGS connection with the same retry/backoff logic as `ingest.ConnectNATS`.

### D5: Trade writes go through `store.Repository.InsertTradeAndUpdatePosition` directly

The engine constructs a `domain.Trade` and calls `repo.InsertTradeAndUpdatePosition` — the same path the ingest consumer uses. This means paper trades and live trades both flow through the same transactional store logic (position upsert, balance adjustment, P&L calculation). No duplication.

For live trades: exchange order is placed first; if it succeeds, ledger write follows. If the ledger write fails, the trade is logged as an orphan (exchange-executed but not recorded) — a critical alert is raised via notification webhook. This is the same best-effort approach the Python bot used.

### D6: Signal action mapping to trade side

| Signal action | Trade side | Position side |
|---|---|---|
| `BUY` | `buy` (spot or futures long open) | `long` |
| `SELL` | `sell` (spot close or futures long close) | closes `long` |
| `SHORT` | `sell` (futures short open) | `short` |
| `COVER` | `buy` (futures short close) | closes `short` |

The engine maps signal actions to `domain.Side` and determines the position direction from the trading config's market type.

### D6a: Balance check before opening a position

Before executing any opening trade, the engine calls `repo.GetAccountBalance` for the account. If a balance exists and is less than the required margin (futures) or cost (spot), the trade is skipped and logged. If no balance row exists the check is bypassed — balance tracking is optional.

Since `UpsertPosition` already adjusts the balance atomically on every trade write, available balance stays accurate without any extra bookkeeping in the engine.

### D7: Cooldown and direction conflict guard in memory, not DB

A 5-minute per-product cooldown and the direction conflict guard (no LONG while SHORT is open) are enforced in memory using a `sync.Map`. On startup the engine loads open positions from `ledger_positions` to seed the conflict guard. This is fast and sufficient — the engine is single-instance per account.

**Alternative:** DB-based locking. Overkill for a single-instance service. If multi-instance support is added in the future, this decision needs revisiting.

### D8: Risk management as a sub-goroutine within the engine, not a separate goroutine in `main.go`

The engine struct owns a `startRiskLoop` method that ticks every 5 minutes. It's started as a goroutine inside `engine.Start()` and stopped via context cancellation. This keeps all engine logic encapsulated in the `engine` package — `main.go` only calls `engine.Start(ctx)`.

### D9: Binance adapter behind an `Exchange` interface

```go
type Exchange interface {
    OpenPosition(ctx context.Context, req OpenPositionRequest) (*OrderResult, error)
    ClosePosition(ctx context.Context, req ClosePositionRequest) (*OrderResult, error)
    GetBalance(ctx context.Context) (float64, error)
}
```

Paper mode uses a `NoopExchange` that returns a synthetic fill at signal price. Live mode uses `BinanceFuturesExchange`. The engine doesn't know which it's talking to — mode is selected at startup via `TRADING_MODE`.

**Alternative considered:** Separate code paths for paper vs live. Rejected — the interface makes paper mode testable and makes adding future exchanges (Coinbase, OKX) trivial.

### D10: `trader trades watch` uses SSE over HTTP, not NATS

The internal `publishTradeNotification` already fires on the existing NATS connection after every ingested trade. Rather than exposing those NATS credentials to CLI users, the service fans out notifications to HTTP subscribers via a new SSE endpoint `GET /api/v1/accounts/{accountId}/trades/stream`. The CLI connects using the existing API key — no new credentials, no new config keys.

The service maintains an in-memory fan-out registry (a map of account ID → set of active SSE response writers, protected by a mutex). When a trade notification arrives on NATS, the service looks up subscribers for that account and writes an SSE `data:` event. If no subscribers are registered, the notification is silently dropped (fire-and-forget, same as today).

The CLI reads the SSE stream and writes each event as a JSON object to stdout (JSONL), making it trivially pipeable into bot scripts:
```bash
trader trades watch paper | while read line; do
  echo "$line" | jq -r '"Trade: \(.symbol) \(.side) @ \(.price)"'
done
```

**Alternative:** WebSocket. Rejected — SSE is simpler (unidirectional, works over HTTP/1.1, reconnects automatically), and the use case is read-only streaming.

## Risks / Trade-offs

**[Risk] Orphaned live trades** — exchange order executes but ledger write fails.
→ Mitigation: Log at error level with full order details. Provide a manual re-import path via the existing `POST /api/v1/import` endpoint.

**[Risk] Position state divergence** — `engine_position_state` and `ledger_positions` get out of sync (e.g. manual trade recorded via NATS while engine is running).
→ Mitigation: Engine reconciles open positions from `ledger_positions` on every risk loop tick, not just on startup. Positions in `engine_position_state` without a matching open `ledger_positions` row are pruned.

**[Risk] Stale signals acted on after a restart** — NGS core NATS has no replay; on reconnect the engine receives only new messages. But a brief downtime (Cloud Run restart) could mean missing signals.
→ Mitigation: Acceptable for paper mode. For live mode, the 2-minute signal TTL (from the signal `timestamp` field) is checked before acting. Signals older than 2 minutes are dropped.

**[Risk] Two NATS connections increase memory/FD pressure in Cloud Run**.
→ Mitigation: Both connections are lightweight (single TCP socket each). Cloud Run's 256MB minimum is more than sufficient. No action needed.

**[Risk] Binance API rate limits hit during high-signal periods**.
→ Mitigation: Per-product 5-minute cooldown naturally limits order rate. Binance 429 responses are retried once with a 1-second delay, then dropped with an error log.

**[Risk] `SN_API_KEY` required for allowlist fetch — missing key means no signals processed**.
→ Mitigation: On startup, if `SN_API_KEY` is not set and `TRADING_ENABLED=true`, log a fatal error and abort engine startup (not the whole service — HTTP server stays up).

## Migration Plan

Deploy directly with `TRADING_ENABLED=true`. The DB migration for `engine_position_state` runs automatically on startup. For live mode, add `BINANCE_API_KEY` and `BINANCE_API_SECRET` as Secret Manager secrets and bind them to the Cloud Run service.

## Open Questions

- **Granularity field**: The signal subject includes a granularity segment (e.g. `ONE_HOUR`). Should the engine use this for cooldown keying, or just (exchange, product, strategy)? The Python bot ignored granularity for cooldowns.
- **Trailing stop price source**: The risk loop needs current market price to evaluate trailing stops. For paper mode, should we call the SignalNGN price API, or use the last signal price seen for that product?
- **`engine_position_state` tenant scoping**: Should this table be scoped by `tenant_id` (for future multi-tenancy) or just `account_id`? Given the service is single-tenant per Cloud Run instance, `account_id` alone is sufficient now, but adding `tenant_id` upfront avoids a future migration.
