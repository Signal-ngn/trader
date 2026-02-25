## ADDED Requirements

### Requirement: API key resolution
The CLI SHALL resolve the API key using the following priority order:
1. `LEDGER_API_KEY` environment variable
2. `api_key` in `~/.config/ledger/config.yaml` (manual override)
3. `api_key` in `~/.config/sn/config.yaml` (written by `sn auth login`)

If no API key can be found from any source the CLI SHALL print a clear error
message directing the user to run `sn auth login` or set `LEDGER_API_KEY`,
and exit non-zero.

#### Scenario: Key resolved from sn config
- **WHEN** `LEDGER_API_KEY` is not set and `~/.config/sn/config.yaml` contains `api_key`
- **THEN** the CLI SHALL use that key for all requests

#### Scenario: LEDGER_API_KEY overrides sn config
- **WHEN** `LEDGER_API_KEY` is set in the environment
- **THEN** the CLI SHALL use that value regardless of any config file contents

#### Scenario: No API key found
- **WHEN** no API key can be resolved from any source
- **THEN** the CLI SHALL print an error referencing `sn auth login` and exit non-zero

---

### Requirement: Lazy tenant ID resolution
The CLI SHALL resolve `tenant_id` on the first command that requires it. If
`LEDGER_TENANT_ID` is set it SHALL use that value. Otherwise if `tenant_id` is
cached in `~/.config/ledger/config.yaml` it SHALL use that. Otherwise it SHALL
call `GET /auth/resolve` with the resolved API key, write the returned
`tenant_id` to `~/.config/ledger/config.yaml`, and proceed.

#### Scenario: Tenant ID resolved from cache
- **WHEN** `tenant_id` is present in `~/.config/ledger/config.yaml`
- **THEN** the CLI SHALL use the cached value without calling `/auth/resolve`

#### Scenario: Tenant ID resolved via auth/resolve
- **WHEN** no cached `tenant_id` exists and no `LEDGER_TENANT_ID` env var is set
- **THEN** the CLI SHALL call `GET /auth/resolve`, cache the result, and proceed

#### Scenario: Tenant ID resolution fails
- **WHEN** `/auth/resolve` returns 401
- **THEN** the CLI SHALL print `authentication failed — check your API key` and exit non-zero

---

### Requirement: accounts show subcommand
The CLI SHALL provide a `ledger accounts show <account-id>` subcommand that calls `GET /api/v1/accounts/{accountId}/stats` and renders a concise summary of the account's all-time aggregate statistics. The output SHALL display: account ID, total trades, closed trades, win count, loss count, win rate (as a percentage), and total realized P&L. With `--json` it SHALL print the raw JSON response from the stats endpoint.

#### Scenario: Show account stats table output
- **WHEN** `ledger accounts show paper` is run and the account has trade history
- **THEN** the CLI SHALL print a summary showing total trades, win count, loss count, win rate percentage, and total realized P&L

#### Scenario: Show account stats JSON output
- **WHEN** `ledger accounts show paper --json` is run
- **THEN** the CLI SHALL print the raw JSON object returned by `GET /api/v1/accounts/paper/stats`

#### Scenario: Show account not found
- **WHEN** `ledger accounts show nonexistent` is run and the API returns HTTP 404
- **THEN** the CLI SHALL print `account not found` and exit non-zero

#### Scenario: Show account with no trades
- **WHEN** `ledger accounts show paper` is run and the account exists but has zero trades
- **THEN** the CLI SHALL print the summary with all counts at zero and win rate at 0.0%

---

### Requirement: List accounts
The CLI SHALL list all accounts for the authenticated tenant when
`ledger accounts list` is run. Output SHALL include account ID, name, type,
and created-at timestamp. With `--json` it SHALL print the raw JSON array.

#### Scenario: List accounts table output
- **WHEN** `ledger accounts list` is run with a valid API key
- **THEN** the CLI SHALL print a table with columns ID, NAME, TYPE, CREATED

#### Scenario: List accounts JSON output
- **WHEN** `ledger accounts list --json` is run
- **THEN** the CLI SHALL print the raw JSON array returned by the API

#### Scenario: List accounts empty
- **WHEN** `ledger accounts list` is run and the tenant has no accounts
- **THEN** the CLI SHALL print an empty table (headers only)

---

### Requirement: Portfolio summary
The CLI SHALL display open positions and total realized P&L for an account when
`ledger portfolio <account-id>` is run. It SHALL exit with a clear error if the
account does not exist (HTTP 404).

#### Scenario: Portfolio with open positions
- **WHEN** `ledger portfolio live` is run and the account has open positions
- **THEN** the CLI SHALL print a positions table followed by the total realized P&L

#### Scenario: Portfolio account not found
- **WHEN** `ledger portfolio nonexistent` is run and the API returns 404
- **THEN** the CLI SHALL print `account not found` and exit non-zero

#### Scenario: Portfolio JSON output
- **WHEN** `ledger portfolio live --json` is run
- **THEN** the CLI SHALL print the raw JSON response

---

