## Context

The ledger service exposes a REST API protected by Bearer API key auth. The
trading bot and operators currently interact with it via raw `curl` calls or
programmatic HTTP clients. The `sn` CLI (in the `sn` repo) is the established
pattern for CLI tooling in this project: cobra commands, viper config,
tablewriter output, a thin `Client` struct wrapping `net/http`. The ledger CLI
follows the same pattern, living as a new binary in `cmd/ledger-cli/` within
the ledger repo.

Auth: API keys are issued by spot-canvas-app via `sn auth login` (Google OAuth
browser flow). The `sn` CLI writes the key to `~/.config/sn/config.yaml`. The
ledger CLI reads that file directly — no separate login flow is needed.

---

## Goals / Non-Goals

**Goals:**
- Wrap every existing `GET` and `POST` ledger endpoint in a CLI command
- Source `api_key` from `~/.config/sn/config.yaml`; `LEDGER_API_KEY` env var
  overrides for bot/CI use
- Resolve `tenant_id` lazily on first use via `GET /auth/resolve`; cache in
  `~/.config/ledger/config.yaml` to avoid the round-trip on subsequent calls
- Human-readable table output by default; `--json` for machine use
- Single self-contained binary with no runtime dependencies
- `LEDGER_URL` env override; `--ledger-url` flag for one-off overrides
- Cursor pagination handled transparently (auto-follow up to `--limit`)

**Non-Goals:**
- Any login/logout commands in the ledger CLI — `sn auth login` is the only
  entry point for obtaining credentials
- Writing trade or order data (no NATS publishing from the CLI)
- Interactive TUI or live-updating views
- Shell completion scripts (can be added later via cobra built-ins)

---

## Decisions

### 1. Same stack as `sn` CLI — cobra + viper + tablewriter

**Decision:** Use `github.com/spf13/cobra`, `github.com/spf13/viper`, and
`github.com/olekukonko/tablewriter`. Same `Client` struct pattern, same
`output.go` helpers, same `config.go` structure.

**Rationale:** Operators already know the `sn` CLI idioms. Code is copy-
adaptable rather than designed from scratch. Keeps the dependency surface small
and familiar.

**Alternative considered:** `urfave/cli` — rejected (inconsistent with `sn`).
**Alternative considered:** plain `flag` package — rejected (no subcommand
tree, no config binding).

---

### 2. API key sourced from `sn` config; no ledger login command

**Decision:** The ledger CLI reads `api_key` from `~/.config/sn/config.yaml`
using a second viper instance (read-only). `LEDGER_API_KEY` env var takes
precedence, allowing bots to inject credentials without needing `sn` installed.

**Rationale:** Users already run `sn auth login` to authenticate with the
platform. Requiring a separate `ledger auth login` step duplicates that flow
and creates credential drift (two copies of the same key). The ledger has no
OAuth endpoints of its own.

**Resolution order for `api_key`:**
1. `LEDGER_API_KEY` env var
2. `~/.config/ledger/config.yaml` `api_key` (manual override escape hatch)
3. `~/.config/sn/config.yaml` `api_key` (primary source for human users)

---

### 3. `tenant_id` resolved lazily and cached

**Decision:** On the first command that requires auth, if `tenant_id` is not
already in `~/.config/ledger/config.yaml` or `LEDGER_TENANT_ID`, the CLI calls
`GET /auth/resolve` and writes the result to `~/.config/ledger/config.yaml`.
Subsequent commands use the cached value.

**Rationale:** Avoids an extra network round-trip on every invocation while
still not requiring a manual login step. Cache is invalidated if the user runs
`ledger config set tenant_id <new-id>` or deletes the config file.

---

### 4. Two config files: `sn` (read-only) + `ledger` (read-write)

**Decision:** The ledger CLI reads `api_key` from `~/.config/sn/config.yaml`
but writes nothing back to it. Ledger-specific overrides (`ledger_url`,
`tenant_id`) live in `~/.config/ledger/config.yaml`.

**Config keys in `~/.config/ledger/config.yaml`:**
| Key | Default | Env |
|---|---|---|
| `ledger_url` | `https://signalngn-ledger-potbdcvufa-ew.a.run.app` | `LEDGER_URL` |
| `tenant_id` | _(resolved lazily)_ | `LEDGER_TENANT_ID` |
| `api_key` | _(escape hatch override)_ | `LEDGER_API_KEY` |

`ledger config show` displays both the ledger config and the resolved `api_key`
source (showing which file it came from).

---

### 5. Pagination: auto-follow all pages, `--limit` to cap

**Decision:** `ledger trades` and `ledger orders` auto-follow cursor pagination.
A `--limit` flag caps the total rows (default 50, 0 = all pages).

**Rationale:** For human use, fetching all results is more useful than
requiring manual cursor management. For scripting, `--json` + `--limit` gives
full control.

---

### 6. Package layout

```
cmd/ledger-cli/
  main.go          root cobra command, version, init
  config.go        viper setup, sn config reading, lazy tenant resolution
  client.go        HTTP client, auth header injection
  output.go        PrintTable, PrintJSON helpers
  cmd_accounts.go  accounts list
  cmd_portfolio.go portfolio <account-id>
  cmd_positions.go positions <account-id>
  cmd_trades.go    trades <account-id>
  cmd_orders.go    orders <account-id>
  cmd_import.go    import <file>
  cmd_config.go    config show/set/get
```

Each command file registers itself via `init()` — same pattern as `sn`.

---

## Risks / Trade-offs

**[Risk] `sn` not installed on bot host** — bots that don't have `sn` installed
won't have `~/.config/sn/config.yaml`.
→ Mitigation: `LEDGER_API_KEY` env var provides a clean bypass; no dependency
on `sn` binary itself, only the config file path.

**[Risk] `sn` config file path changes** — if `sn` moves its config location
the read will silently fail and the key won't be found.
→ Mitigation: `LEDGER_API_KEY` override; clear error message when no API key
can be resolved from any source.

**[Risk] Ledger URL default hardcoded** — if the production URL changes the
default becomes stale.
→ Mitigation: override via `LEDGER_URL` or `ledger config set ledger_url`.

**[Risk] Pagination for large datasets** — auto-following all pages could be
slow for accounts with thousands of trades.
→ Mitigation: `--limit` defaults to 50; users opt-in to full fetch with
`--limit 0`.
