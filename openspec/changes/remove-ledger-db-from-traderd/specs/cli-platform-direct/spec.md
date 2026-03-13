## ADDED Requirements

### Requirement: CLI data commands call the platform API directly
All CLI data commands (`trades`, `positions`, `portfolio`, `accounts`, `balance get`, `balance set`, `import`, `trades delete`, `accounts stats`) SHALL call the platform API directly using `PlatformClient` and the `api_url` config value. They SHALL NOT route through the traderd REST API. The `trader watch` command SHALL continue to connect to the traderd SSE endpoint via `trader_url`.

#### Scenario: Trade list calls platform API
- **WHEN** `trader trades list` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts/{id}/trades` with `Authorization: Bearer {api_key}` and display the results

#### Scenario: Position list calls platform API
- **WHEN** `trader positions list` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts/{id}/positions` with `Authorization: Bearer {api_key}` and display the results

#### Scenario: Portfolio calls platform API
- **WHEN** `trader portfolio` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts/{id}/portfolio` with `Authorization: Bearer {api_key}` and display the results

#### Scenario: Accounts list calls platform API
- **WHEN** `trader accounts list` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts` with `Authorization: Bearer {api_key}` and display the results

#### Scenario: Account stats calls platform API
- **WHEN** `trader accounts stats` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts/{id}/stats` with `Authorization: Bearer {api_key}` and display the results

#### Scenario: Balance get calls platform API
- **WHEN** `trader balance get` is run
- **THEN** the CLI SHALL call `GET {api_url}/api/v1/accounts/{id}/balance` with `Authorization: Bearer {api_key}`

#### Scenario: Balance set calls platform API
- **WHEN** `trader balance set <amount>` is run
- **THEN** the CLI SHALL call `PUT {api_url}/api/v1/accounts/{id}/balance` with `{"balance": <amount>}` and `Authorization: Bearer {api_key}`

#### Scenario: Trade delete calls platform API
- **WHEN** `trader trades delete <tradeId>` is run
- **THEN** the CLI SHALL call `DELETE {api_url}/api/v1/trades/{tradeId}` with `Authorization: Bearer {api_key}`

#### Scenario: trader watch still uses trader_url
- **WHEN** `trader watch` is run
- **THEN** the CLI SHALL connect to the SSE endpoint at `{trader_url}/api/v1/accounts/{id}/trades/stream` as before

---

### Requirement: CLI import calls platform API
The `trader import` command SHALL submit trades to the platform API one trade at a time using `POST {api_url}/api/v1/trades`. If the platform returns 409 for a trade (duplicate), the CLI SHALL skip it and continue. The CLI SHALL print a summary of how many trades were submitted, skipped (409), and failed.

#### Scenario: Import succeeds for all trades
- **WHEN** `trader import <file>` is run and all trades are new
- **THEN** the CLI SHALL POST each trade to the platform API and report the count submitted

#### Scenario: Import skips duplicates
- **WHEN** `trader import <file>` is run and some trades are already recorded (platform returns 409)
- **THEN** the CLI SHALL skip those trades, count them as skipped, and continue with the rest

#### Scenario: Import summary shown
- **WHEN** `trader import <file>` completes
- **THEN** the CLI SHALL print a summary line with counts: submitted, skipped, failed

---

### Requirement: Missing api_url is a startup error
If `api_url` cannot be resolved (no env var, no config value, no default), CLI data commands that use the platform API SHALL print a clear error and exit non-zero.

#### Scenario: api_url not configured
- **WHEN** a CLI data command is run and `api_url` is not set in the environment or config
- **THEN** the CLI SHALL print an error message referencing `trader config set api_url <url>` and exit non-zero
