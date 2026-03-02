## 1. Configuration

- [x] 1.1 Add trading engine config fields to `internal/config/config.go`: `TradingEnabled`, `TradingMode`, `TraderAccount`, `StrategyFilter`, `PortfolioSize`, `PositionSizePct`, `MaxPositionSize`, `MinPositionSize`, `MaxPositions`, `DailyLossLimit`, `KillSwitchFile`, `SNAPIKey`, `SNAPIURL`, `SNNATSCredsFile`, `BinanceAPIKey`, `BinanceAPISecret`

## 2. Database Migration

- [x] 2.1 Write migration `007_engine_position_state`: create `engine_position_state` table with columns `id`, `account_id`, `symbol`, `market_type`, `side`, `entry_price`, `stop_loss`, `take_profit`, `leverage`, `strategy`, `opened_at`, `peak_price`, `trailing_stop`, `tenant_id`

## 3. Engine Package Scaffold

- [x] 3.1 Create `internal/engine/` package with `engine.go` — define `Engine` struct holding config, repo, exchange, NGS connection, position state cache, cooldown map, conflict guard map
- [x] 3.2 Define `Engine.Start(ctx context.Context) error` — starts signal loop and risk loop goroutines, loads startup state, returns on context cancellation
- [x] 3.3 Wire engine startup into `cmd/traderd/main.go` — start engine goroutine when `cfg.TradingEnabled` is true, after NATS and DB are ready

## 4. Signal Execution

- [x] 4.1 Copy and adapt `SignalPayload`, `signalAllowlist`, `buildSubject`, `parseSubject` from `sn` into `internal/engine/signals.go` — remove viper/cobra deps, accept config struct
- [x] 4.2 Implement `fetchAllowlist(ctx, cfg)` — HTTP GET to `SN_API_URL/config/trading` with `SN_API_KEY`, build allowlist from enabled configs expanding all strategy lists
- [x] 4.3 Implement `resolveNATSCreds(cfg)` — return creds file path from `SN_NATS_CREDS_FILE` or write embedded JWT to temp file
- [x] 4.4 Implement NGS NATS connection in `Engine.Start` — connect to `tls://connect.ngs.global`, subscribe to `signals.>`, with exponential backoff retry (10s→5m)
- [x] 4.5 Implement signal handler — parse subject, unmarshal `SignalPayload`, run all filter checks in order: allowlist, strategy prefix filter, stale (>2min), confidence (<0.5 for BUY/SHORT), cooldown
- [x] 4.6 Implement allowlist refresh ticker — re-fetch every 5 minutes in a goroutine, update in-memory allowlist atomically

## 5. Position Engine

- [x] 5.1 Implement `mapSignalToTrade(signal, cfg, tradingConfig)` — map action to side, determine market type from trading config, set strategy metadata fields
- [x] 5.2 Implement `calculatePositionSize(signal, cfg, tradingConfig)` — use signal `position_pct` if provided, else `PositionSizePct`; clamp to [Min, Max]; calculate quantity and margin
- [x] 5.3 Implement balance check — call `repo.GetAccountBalance`, compare against required margin/cost, skip trade if insufficient
- [x] 5.4 Implement direction conflict guard — `sync.Map` keyed by `symbol`, seeded from `ledger_positions` open positions on startup; block conflicting opens
- [x] 5.5 Implement max positions check — count open rows in `engine_position_state` for account, block if >= `MaxPositions` (when > 0)
- [x] 5.6 Implement `executeOpenTrade(ctx, signal, trade)` — paper: call `repo.InsertTradeAndUpdatePosition` directly; live: call exchange adapter first, use fill price/qty for ledger write
- [x] 5.7 Implement `executeCloseTrade(ctx, position, reason)` — paper: construct sell/buy trade and call `repo.InsertTradeAndUpdatePosition`; live: call exchange adapter first
- [x] 5.8 Implement `engine_position_state` store methods on `store.Repository`: `InsertPositionState`, `LoadPositionStates(accountID)`, `UpdatePositionState`, `DeletePositionState`

