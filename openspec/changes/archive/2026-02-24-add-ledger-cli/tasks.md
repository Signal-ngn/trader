## 1. Project scaffold

- [x] 1.1 Create `cmd/ledger-cli/` directory
- [x] 1.2 Add `github.com/spf13/cobra`, `github.com/spf13/viper`, `github.com/olekukonko/tablewriter` to `go.mod` via `go get`
- [x] 1.3 Create `cmd/ledger-cli/main.go`: root cobra command (`ledger`), `--ledger-url` and `--json` persistent flags, `version` var, `SilenceUsage`/`SilenceErrors`
- [x] 1.4 Create `cmd/ledger-cli/output.go`: `PrintTable`, `PrintJSON`, `fmtFloat`, `fmtTime` helpers

## 2. Config and auth resolution

- [x] 2.1 Create `cmd/ledger-cli/config.go`: viper setup with `LEDGER_` env prefix; ledger config file at `~/.config/ledger/config.yaml` (writable); sn config at `~/.config/sn/config.yaml` (read-only second viper instance)
- [x] 2.2 Implement `resolveAPIKey()`: checks `LEDGER_API_KEY` env → ledger config `api_key` → sn config `api_key`; returns error with `sn auth login` hint if nothing found
- [x] 2.3 Implement `resolveTenantID(client)`: checks `LEDGER_TENANT_ID` env → ledger config `tenant_id` → calls `GET /auth/resolve`, caches result in ledger config
- [x] 2.4 Implement `writeConfigValue`, `maskValue` helpers (same pattern as `sn`)
- [x] 2.5 Write unit tests for `resolveAPIKey` priority order (env beats ledger beats sn)
- [x] 2.6 Write unit tests for `resolveTenantID` (cache hit, cache miss → resolve, 401 error)

## 3. HTTP client

- [x] 3.1 Create `cmd/ledger-cli/client.go`: `Client` struct with `LedgerURL`, `APIKey`; `do()` method injecting `Authorization: Bearer <api_key>`; `Get`, `Post` helpers; `ledgerURL(path, params)` builder
- [x] 3.2 `newClient()` calls `resolveAPIKey()` and `resolveTenantID()`, exits non-zero on error

## 4. Config command

- [x] 4.1 Create `cmd/ledger-cli/cmd_config.go` with `config show`, `config set <key> <value>`, `config get <key>` subcommands
- [x] 4.2 `config show` prints table of all keys with masked values and source labels (`[env]`, `[ledger]`, `[sn]`, `[default]`)
- [x] 4.3 `config set` validates key is in allowed list (`ledger_url`, `tenant_id`, `api_key`), writes to ledger config file
- [x] 4.4 `config get` validates key, prints raw resolved value

## 5. Accounts command

- [x] 5.1 Create `cmd/ledger-cli/cmd_accounts.go` with `accounts list` subcommand
- [x] 5.2 Calls `GET /api/v1/accounts`; prints table with ID, NAME, TYPE, CREATED columns
- [x] 5.3 `--json` prints raw JSON array; empty result prints headers only

## 6. Portfolio command

- [x] 6.1 Create `cmd/ledger-cli/cmd_portfolio.go` with `portfolio <account-id>` command
- [x] 6.2 Calls `GET /api/v1/accounts/{accountId}/portfolio`; prints positions table then `Total Realized P&L: <value>`
- [x] 6.3 Handles HTTP 404 with `account not found` message and non-zero exit
- [x] 6.4 `--json` prints raw JSON response

## 7. Positions command

- [x] 7.1 Create `cmd/ledger-cli/cmd_positions.go` with `positions <account-id>` command
- [x] 7.2 Calls `GET /api/v1/accounts/{accountId}/positions?status=<status>`; `--status` flag defaults to `open`
- [x] 7.3 Prints table with SYMBOL, SIDE, QTY, AVG-ENTRY, COST-BASIS, REALIZED-PNL, STATUS columns
- [x] 7.4 `--json` prints raw JSON array

## 8. Trades command

- [x] 8.1 Create `cmd/ledger-cli/cmd_trades.go` with `trades <account-id>` command
- [x] 8.2 Implement flags: `--symbol`, `--side`, `--market-type`, `--start`, `--end`, `--limit` (default 50, 0 = all)
- [x] 8.3 Implement cursor pagination loop: fetch page, append results, follow `next_cursor` until exhausted or limit reached
- [x] 8.4 Prints table with TRADE-ID, SYMBOL, SIDE, QTY, PRICE, FEE, MARKET-TYPE, TIMESTAMP columns
- [x] 8.5 `--json` prints JSON array of all fetched trades (assembled across pages)

## 9. Orders command

- [x] 9.1 Create `cmd/ledger-cli/cmd_orders.go` with `orders <account-id>` command
- [x] 9.2 Implement flags: `--status`, `--symbol`, `--limit` (default 50)
- [x] 9.3 Implement cursor pagination loop (same pattern as trades)
- [x] 9.4 Prints table with ORDER-ID, SYMBOL, SIDE, TYPE, REQ-QTY, FILLED-QTY, AVG-FILL, STATUS, CREATED columns
- [x] 9.5 `--json` prints JSON array of all fetched orders

## 10. Import command

- [x] 10.1 Create `cmd/ledger-cli/cmd_import.go` with `import <file>` command
- [x] 10.2 Reads and validates the file exists; reads raw JSON bytes
- [x] 10.3 POSTs to `POST /api/v1/import` with file contents as body
- [x] 10.4 Prints `Total: N  Inserted: N  Duplicates: N  Errors: N`
- [x] 10.5 Exits non-zero if `errors > 0`; `--json` prints full import response JSON

## 11. Build and smoke test

- [x] 11.1 Run `go build ./cmd/ledger-cli` and fix any compilation errors
- [x] 11.2 Write unit tests for pagination loop (mock HTTP server returning two pages)
- [x] 11.3 Manually smoke-test against production: `ledger accounts list`, `ledger portfolio live`
- [x] 11.4 Add `ledger-cli` build target to `Taskfile.yaml`
