## Why

The ledger service currently has no CLI — trading bots and operators must use
raw HTTP calls to list accounts, query positions, view trades, and import
historic data. A dedicated `ledger` CLI (modelled on the `sn` CLI) gives bots
and developers a scriptable, human-friendly interface to all ledger API
endpoints, with built-in auth, config management, and table/JSON output.

## What Changes

- New binary `cmd/ledger-cli/` — a standalone CLI tool named `ledger`
- No separate login flow — auth credentials are read from `~/.config/sn/config.yaml`
  (`api_key` written there by `sn auth login`); users authenticate via `sn`, not `ledger`
- `tenant_id` is resolved lazily on first use via `GET /auth/resolve` and cached
  in `~/.config/ledger/config.yaml`
- `LEDGER_API_KEY` and `LEDGER_TENANT_ID` env vars override config for bot use
- `ledger accounts list`
- `ledger portfolio <account-id>` — open positions + total realized P&L
- `ledger positions <account-id>` — with `--status open|closed|all`
- `ledger trades <account-id>` — with `--symbol`, `--side`, `--market-type`,
  `--start`, `--end`, `--limit`; auto-follows cursor pagination
- `ledger orders <account-id>` — with `--status`, `--symbol`, `--limit`
- `ledger import <file.json>` — POST batch of historic trades to
  `/api/v1/import`; prints inserted/duplicate/error counts
- `ledger config show|set|get` — manage `~/.config/ledger/config.yaml`
  (ledger-specific overrides only: `ledger_url`, `tenant_id`)
- `--json` flag on all read commands for machine-readable output
- `--ledger-url` persistent flag + `LEDGER_URL` env var override

## Capabilities

### New Capabilities
- `ledger-cli`: CLI binary wrapping all ledger REST API endpoints; auth via
  API key sourced from `~/.config/sn/config.yaml` or `LEDGER_API_KEY` env var;
  `tenant_id` resolved lazily via `/auth/resolve` and cached locally; table and
  JSON output modes; ledger-specific config at `~/.config/ledger/config.yaml`

### Modified Capabilities

## Impact

- New directory `cmd/ledger-cli/` alongside existing `cmd/ledger/`
- New dependencies: `github.com/spf13/cobra`, `github.com/spf13/viper`,
  `github.com/olekukonko/tablewriter` (same stack as the `sn` CLI)
- No changes to the server, store, domain, or ingest packages
- Requires `sn` to be installed and `sn auth login` completed for human users;
  bots set `LEDGER_API_KEY` directly
- Installable as a standalone binary: `go install ./cmd/ledger-cli`
