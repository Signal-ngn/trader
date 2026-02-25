## Why

The Ledger service currently has no way to query all-time aggregate statistics for an account in a single call — callers must page through all trades themselves, which is expensive and incorrect when a paginated view is used. Additionally, after a trade is ingested there is no lightweight signal for real-time UIs (e.g. the spot-canvas web dashboard) to know a new trade arrived; they must poll. Both gaps limit the usefulness of the service as a live trading monitor.

## What Changes

- **New `GET /api/v1/accounts/{accountId}/stats` endpoint**: Returns pre-computed all-time aggregate stats for the account — total trade count, win count, loss count, win rate, total realized P&L, and a count of open positions. Computed directly in the database, not from a paged query.
- **NATS publish on trade ingestion**: After each successfully ingested trade, the ledger service SHALL publish a lightweight notification to `ledger.trades.notify.<tenantID>` on NATS core (not JetStream). The payload contains `{"tenant_id":"...","account_id":"...","trade_id":"..."}` so subscribers can react without re-subscribing to the full trade stream.
- **`trades add` publishes notification**: The `ledger trades add` CLI command calls the REST import endpoint (as today) AND the server-side ingestion already fires the NATS notification — no extra CLI step needed; notification is always server-side.
- **`ledger accounts show <account-id>` CLI command**: A new subcommand that calls `GET /api/v1/accounts/{accountId}/stats` and renders a concise terminal summary of the account statistics.

## Capabilities

### New Capabilities
- `account-stats`: New REST endpoint `GET /api/v1/accounts/{accountId}/stats` returning all-time aggregate account statistics computed in the database.
- `trade-ingested-notify`: After each trade ingestion, publish a notification to `ledger.trades.notify.<tenantID>` on NATS core so real-time subscribers can react immediately.

### Modified Capabilities
- `ledger-cli`: Add `accounts show <account-id>` subcommand that calls the new stats endpoint and renders a terminal summary.

## Impact

- `internal/api/handlers.go` — add `handleAccountStats` handler
- `internal/api/router.go` — register `GET /api/v1/accounts/{accountId}/stats`
- `internal/store/accounts.go` — add `GetAccountStats` repository method (SQL aggregation over `ledger_trades` and `ledger_positions`)
- `internal/ingest/consumer.go` — after `InsertTradeAndUpdatePosition` succeeds, publish to `ledger.trades.notify.<tenantID>`
- `cmd/ledger/cmd_accounts.go` — add `accounts show` subcommand
- Security: `ledger.trades.notify.<tenantID>` is on NATS core (no persistence). It is scoped by tenant UUID so different tenants cannot read each other's notifications. The subject is published server-side only — there is no way for a client to inject a notification. The web server SSE handler validates the `tenantID` from its own auth context before subscribing, so a user cannot subscribe to another tenant's notifications.