## 6. Risk Management

- [x] 6.1 Implement `Engine.startRiskLoop(ctx)` — tick every 5 minutes, call `evaluatePositions`
- [x] 6.2 Implement `evaluatePositions(ctx)` — load open positions from `ledger_positions`, reconcile with `engine_position_state` (prune orphans), evaluate each position
- [x] 6.3 Implement stop-loss check — apply 0.1% sanity check, use default -4% if too close, close position if price breaches SL
- [x] 6.4 Implement take-profit check — apply 0.1% sanity check, use default +10% if too close, close position if price reaches TP
- [x] 6.5 Implement trailing stop — activate at +3% unrealised P&L, trail 2% behind peak, update peak and trailing stop level, close if price breaches
- [x] 6.6 Implement max hold time check — close positions open longer than 48 hours with exit_reason=`max hold time`
- [x] 6.7 Implement kill switch check — read `KillSwitchFile` existence before every open trade and on each risk loop tick; log and skip opens when active
- [x] 6.8 Implement daily loss limit — track realised P&L from engine-recorded trades since midnight UTC; block new opens when loss exceeds `DailyLossLimit` (when > 0)

## 7. Exchange Adapter

- [x] 7.1 Define `Exchange` interface in `internal/engine/exchange.go`: `OpenPosition`, `ClosePosition`, `GetBalance`
- [x] 7.2 Implement `NoopExchange` — `OpenPosition` returns synthetic fill at signal price, zero fees; `ClosePosition` returns same; `GetBalance` returns configured portfolio size
- [x] 7.3 Implement `BinanceFuturesExchange` — add `github.com/adshao/go-binance/v2` dependency, implement `OpenPosition` with market order, set leverage before order
- [x] 7.4 Implement `BinanceFuturesExchange.ClosePosition` — fetch open position quantity from Binance, place opposite-side market order
- [x] 7.5 Implement `BinanceFuturesExchange.GetBalance` — fetch USDT available balance from futures account
- [x] 7.6 Implement Binance 429 retry — retry once after 1 second on rate limit response, return error if still failing
- [x] 7.7 Validate Binance credentials on engine startup in live mode — call `GetBalance`, abort engine (not service) if credentials are missing or invalid

## 8. SSE Trade Stream

- [x] 8.1 Implement `StreamRegistry` in `internal/api/` — thread-safe fan-out registry mapping `accountID` → set of `http.ResponseWriter` subscribers; methods: `Subscribe`, `Unsubscribe`, `Publish`
- [x] 8.2 Add `GET /api/v1/accounts/{accountId}/trades/stream` handler — set SSE headers, register client in `StreamRegistry`, block on context done or client disconnect, unregister on exit
- [x] 8.3 Wire `StreamRegistry` into `publishTradeNotification` in `internal/ingest/consumer.go` — after NATS publish, call `registry.Publish(accountID, tradePayload)`
- [x] 8.4 Pass `StreamRegistry` through `api.NewServer` and wire into router
- [x] 8.5 Implement SSE event format — write `data: <json>\n\n` for each trade event, flush immediately

## 9. `trader trades watch` CLI

- [x] 9.1 Add `tradesWatchCmd` to `cmd/trader/cmd_trades.go` — `trader trades watch <account-id>` subcommand
- [x] 9.2 Implement SSE client loop — connect to `GET /api/v1/accounts/{accountId}/trades/stream` with `Authorization` header, read lines, write JSONL to stdout
- [x] 9.3 Implement reconnect on disconnect — retry after 5 seconds, log reconnect attempt to stderr
- [x] 9.4 Handle SIGINT/SIGTERM — close connection and exit 0
- [x] 9.5 Register `tradesWatchCmd` under `tradesCmd`

## 10. Cleanup

- [x] 10.1 Remove `scripts/nats_trader.py`
- [x] 10.2 Remove `scripts/mexc_futures.py`
- [x] 10.3 Remove `scripts/.position_state.json` and `scripts/.exit_reasons.json` if present
