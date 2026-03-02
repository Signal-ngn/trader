# Trader Service — Architecture & Developer Reference

Comprehensive reference for the `trader` Go service (module `github.com/Signal-ngn/trader`).
Project root: `/Users/anssi/Documents/projects/spot-canvas/trader`.

---

## Repo Layout

```
trader/
├── cmd/
│   ├── traderd/          # Server binary (main.go)
│   └── trader/           # CLI binary (main.go + cmd_*.go)
├── internal/
│   ├── api/              # HTTP handlers, router, SSE stream
│   │   └── middleware/   # Auth, tenant injection
│   ├── config/           # Config struct + Load() from env
│   ├── domain/           # Shared types (Trade, Position, Side …)
│   ├── engine/           # Trading engine goroutine
│   ├── ingest/           # NATS JetStream consumer
│   └── store/            # Postgres repository + migrations
│       └── migrations/   # SQL files (001–008)
└── openspec/             # OpenSpec change workflow docs
```

---

## Binaries

### `traderd` — server

`cmd/traderd/main.go` wires everything together:

1. `config.Load()` → reads env / `.env`
2. `store.New()` + `store.Migrate()` → Postgres pool, runs migrations
3. `api.NewServer(repo)` → HTTP server on `$HTTP_PORT` (default 8080)
4. `ingest.ConnectNATS()` → NATS connection
5. `ingest.NewConsumer(nc, repo).WithPublisher(srv.StreamRegistry())` → starts JetStream consumer
6. If `TRADING_ENABLED=true` → `engine.New(cfg, repo).Start(ctx)` in a goroutine

### `trader` — CLI

Cobra-based. Subcommands:

| Command | Description |
|---|---|
| `trader trades list <account-id>` | List trades via REST |
| `trader trades watch <account-id>` | Stream live trade events (SSE → JSONL stdout) |
| `trader accounts list` | List accounts |
| `trader positions list <account-id>` | List open positions |

`trader trades watch` reconnects automatically every 5 s on disconnect.
Exits cleanly on `SIGINT`/`SIGTERM`.

---

## Configuration (`internal/config/config.go`)

All fields read from env vars. `.env` file is auto-loaded if present.

### Core

| Env var | Default | Description |
|---|---|---|
| `HTTP_PORT` | `8080` | Server port |
| `DATABASE_URL` | `postgres://…localhost…` | Postgres DSN |
| `CLOUDSQL_INSTANCE` | — | Cloud SQL instance name (overrides DATABASE_URL) |
| `NATS_URLS` | `nats://localhost:4222` | NATS server URLs |
| `NATS_CREDS_FILE` | — | Path to NATS credentials file |
| `NATS_CREDS` | — | Inline NATS credentials (written to temp file) |
| `LOG_LEVEL` | `info` | zerolog level |
| `ENVIRONMENT` | `development` | `development` / `production` |
| `ENFORCE_AUTH` | `true` | Set `false` to disable auth middleware |

### Trading Engine

| Env var | Default | Description |
|---|---|---|
| `TRADING_ENABLED` | `false` | Set `true` to start the engine goroutine |
| `TRADING_MODE` | `paper` | `paper` (NoopExchange) or `live` (Binance Futures) |
| `TRADER_ACCOUNT` | `paper` | Account ID all engine trades are booked to |
| `STRATEGY_FILTER` | — | Optional prefix filter for signal strategy names |
| `PORTFOLIO_SIZE` | `10000` | Total portfolio size in USD (for position sizing) |
| `POSITION_SIZE_PCT` | `10` | Default position size as % of portfolio |
| `MAX_POSITION_SIZE` | `0` | Max position size USD (0 = no limit) |
| `MIN_POSITION_SIZE` | `0` | Min position size USD (0 = no minimum) |
| `MAX_POSITIONS` | `0` | Max concurrent open positions (0 = no limit) |
| `DAILY_LOSS_LIMIT` | `0` | Max daily loss USD before halting opens (0 = no limit) |
| `KILL_SWITCH_FILE` | `/tmp/trader.kill` | Touch this file to halt all new opens immediately |
| `SN_API_KEY` | — | SignalNGN API key (required when TRADING_ENABLED) |
| `SN_API_URL` | `https://api.signal-ngn.com` | SignalNGN API base URL |
| `SN_NATS_CREDS_FILE` | — | Path to NGS NATS credentials (optional; embedded default used otherwise) |
| `BINANCE_API_KEY` | — | Binance API key (live mode only) |
| `BINANCE_API_SECRET` | — | Binance API secret (live mode only) |

---

