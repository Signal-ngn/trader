## Why

The ledger already pairs entry and exit trades into `ledger_positions` rows during ingestion — each closed position is a complete round-trip with entry price, exit price, realized P&L, direction, and exit reason. This paired view is exactly what traders want to see when reviewing trade history.

Today, the web dashboard re-implements this pairing client-side in `buildRoundTrips()` using a fragile timestamp-proximity heuristic over a paged 50-trade slice. The CLI has no paired view at all — `ledger trades list` shows raw individual trades which are hard to interpret (each round-trip is two rows). Any new client would have to re-implement the same pairing logic.

The fix is simple: expose the server-authoritative paired view through the existing positions endpoint and as a new `--roundtrip` flag on `ledger trades list`. The web server can then drop its client-side heuristic entirely.

## What Changes

- **`GET /api/v1/accounts/{accountId}/positions` gains cursor pagination**: the positions endpoint currently returns all positions unpaginated. Add `cursor` and `limit` query parameters (same pattern as trades) so the web server and CLI can page through large position histories.
- **`ledger trades list <account>`**: default output is now the closed-position (round-trip) view, sourced from `GET /api/v1/accounts/{accountId}/positions?status=closed`. Columns: Symbol, Dir, Size, Entry, Exit, P&L, P&L%, Opened, Closed, Exit Reason. Open positions are included at the bottom as incomplete round-trips. Pass `--raw` (alias `--legs`) to revert to the old individual-trade rows.
- **Web server drops `buildRoundTrips()`**: `HandleLedgerPage` and `HandleLedgerStream` replace the `buildRoundTrips(trades)` call with a direct fetch of `GET /api/v1/accounts/{accountId}/positions?status=all`, which is authoritative and correct for any number of concurrent same-symbol positions.
- **`LedgerClient.GetPositions()`**: new method on the web server's ledger client, mirrors the existing `GetPortfolio` but returns all positions with status filter.

## Capabilities

### New Capabilities
- `roundtrip-trade-view`: `ledger trades list` defaults to the paired open+closed positions view; `--raw` / `--legs` reverts to individual trade rows.

### Modified Capabilities
- `rest-api`: positions endpoint gains cursor pagination.
- `ledger-cli`: `ledger trades list` defaults to round-trip view; gains `--raw` / `--legs` flag to show individual trade rows.
- `ledger-dashboard`: web server replaces client-side `buildRoundTrips()` with server-fetched positions; `LedgerClient` gains `GetPositions()`.

## Impact

- `internal/api/handlers.go` — add pagination to `handleListPositions`
- `internal/store/positions.go` — add `PositionFilter` + cursor pagination to `ListPositions`
- `cmd/ledger/cmd_trades.go` — add `--roundtrip` flag and positions-based output path
- `spot-canvas-app/internal/web/handlers/dashboard_ledger.go` — replace `buildRoundTrips()` with `GetPositions()` call; update `HandleLedgerPage` and `HandleLedgerStream`
- `spot-canvas-app/internal/web/dashmodels/ledger.go` — `RoundTrip` struct can be derived from `LedgerPosition` directly
