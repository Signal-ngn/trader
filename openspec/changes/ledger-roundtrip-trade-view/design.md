## Context

`ledger_positions` is the authoritative paired view. During ingestion, every entry trade creates or extends a position row; every exit trade closes it, recording `exit_price`, `exit_reason`, `realized_pnl`, `opened_at`, and `closed_at`. This is exactly a round-trip record — no client-side pairing is needed.

The current `GET /api/v1/accounts/{id}/positions` endpoint returns all positions for a status filter but has no pagination — it loads everything into memory. For accounts with hundreds of closed positions this is wasteful. The trades endpoint already has cursor pagination; positions should match.

The web server's `buildRoundTrips()` matches entry+exit trades by symbol and timestamp proximity. This breaks when two concurrent positions exist for the same symbol (e.g. a long and a short opened close together). Using `ledger_positions` rows instead is always correct because the ledger pairs them during ingestion using the actual position state machine.

## Goals / Non-Goals

**Goals:**
- Add cursor pagination to `GET /api/v1/accounts/{id}/positions`
- `ledger trades list <account>` defaults to the round-trip view; `--raw` (alias `--legs`) renders the old individual-trade rows
- Web server fetches positions directly instead of computing round-trips from raw trades

**Non-Goals:**
- Changing how positions are built during ingestion
- Adding filtering by symbol/date to positions endpoint (future work)
- Changing the existing `ledger positions list` command (it stays as-is; `--roundtrip` is on `trades list` for discoverability)

## Decisions

### Decision 1: Positions endpoint cursor pagination mirrors trades endpoint
Use the same `cursor` / `limit` / `next_cursor` pattern as `ListTrades`. Cursor encodes `(opened_at, id)` for stable ordering. Default limit 50, max 200.

`ListPositions` grows a `PositionFilter` struct (same pattern as `TradeFilter`) with `Limit`, `Cursor`, `Status`. Returns `PositionListResult{Positions []Position, NextCursor string}`.

**Alternative considered**: keyset pagination on `closed_at`. Rejected — open positions have no `closed_at`; `opened_at` works for both.

### Decision 2: Round-trip is the default; `--raw` / `--legs` opts out
`ledger trades list paper` defaults to the positions-based round-trip view — it's the most useful output for reviewing trade history. `--raw` (with alias `--legs`) reverts to raw individual trade rows by switching the data source back to the trades endpoint. Both `--json` and table output are supported in either mode.

**Alternative considered**: `--roundtrip` opt-in flag. Rejected — the paired view is strictly more useful; making users discover a flag to get it is bad UX. Defaulting to it and providing `--raw` / `--legs` for power users who need individual rows is the better default.

**Alternative considered**: separate `ledger roundtrips` subcommand. Rejected — it's the same conceptual operation (show trade history) just with a different view; a flag is cleaner.

### Decision 3: Web server replaces `buildRoundTrips()` with `GetPositions(status=all)`
`GetPositions` fetches `GET /api/v1/accounts/{id}/positions?status=all` (paginated, following cursors up to a reasonable cap of 200). The result maps directly to `RoundTrip` structs:

| Position field | RoundTrip field |
|---|---|
| `side` (long/short) | `Side` |
| `avg_entry_price` | `EntryPrice` |
| `exit_price` | `ExitPrice` |
| `cost_basis` | `PositionSize` |
| `realized_pnl` | `PnL` |
| `opened_at` | `OpenedAt` |
| `closed_at` | `ClosedAt` |
| `exit_reason` | `ExitReason` |
| `status == "open"` | `IsOpen` |
| `realized_pnl > 0` | `IsWin` |

**Alternative considered**: keep `buildRoundTrips()` and add the positions fetch only for stats. Rejected — the heuristic is wrong for concurrent same-symbol positions; fixing it properly means using the server data.

### Decision 4: `GetPositions` pages up to 200 positions, not all
The web page shows the 50 most recent round-trips. Fetching up to 200 positions gives enough history without unbounded memory use.

### Decision 5: `--limit` applies uniformly to both round-trip and raw modes
The existing `--limit` flag (default 50, `0 = all pages`) governs how many rows are returned regardless of mode. Round-trip mode follows cursors until `--limit` rows are accumulated, then stops — identical behaviour to raw mode. This gives a single mental model: `--limit 0` always means "give me everything", whether you're looking at positions or individual trades.

## Risks / Trade-offs

- **[Risk] `buildRoundTrips()` removal changes how the trade history table is sorted/displayed** → Mitigation: positions are already ordered by `opened_at DESC`; sort matches current behaviour. The "orphaned entry" edge case (entry with no exit) maps cleanly to `IsOpen=true`.
- **[Risk] Pagination on positions adds complexity to the store layer** → Low risk; it's a copy of the already-tested trades pagination.
- **[Risk] Web server fetches positions AND portfolio on initial load** → `GetPortfolio` returns open positions + total P&L. With this change, open positions come from `GetPositions(status=open)` instead. We can drop `GetPortfolio` or keep it for `TotalRealizedPnL` only. Keep it for now to avoid breaking changes; `GetPositions(status=all)` is the new source for the round-trip table.

## Migration Plan

1. Deploy ledger service with paginated positions endpoint — backwards compatible (existing callers without `limit`/`cursor` still work, they just get a `next_cursor` they can ignore)
2. Deploy web server change — replaces `buildRoundTrips()` with positions fetch
3. No database migrations required