## Database (`internal/store/`)

### Migrations

Files in `internal/store/migrations/`, run at startup via `store.Migrate()`.

| Migration | Table / Purpose |
|---|---|
| `001` | `ledger_trades` |
| `002` | `ledger_positions` |
| `003` | `ledger_accounts` |
| `004–007` | Various schema additions |
| `008` | `engine_position_state` — engine risk metadata |

### `engine_position_state` (migration 008)

Separate from `ledger_positions`. Stores per-position risk state for the engine.

```sql
UNIQUE (account_id, symbol, market_type, tenant_id)
```

Columns: `id`, `account_id`, `symbol`, `market_type`, `side` (`long`/`short`),
`entry_price`, `stop_loss`, `take_profit`, `leverage`, `strategy`, `opened_at`,
`peak_price`, `trailing_stop`, `tenant_id`.

### Repository methods added for the engine

In `internal/store/engine_positions.go`:

| Method | Description |
|---|---|
| `InsertPositionState(ctx, tenantID, *EnginePositionState)` | Upserts (ON CONFLICT DO UPDATE) |
| `LoadPositionStates(ctx, accountID)` | Loads all rows for account (ordered by `opened_at`) |
| `UpdatePositionState(ctx, tenantID, *EnginePositionState)` | Updates `peak_price`, `trailing_stop`, `stop_loss`, `take_profit` |
| `DeletePositionState(ctx, tenantID, symbol, marketType, accountID)` | Removes on position close |
| `CountOpenPositionStates(ctx, accountID)` | Returns count — used for `MAX_POSITIONS` guard |
| `ListOpenPositionsForAccount(ctx, accountID)` | Queries `ledger_positions` (open status, all tenants) |
| `DailyRealizedPnL(ctx, accountID)` | `SUM(realized_pnl)` from `ledger_trades` since midnight UTC |

`DailyRealizedPnL` is the authoritative daily loss check — it is DB-backed so it survives restarts and counts trades from all sources (engine, API ingestion, manual).

---

## Trading Engine (`internal/engine/`)

### Overview

The engine runs as a goroutine inside `traderd`. It:

1. Fetches the signal allowlist from the SN API
2. Connects to Synadia NGS (NATS) and subscribes to `signals.>`
3. Filters and processes incoming signals
4. Manages positions: opens, closes, and risk enforcement

### `engine.go` — Engine struct & lifecycle

```go
type Engine struct { … }

func New(cfg *config.Config, repo *store.Repository) *Engine
func (e *Engine) Start(ctx context.Context) error
```

`Start` flow:
- Validates live-mode Binance credentials (if `TradingMode == "live"`)
- Requires `SN_API_KEY`
- Fetches initial allowlist
- Calls `loadStartupState` (seeds conflict guard from `ledger_positions`, loads `engine_position_state`)
- Starts allowlist refresher goroutine (every 5 min)
- Starts risk loop goroutine (every 5 min)
- Runs `runSignalLoop` (blocks until ctx cancelled)

In-memory state on the `Engine` struct:

| Field | Type | Description |
|---|---|---|
| `posState` | `map[string]*PositionState` | symbol → risk state (synced to DB) |
| `cooldown` | `map[cooldownKey]time.Time` | per (symbol, action) cooldown expiry (5 min after open) |
| `conflict` | `map[string]string` | symbol → `"long"` or `"short"` direction guard |
| `allowlist` | `signalAllowlist` | set of allowed (exchange, product, granularity, strategy) tuples |
| `lastPrice` | `map[string]float64` | last signal price per symbol — used by risk loop |

### `signals.go` — Signal ingestion

**Subject format:** `signals.<exchange>.<product>.<granularity>.<strategy>`

**`SignalPayload`** fields (from NATS):
`strategy`, `product`, `exchange`, `action` (`BUY`/`SELL`/`SHORT`/`COVER`),
`market`, `leverage`, `price`, `confidence`, `reason`, `stop_loss`, `take_profit`,
`risk_reasoning`, `position_pct` (0–1 fraction), `indicators`, `timestamp` (Unix seconds).

**Allowlist (`signalAllowlist`)**:
- Fetched from `GET {SN_API_URL}/config/trading` using `SN_API_KEY`
- Rebuilt every 5 minutes
- Strategy matching uses **prefix matching**: `"ml_xgboost+trend"` matches allowlist entry `"ml_xgboost"` (splits on `_` and `+`)

