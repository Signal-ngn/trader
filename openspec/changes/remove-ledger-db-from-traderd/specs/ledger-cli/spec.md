## MODIFIED Requirements

### Requirement: Config management
The CLI SHALL support `trader config show`, `trader config set <key> <value>`, and `trader config get <key>` to manage `~/.config/trader/config.yaml`. Valid writable keys are `trader_url`, `tenant_id`, `api_key`, `api_url`, `web_url`, `ingestion_url`, `nats_url`, `nats_creds_file`. `config show` SHALL display all keys with their resolved values and sources. `api_key` SHALL be masked (first 8 chars + `...`). The source column SHALL indicate `[env]`, `[trader]`, `[sn]`, or `[default]`.

The `api_url` key is now the primary endpoint used by all data commands. Its default value SHALL be `https://signalngn-api-potbdcvufa-ew.a.run.app`. The `trader_url` key is retained for `trader watch` only.

#### Scenario: Config show displays all keys with sources
- **WHEN** `trader config show` is run
- **THEN** the CLI SHALL print a table of all config keys, their resolved values, and sources
- **AND** `api_key` SHALL be masked (first 8 chars + `...`)
- **AND** the source column SHALL indicate whether the key came from `[sn]`, `[trader]`, `[env]`, or `[default]`

#### Scenario: Config show includes api_url with default
- **WHEN** `trader config show` is run on a fresh install
- **THEN** the table SHALL include `api_url` showing `https://signalngn-api-potbdcvufa-ew.a.run.app` with source `[default]`

#### Scenario: Config set writes new platform key
- **WHEN** `trader config set api_url https://my-api.example.com` is run
- **THEN** the value SHALL be written to `~/.config/trader/config.yaml`

#### Scenario: Config set writes ledger URL
- **WHEN** `trader config set trader_url https://my-traderd.example.com` is run
- **THEN** the value SHALL be written to `~/.config/trader/config.yaml`

#### Scenario: Config get unknown key
- **WHEN** `trader config get unknown_key` is run
- **THEN** the CLI SHALL print an error and exit non-zero
