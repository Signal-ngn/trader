## 1. Store Layer — Paginated Positions

- [x] 1.1 Add `PositionFilter` struct to `internal/store/positions.go` (fields: `Status string`, `Limit int`, `Cursor string`)
- [x] 1.2 Add `PositionListResult` struct (fields: `Positions []domain.Position`, `NextCursor string`)
- [x] 1.3 Update `ListPositions` signature to accept `PositionFilter` and return `(*PositionListResult, error)`
- [x] 1.4 Implement cursor encode/decode for positions using `(opened_at, id)` — reuse the same base64 pattern as `encodeCursor`/`decodeCursor` in `trades.go`
- [x] 1.5 Update `ListPositions` SQL query to apply `WHERE (opened_at, id) < (cursor_time, cursor_id)` keyset pagination with `LIMIT`
- [x] 1.6 Fix all callers of the old `ListPositions` signature (`handleListPositions`, `GetPortfolioSummary`) to pass a `PositionFilter`

## 2. API Layer — Paginated Positions Endpoint

- [x] 2.1 Update `handleListPositions` in `internal/api/handlers.go` to read `limit` and `cursor` query params and pass them via `PositionFilter`
- [x] 2.2 Change `handleListPositions` response body from a bare JSON array to `{"positions": [...], "next_cursor": "..."}` (omit `next_cursor` when empty)
- [x] 2.3 Update `GetPortfolioSummary` caller in `handlePortfolioSummary` to use updated `ListPositions` with `PositionFilter{Status: "open", Limit: 200}`

## 3. CLI — `ledger trades list --roundtrip`

- [x] 3.1 Add `PositionListResult` and position struct types to `cmd/ledger/cmd_trades.go` (matching the updated positions endpoint response shape)
- [x] 3.2 Add `--raw` bool flag (with alias `--legs`) to `tradesListCmd`
- [x] 3.3 In `tradesListCmd.RunE`: default to positions-based round-trip view (`GET /api/v1/accounts/{accountId}/positions?status=all`); follow cursors and accumulate until `--limit` rows are reached (stop early) or `next_cursor` is empty (`--limit 0` = follow all pages); when `--raw` / `--legs` is set, fall back to the trades endpoint with the same `--limit` logic already in place
- [x] 3.4 Print warning if `--symbol`, `--side`, `--market-type`, `--start`, or `--end` flags are set without `--raw` / `--legs` (these filters only apply to the raw trades endpoint)
- [x] 3.5 Implement round-trip table output (default): columns RESULT, SYMBOL, DIR, SIZE, ENTRY, EXIT, P&L, P&L%, OPENED, CLOSED, EXIT-REASON; `realized_pnl > 0` → `✓ win`, `≤ 0` → `✗ loss`, open → `open`
- [x] 3.6 With `--json` and no `--raw`: print the raw JSON position array; with `--json --raw`: print the raw JSON trades array

## 4. Web Server — Replace `buildRoundTrips()`

- [ ] 4.1 Add `LedgerPositionsResponse` struct to `spot-canvas-app/internal/web/dashmodels/ledger.go` (fields: `Positions []LedgerPosition`, `NextCursor string`)
- [ ] 4.2 Add `GetPositions(ctx, accountID, status string)` method to `LedgerClient` in `spot-canvas-app/internal/web/handlers/dashboard_ledger.go` — fetches `GET /api/v1/accounts/{accountId}/positions?status=<status>&limit=200`, follows `next_cursor` up to a cap of 200 total positions
- [ ] 4.3 Add `positionToRoundTrip(p dashmodels.LedgerPosition) dashmodels.RoundTrip` converter function mapping position fields to `RoundTrip` struct
- [ ] 4.4 In `HandleLedgerPage`: replace `buildRoundTrips(trades)` with `GetPositions(ctx, selectedAccount, "all")` → map to `[]RoundTrip` via `positionToRoundTrip`
- [ ] 4.5 In `HandleLedgerStream` `pushStatsAndTrades`: replace `GetTrades` + `buildRoundTrips` with `GetPositions(ctx, selectedAccount, "all")`; push the resulting `LedgerTradesBodyFragment`
- [ ] 4.6 Remove `buildRoundTrips()`, `sortRoundTrips()`, and the raw-trade `GetTrades` call from the initial page load (trades fetch is no longer needed for the round-trip table — keep only for any other use)
- [ ] 4.7 Run `task build:web` and confirm clean compile

## 5. Tests

- [x] 5.1 Add unit test for paginated `ListPositions` in `internal/store/` — verify cursor round-trip and that open/closed filter works
- [x] 5.2 Add handler test for `GET /api/v1/accounts/{id}/positions` — verify new JSON shape `{"positions":[...], "next_cursor":"..."}` and that `limit`/`cursor` params are respected