**Signal pipeline (in `handleSignal`):**
1. Parse subject → exchange, product, granularity, strategy
2. Parse JSON payload
3. Allowlist check (silent drop if not allowed)
4. `STRATEGY_FILTER` prefix check (silent drop)
5. Staleness check: drop if `signal.Timestamp` > 2 minutes old
6. Confidence check: drop `BUY`/`SHORT` if `confidence < 0.5`
7. Cooldown check: drop `BUY`/`SHORT` within 5 min of previous open for same (symbol, action)
8. Cache `signal.Price` in `lastPrice[product]`
9. Route to `processSignal`

**NGS connection:** `tls://connect.ngs.global` with embedded credentials (subscribe-only JWT). Override with `SN_NATS_CREDS_FILE`. Exponential backoff on connect failure (10 s → 5 min).

### `position.go` — Position engine

**`mapSignalToSide(action, tc) → (Side, PositionSide, MarketType)`**

| Action | Side | PositionSide | MarketType |
|---|---|---|---|
| `BUY` | `buy` | `long` | spot (if no long/short strats) or futures |
| `SELL` | `sell` | `long` | same |
| `SHORT` | `sell` | `short` | futures |
| `COVER` | `buy` | `short` | futures |

Market type is `futures` when `TradingConfig.StrategiesLong` or `StrategiesShort` is non-empty.

**`calculatePositionSize(signal, tc, marketType) → (size, qty, margin, error)`**

```
pct = cfg.PositionSizePct          (default)
pct = signal.PositionPct * 100     (if signal.PositionPct > 0 — signal uses 0–1 fraction)

size = cfg.PortfolioSize * (pct / 100)
size = clamp(size, cfg.MinPositionSize, cfg.MaxPositionSize)
qty  = size / signal.Price
margin = size / leverage            (futures only)
```

**Open signal guards (in `handleOpenSignal`):**
1. Kill switch file exists → skip
2. `isDailyLossLimitReached` → skip (queries `DailyRealizedPnL` from DB)
3. Direction conflict (already open in opposite direction) → skip
4. `MAX_POSITIONS` check → skip

**Trade IDs:** `"engine-<accountID>-<symbol>-<unixNano>"` for opens, `"engine-close-…"` for closes.

**All trades write to the ledger** via `repo.InsertTradeAndUpdatePosition` — same path as ingest.

**CRITICAL log on ledger failure in live mode:** If the Binance order executes but the DB write fails, a `CRITICAL` log is emitted to trigger manual recovery.

### `risk.go` — Risk loop

Runs every `5 minutes`. Per position, in order:

| Rule | Constants | Behaviour |
|---|---|---|
| Max hold time | `maxHoldDuration = 48h` | Close if `time.Since(openedAt) > 48h` |
| Stop-loss | `defaultSLPct = 4%`, `slSanityPct = 0.1%` | If SL missing or within 0.1% of entry, use -4% (long) / +4% (short) |
| Take-profit | `defaultTPPct = 10%`, `slSanityPct = 0.1%` | If TP missing or within 0.1% of entry, use +10% (long) / -10% (short) |
| Trailing stop | `trailingActivatePct = 3%`, `trailingTrailPct = 2%` | Activates when unrealised gain ≥ 3%; trails 2% behind peak; never loosens; for leveraged positions, gain is scaled by `1/leverage` |

**Price resolution in risk loop:**
1. `lastPrice[symbol]` — cached from most recent signal
2. SN price API `GET {SN_API_URL}/prices/{exchange}/{product}?granularity=ONE_MINUTE` → `{"close": float64}`
3. Skip tick if neither source is available (log warning)

**Orphan pruning:** Each tick checks `engine_position_state` rows against `ledger_positions`. Orphaned rows (position closed externally) are deleted and removed from the conflict guard.

### `exchange.go` — Exchange adapter

```go
type Exchange interface {
    OpenPosition(ctx, OpenPositionRequest) (*OrderResult, error)
    ClosePosition(ctx, ClosePositionRequest) (*OrderResult, error)
    GetBalance(ctx) (float64, error)
}
```

**`NoopExchange`** (paper mode):
- `OpenPosition`: returns synthetic fill at signal price, qty = sizeUSD / price, margin = sizeUSD / leverage
- `ClosePosition`: returns zeroed result (caller uses cached price)
- `GetBalance`: returns `cfg.PortfolioSize`

**`BinanceFuturesExchange`** (live mode):
- Raw HTTP + HMAC-SHA256 signing, no SDK
- Base URL: `https://fapi.binance.com`
- Endpoints used: `POST /fapi/v1/order`, `POST /fapi/v1/leverage`, `GET /fapi/v2/balance`, `GET /fapi/v2/positionRisk`
- 1-retry on HTTP 429 rate limit (waits 1 s)
- Product ID conversion: `BTC-USD` → `BTCUSDT` (strips hyphen, replaces `USD` suffix with `USDT`)
- `GetBalance` returns USDT available balance

