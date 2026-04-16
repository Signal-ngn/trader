## ADDED Requirements

### Requirement: trading list
The CLI SHALL provide a `trader trading list [account]` subcommand that calls `GET /config/trading` and renders a table with columns `ACCOUNT`, `EXCHANGE`, `PRODUCT`, `GRANULARITY`, `LONG`, `SHORT`, `SPOT`, `L-LEV`, `S-LEV`, `TREND`, `ENABLED`, `PARAMS`. When an account positional argument is given, the CLI SHALL pass `?account_id=<account>` to filter results. With `--json` it SHALL print the raw JSON array.

#### Scenario: List all trading configs
- **WHEN** `trader trading list` is run
- **THEN** the CLI SHALL call `GET /config/trading` without an account filter and render all configs in a table

#### Scenario: List filtered by account
- **WHEN** `trader trading list live` is run
- **THEN** the CLI SHALL call `GET /config/trading?account_id=live` and render only that account's configs

#### Scenario: Filter enabled only
- **WHEN** `trader trading list --enabled` is run
- **THEN** the CLI SHALL pass `?enabled=true` to the API

#### Scenario: List JSON output
- **WHEN** `trader trading list --json` is run
- **THEN** the CLI SHALL print the raw JSON array

---

### Requirement: trading get
The CLI SHALL provide a `trader trading get <account> <exchange> <product>` subcommand that calls `GET /config/trading/<exchange>/<product>?account_id=<account>` and renders a detail view. With `--json` it SHALL print the full JSON object.

#### Scenario: Get trading config
- **WHEN** `trader trading get live coinbase BTC-USD` is run
- **THEN** the CLI SHALL call `GET /config/trading/coinbase/BTC-USD?account_id=live` and render a key-value table

#### Scenario: Get JSON output
- **WHEN** `trader trading get live coinbase BTC-USD --json` is run
- **THEN** the CLI SHALL print the full JSON object

#### Scenario: Config not found
- **WHEN** `trader trading get live coinbase XYZ-USD` is run and the API returns 404
- **THEN** the CLI SHALL print an appropriate error and exit non-zero

---

### Requirement: trading set
The CLI SHALL provide a `trader trading set <account> <exchange> <product>` subcommand that creates or updates a trading config via `PUT /config/trading/<exchange>/<product>`. It SHALL first fetch the existing config to merge unset flags, then PUT the merged body. The account positional argument is required. Optional flags:

| Flag | Description |
|---|---|
| `--granularity` | Candle granularity (e.g. `ONE_HOUR`) |
| `--long` | Long strategies (comma-separated) |
| `--short` | Short strategies (comma-separated) |
| `--spot` | Spot strategies (comma-separated) |
| `--long-leverage` | Long leverage multiplier |
| `--short-leverage` | Short leverage multiplier |
| `--trend-filter` | Enable trend filter (shared default) |
| `--no-trend-filter` | Disable trend filter (shared default) |
| `--trend-filter-long` | Enable trend filter for long side only (overrides shared default) |
| `--no-trend-filter-long` | Disable trend filter for long side only |
| `--clear-trend-filter-long` | Clear long-side override; long side falls back to the shared default |
| `--trend-filter-short` | Enable trend filter for short side only |
| `--no-trend-filter-short` | Disable trend filter for short side only |
| `--clear-trend-filter-short` | Clear short-side override; short side falls back to the shared default |
| `--enable` | Enable the config |
| `--disable` | Disable the config |
| `--params` | Per-strategy params (shared default): `<strategy>:<key>=<value>` or `<strategy>:clear` (repeatable) |
| `--params-long` | Per-strategy params for long side only (same grammar as `--params`); merged into existing per-side map |
| `--params-short` | Per-strategy params for short side only (same grammar as `--params`); merged into existing per-side map |
| `--clear-params-long` | Clear all long-side param overrides; long side falls back to the shared `--params` |
| `--clear-params-short` | Clear all short-side param overrides; short side falls back to the shared `--params` |

Unset flags SHALL preserve the existing values from the fetched config. On success it SHALL render the updated config. With `--json` it SHALL print the full JSON response.

#### Scenario: Create new config
- **WHEN** `trader trading set live coinbase BTC-USD --granularity ONE_HOUR --spot ml_xgboost` is run and no config exists
- **THEN** the CLI SHALL PUT a new config with the given values and sensible defaults for unset fields

#### Scenario: Update merges with existing
- **WHEN** `trader trading set live coinbase BTC-USD --enable` is run and a config exists
- **THEN** the CLI SHALL fetch the existing config, set `enabled: true`, and PUT the merged result leaving all other fields unchanged

#### Scenario: Set strategy params
- **WHEN** `trader trading set live coinbase BTC-USD --params ml_xgboost:confidence=0.80 --params ml_xgboost:exit_confidence=0.40` is run
- **THEN** the request body SHALL include the merged `strategy_params` map

#### Scenario: Clear strategy params
- **WHEN** `trader trading set live coinbase BTC-USD --params ml_xgboost:clear` is run
- **THEN** the request body SHALL set `strategy_params.ml_xgboost` to `{}`

#### Scenario: Set per-side strategy params override
- **WHEN** `trader trading set live coinbase FLOKI-USD --params-short hts:uncertainty_cap=1.7` is run
- **THEN** the request body SHALL include `strategy_params_short` merged from the existing per-side map plus the new key, leaving `strategy_params` and `strategy_params_long` untouched
- **AND** at signal-processing time the engine SHALL apply `uncertainty_cap=1.7` to short-side `hts` evaluation only, while long-side evaluation continues to use the shared `strategy_params`

#### Scenario: Clear per-side strategy params
- **WHEN** `trader trading set live coinbase FLOKI-USD --clear-params-short` is run
- **THEN** the request body SHALL set `strategy_params_short` to `{}`, dropping the per-side override so the short side falls back to the shared `strategy_params`

#### Scenario: Set per-side trend filter override
- **WHEN** `trader trading set live coinbase BONK-USD --trend-filter-short --no-trend-filter-long` is run
- **THEN** the request body SHALL set `trend_filter_short: true` and `trend_filter_long: false`, leaving the shared `trend_filter` field unchanged

#### Scenario: Clear per-side trend filter override
- **WHEN** `trader trading set live coinbase BONK-USD --clear-trend-filter-long` is run
- **THEN** the request body SHALL set `trend_filter_long: null`, so the long side falls back to the shared `trend_filter` value

#### Scenario: Invalid params format
- **WHEN** `--params` is provided with an invalid format (missing colon or equals)
- **THEN** the CLI SHALL print a parse error and exit non-zero without calling the API

#### Scenario: Set JSON output
- **WHEN** `trader trading set live coinbase BTC-USD --enable --json` is run
- **THEN** the CLI SHALL print the full updated config JSON

---

### Requirement: trading delete
The CLI SHALL provide a `trader trading delete <account> <exchange> <product>` subcommand that calls `DELETE /config/trading/<exchange>/<product>?account_id=<account>`. On success it SHALL print `Deleted trading config for <exchange>/<product>`.

#### Scenario: Delete trading config
- **WHEN** `trader trading delete live coinbase BTC-USD` is run
- **THEN** the CLI SHALL call `DELETE /config/trading/coinbase/BTC-USD?account_id=live` and print a confirmation

#### Scenario: Config not found
- **WHEN** `trader trading delete live coinbase XYZ-USD` is run and the API returns 404
- **THEN** the CLI SHALL print an appropriate error and exit non-zero