### Requirement: List positions
The CLI SHALL list positions for an account when `ledger positions <account-id>`
is run. It SHALL support a `--status open|closed|all` flag (default `open`).
With `--json` it SHALL print the raw JSON array.

#### Scenario: List open positions
- **WHEN** `ledger positions live` is run (default status)
- **THEN** the CLI SHALL print a table of open positions with SYMBOL, SIDE, QTY, AVG-ENTRY, COST-BASIS, REALIZED-PNL, STATUS columns

#### Scenario: List closed positions
- **WHEN** `ledger positions live --status closed` is run
- **THEN** the CLI SHALL return only closed positions

#### Scenario: List all positions JSON
- **WHEN** `ledger positions live --status all --json` is run
- **THEN** the CLI SHALL print the raw JSON array of all positions

---

### Requirement: List trades
The CLI SHALL list trades for an account when `ledger trades <account-id>` is
run. It SHALL support: `--symbol`, `--side`, `--market-type`, `--start`
(RFC3339), `--end` (RFC3339), `--limit` (default 50, 0 = all pages). It SHALL
auto-follow cursor pagination up to the limit. With `--json` it SHALL print
a JSON array of all fetched trades.

#### Scenario: List trades default
- **WHEN** `ledger trades live` is run
- **THEN** the CLI SHALL print the 50 most recent trades as a table

#### Scenario: List trades with symbol filter
- **WHEN** `ledger trades live --symbol BTC-USD` is run
- **THEN** the CLI SHALL return only BTC-USD trades

#### Scenario: List all trades by following pagination
- **WHEN** `ledger trades live --limit 0` is run and there are 200 trades
- **THEN** the CLI SHALL follow all cursor pages and print all 200 trades

#### Scenario: List trades JSON output
- **WHEN** `ledger trades live --json` is run
- **THEN** the CLI SHALL print a JSON array of the fetched trades

---

### Requirement: List orders
The CLI SHALL list orders for an account when `ledger orders <account-id>` is
run. It SHALL support: `--status`, `--symbol`, `--limit` (default 50). It
SHALL auto-follow cursor pagination up to the limit. With `--json` it SHALL
print a JSON array.

#### Scenario: List orders default
- **WHEN** `ledger orders live` is run
- **THEN** the CLI SHALL print the 50 most recent orders as a table with ORDER-ID, SYMBOL, SIDE, TYPE, REQ-QTY, FILLED-QTY, AVG-FILL, STATUS, CREATED columns

#### Scenario: List open orders only
- **WHEN** `ledger orders live --status open` is run
- **THEN** the CLI SHALL return only open orders

---

### Requirement: Import trades
The CLI SHALL POST a JSON file of historic trades to `/api/v1/import` when
`ledger import <file.json>` is run. It SHALL print total, inserted, duplicate,
and error counts. With `--json` it SHALL print the full import response. It
SHALL exit non-zero if any trade errors occurred.

#### Scenario: Successful import
- **WHEN** `ledger import trades.json` is run with a valid JSON file
- **THEN** the CLI SHALL print `Total: N  Inserted: N  Duplicates: N  Errors: 0`

#### Scenario: Import with errors
- **WHEN** `ledger import trades.json` is run and some trades fail validation
- **THEN** the CLI SHALL print the summary including error count and exit non-zero

#### Scenario: File not found
- **WHEN** `ledger import missing.json` is run and the file does not exist
- **THEN** the CLI SHALL print a file-not-found error and exit non-zero

---

### Requirement: Config management
The CLI SHALL support `ledger config show`, `ledger config set <key> <value>`,
and `ledger config get <key>` to manage `~/.config/ledger/config.yaml`. Valid
writable keys are `ledger_url`, `tenant_id`, `api_key`. `config show` SHALL
also display the resolved `api_key` source (sn config / ledger config / env)
and mask the key value.

#### Scenario: Config show displays all keys with sources
- **WHEN** `ledger config show` is run
- **THEN** the CLI SHALL print a table of all config keys, their resolved values, and sources
- **AND** `api_key` SHALL be masked (first 8 chars + `...`)
- **AND** the source column SHALL indicate whether the key came from `[sn]`, `[ledger]`, `[env]`, or `[default]`

#### Scenario: Config set writes to ledger config file
- **WHEN** `ledger config set ledger_url https://my-ledger.example.com` is run
- **THEN** the value SHALL be written to `~/.config/ledger/config.yaml`

#### Scenario: Config get unknown key
- **WHEN** `ledger config get unknown_key` is run
- **THEN** the CLI SHALL print an error and exit non-zero

---

### Requirement: Global flags
The CLI SHALL support `--ledger-url` as a persistent flag on the root command
to override the configured ledger URL for a single invocation. It SHALL also
support `--json` on all read commands.

#### Scenario: Override URL via flag
- **WHEN** `ledger --ledger-url http://localhost:8080 accounts list` is run
- **THEN** all HTTP calls SHALL use `http://localhost:8080` as the base URL

#### Scenario: LEDGER_URL env var
- **WHEN** `LEDGER_URL=http://localhost:8080 ledger accounts list` is run
- **THEN** all HTTP calls SHALL use `http://localhost:8080` as the base URL