**Test injection:** `BinanceFuturesExchange.WithClient(binanceFuturesClient)` swaps the HTTP client for testing. The `binanceFuturesClient` interface is **unexported** — only usable from `package engine` (not `engine_test`). External tests should mock the `Exchange` interface directly.

---

## SSE Trade Stream (`internal/api/stream.go`)

### `StreamRegistry`

Thread-safe fan-out: `accountID → set of subscriber channels`.

```go
func NewStreamRegistry() *StreamRegistry
func (r *StreamRegistry) Subscribe(accountID string) (<-chan []byte, func())
func (r *StreamRegistry) Publish(accountID string, payload interface{})
```

**Design invariant:** Subscriber channels are **never closed**. The SSE handler exits via `ctx.Done()` (HTTP request context cancel). This avoids the send-on-closed-channel race. `unsubscribe()` only removes the channel from the registry map.

**Slow consumers:** `Publish` is non-blocking (buffer = 16). Events are dropped silently if the buffer is full.

**Concurrency:** Safe for concurrent `Subscribe`, `Publish`, and `unsubscribe` calls. Lock is held only for the registry map mutation / snapshot copy; sends happen outside the lock.

### SSE handler

`GET /api/v1/accounts/{accountId}/trades/stream`

- Sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`
- Writes events as `data: <json>\n\n`
- Exits on `ctx.Done()` (client disconnect or server shutdown)

### Wiring

`ingest.Consumer` holds a `TradePublisher` interface (set via `WithPublisher`).
`api.StreamRegistry` satisfies `TradePublisher`.
After a trade is ingested and written to the ledger, `consumer.handleMessage` calls `publisher.Publish(accountID, tradeSSEPayload)`.

`tradeSSEPayload` fields: `trade_id`, `account_id`, `symbol`, `side`, `quantity`, `price`, `fee`, `market_type`, `timestamp` (RFC3339), `strategy?`, `confidence?`, `stop_loss?`, `take_profit?`, `entry_reason?`, `exit_reason?`.

---

## Tests

### Engine tests (`internal/engine/`)

`signals_test.go`:
- `signalAllowlist.allows`: exact match, suffix match, `+` separator, no-match cases, empty allowlist
- `parseSubject` / `buildSubject`: round-trip, wildcards, too-few-segments edge case
- Staleness boundary: exactly-2-min-old signal is accepted; 2-min-1-ns is dropped

`position_test.go`:
- `mapSignalToSide`: all 4 actions × spot / futures configurations
- `calculatePositionSize`: default pct, signal override (0–1 fraction × 100), max clamp, min clamp, futures margin with leverage, zero-leverage fallback to 1, zero-price error

`risk_test.go`:
- SL/TP sanity threshold (too-close → default, zero → default, valid explicit → kept, both sides)
- Trailing stop: activation threshold, tightening on new peak, never-loosens, short-side direction

### API tests (`internal/api/`)

`stream_test.go`:
- Publish reaches subscriber
- Wrong account not received
- Multiple subscribers on same account
- Unsubscribe stops delivery (no more events; no panic)
- No subscribers — Publish is a no-op
- Slow subscriber event dropped (buffer full, non-blocking)
- Concurrent publish + subscribe under `-race` (10 × runs, clean)

All tests pass with `go test -race ./...`.

---

## Key Design Decisions

| Decision | Rationale |
|---|---|
| Package `engine` (not `exec`) | `exec` collides with stdlib |
| `engine_position_state` separate from `ledger_positions` | Decouples engine bookkeeping from ledger; avoids migration coupling |
| No go-binance SDK | Avoids large CLI dependency; raw HTTP + HMAC is sufficient; `binanceFuturesClient` interface enables mock injection |
| SSE channel never closed | Prevents send-on-closed-channel race; handler exits via `ctx.Done()` |
| `TradePublisher` interface in ingest | Avoids circular import (`api` ↔ `ingest`) |
| Daily loss limit queries DB | Survives restarts; counts all trades for account, not just engine-originated |
| Price feed: signal cache → SN API → skip | Risk enforcement stays timely without a dedicated price feed goroutine |
| `isDailyLossLimitReached` allows trade on DB error | Fail-open on transient DB errors to avoid false halts |
| CRITICAL log on live-mode ledger failure | Exchange order executed but DB write failed — needs manual recovery |
