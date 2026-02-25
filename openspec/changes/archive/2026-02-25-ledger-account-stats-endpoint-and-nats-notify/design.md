## Context

The ledger service is a Go HTTP server backed by PostgreSQL. It ingests trades via NATS JetStream (`LEDGER_TRADES` stream) and exposes a REST API for queries. The service already holds a `*nats.Conn` in the API `Server` struct (passed in from `cmd/ledgerd/main.go`).

Two gaps exist:
1. **No aggregate-stats endpoint** — callers must page through all trades to compute win/loss counts. The web dashboard exposes this bug visibly: stats cards show wrong numbers when > 50 trades exist.
2. **No post-ingestion notification** — real-time UIs must poll; there is no lightweight push signal after a new trade is stored.

The CLI (`cmd/ledger`) already has `accounts list`. The `accounts` subcommand is where `show` naturally belongs.

## Goals / Non-Goals

**Goals:**
- `GET /api/v1/accounts/{accountId}/stats` returns pre-computed all-time aggregate stats from a single SQL aggregation query
- After each successfully ingested trade, publish a small JSON notification to `ledger.trades.notify.<tenantID>` on NATS core
- `ledger accounts show <account-id>` CLI subcommand calls the stats endpoint and prints a human-readable summary

**Non-Goals:**
- JetStream persistence for the notification subject — core pub/sub is sufficient; no replay needed
- Changing how trade ingestion works (JetStream consumer stays as-is)
- Exposing stats breakdown by symbol or strategy (future work)

## Decisions

### Decision 1: Stats computed in SQL, not application layer
`GetAccountStats` executes a single SQL query with conditional aggregation over `ledger_trades`:

```sql
SELECT
  COUNT(*) FILTER (WHERE exit_reason IS NOT NULL OR side = 'sell') AS total_closed,
  COUNT(*) FILTER (WHERE exit_reason IS NOT NULL AND realized_pnl > 0) AS win_count,
  COUNT(*) FILTER (WHERE exit_reason IS NOT NULL AND realized_pnl <= 0) AS loss_count,
  COALESCE(SUM(realized_pnl), 0) AS total_realized_pnl
FROM ledger_trades
WHERE tenant_id = $1 AND account_id = $2
```

Plus a separate count of open positions from `ledger_positions`.

**Alternative considered**: Materialised view or cached counter columns. Rejected — trade volume is low (thousands, not millions) so a live aggregate per request is fast enough and avoids cache invalidation complexity.

### Decision 2: NATS core (not JetStream) for notifications
The notification subject `ledger.trades.notify.<tenantID>` uses bare NATS core publish — fire-and-forget, no persistence. Subscribers that miss a message (e.g. SSE connection not yet established) will simply not get that notification; the next trade will trigger a refresh anyway.

**Alternative considered**: A separate JetStream subject with short retention. Rejected — notifications are only useful in real-time; replay makes no sense for a "re-fetch now" signal.

### Decision 3: Notification payload is minimal
```json
{"tenant_id": "<uuid>", "account_id": "<string>", "trade_id": "<string>"}
```
Subscribers use this as a trigger to re-query the API. They do not need the full trade in the notification.

**Alternative considered**: Full trade payload in the notification. Rejected — the subscriber (web server SSE handler) needs the full aggregated stats anyway, so it would re-query regardless; sending full trade data in the notification is wasted bytes.

### Decision 4: Win/loss determination from `realized_pnl` sign, filtered by `exit_reason`
A trade is an "exit" (and therefore contributes to win/loss) if it has a non-empty `exit_reason`. A win is exit_reason set AND `realized_pnl > 0`. This matches the existing `buildRoundTrips` logic in the web server.

**Alternative considered**: Count positions instead of trades. Rejected — trades are the source of truth for P&L; positions can be rebuilt and are derived state.

### Decision 5: Stats endpoint scoped by tenant via existing auth middleware
The handler uses `middleware.TenantIDFromContext` (identical to all other handlers). No new auth mechanism is needed.

### Decision 6: NATS publish is best-effort, never blocks ingestion
The publish call is `nc.Publish(subject, payload)` — synchronous but non-blocking (NATS client buffers outgoing). If NATS is disconnected, the publish silently fails. A failed publish SHALL NOT cause the ingestion handler to return an error or NAK the JetStream message; trade persistence is never sacrificed for notification delivery.

## Risks / Trade-offs

- **[Risk] Stats query is slow for very high trade volumes** → Mitigation: `(tenant_id, account_id)` is already indexed (primary filter). If needed, add a partial index on `exit_reason IS NOT NULL`. For current scale (thousands of trades) the live aggregation is fine.
- **[Risk] Notification fires before the DB transaction is committed** → Mitigation: publish AFTER `InsertTradeAndUpdatePosition` returns nil (i.e. after commit). The existing code already does this in `handleMessage`.
- **[Risk] NATS not connected at publish time** → Best-effort: `nc.Publish` returns error which is logged at warn level and ignored.
- **[Risk] `win_count` calculation is approximate** → The current round-trip logic in the web server uses exit_reason to detect exits. Trades imported without `exit_reason` set (e.g. raw spot buys without a paired exit) won't count as wins/losses. This is the same limitation as today and acceptable for now.

## Migration Plan

1. Deploy this change to the ledger service — additive only, no schema changes
2. The new endpoint is immediately available; existing consumers are unaffected
3. The web server change (separate repo) can be deployed any time after this

## Open Questions

- Should `total_closed` count raw trades with `exit_reason` set, or should it count matched round-trips? For now, counting exit trades is simpler and good enough. Round-trip matching can be added to the query later.
