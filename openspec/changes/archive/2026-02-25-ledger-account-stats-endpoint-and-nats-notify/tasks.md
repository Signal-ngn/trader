## 1. Account Stats — Store Layer

- [x] 1.1 Add `AccountStats` struct to `internal/store/accounts.go` (fields: `TotalTrades`, `ClosedTrades`, `WinCount`, `LossCount`, `WinRate`, `TotalRealizedPnL`, `OpenPositions`)
- [x] 1.2 Implement `GetAccountStats(ctx, tenantID, accountID)` in `internal/store/accounts.go` using a single SQL conditional aggregation query over `ledger_trades` plus a count of open `ledger_positions`
- [x] 1.3 Compute `WinRate` in Go after the query (win_count / closed_trades, 0 if closed_trades == 0)

## 2. Account Stats — API Layer

- [x] 2.1 Add `handleAccountStats` handler in `internal/api/handlers.go` — resolves tenant from context, calls `repo.GetAccountStats`, returns JSON; returns 404 if account does not exist
- [x] 2.2 Register route `GET /api/v1/accounts/{accountId}/stats` in `internal/api/router.go` inside the existing `/api/v1` auth-protected block

## 3. NATS Notification — Ingestion

- [x] 3.1 Add `publishTradeNotification(nc *nats.Conn, tenantID uuid.UUID, accountID, tradeID string)` helper in `internal/ingest/consumer.go` — publishes JSON `{"tenant_id":"...","account_id":"...","trade_id":"..."}` to `ledger.trades.notify.<tenantID>`; logs warn on error, never returns error
- [x] 3.2 In `Consumer.handleMessage`, call `publishTradeNotification` after `InsertTradeAndUpdatePosition` returns `inserted=true` (i.e. only for new trades, not duplicates)

## 4. CLI — accounts show

- [x] 4.1 Add `AccountStats` type (matching stats endpoint JSON) to `cmd/ledger/cmd_accounts.go`
- [x] 4.2 Implement `accountsShowCmd` (`ledger accounts show <account-id>`) — calls `GET /api/v1/accounts/{accountId}/stats`, prints a summary table (Account, Total Trades, Closed Trades, Wins, Losses, Win Rate %, Realized P&L); exits non-zero on 404
- [x] 4.3 Add `--json` flag to `accountsShowCmd` to print raw JSON response
- [x] 4.4 Register `accountsShowCmd` under `accountsCmd` in `init()`

## 5. Tests

- [x] 5.1 Add unit test for `GetAccountStats` in `internal/store/` (table-driven: no trades, mixed wins/losses, only entries no exits)
- [x] 5.2 Add integration test or handler test for `GET /api/v1/accounts/{accountId}/stats` covering: 200 with data, 404 for missing account, 401 without auth
- [x] 5.3 Add unit test for `publishTradeNotification` — verify subject format and that a publish error does not propagate
