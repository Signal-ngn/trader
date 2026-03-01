## ADDED Requirements

### Requirement: accounts balance set subcommand
The CLI SHALL provide a `ledger accounts balance set <account-id> <amount>` subcommand that calls `PUT /api/v1/accounts/{accountId}/balance` and prints a confirmation. It SHALL accept an optional `--currency` flag (default `USD`). With `--json` it SHALL print the raw JSON response. This command is used to set an initial balance or to correct the balance manually after broker reconciliation.

#### Scenario: Set balance table output
- **WHEN** `ledger accounts balance set live 50000` is run successfully
- **THEN** the CLI SHALL print a confirmation showing account ID, currency, and the stored amount

#### Scenario: Set balance with explicit currency
- **WHEN** `ledger accounts balance set live 40000 --currency EUR` is run successfully
- **THEN** the CLI SHALL call the API with `{"amount": 40000, "currency": "EUR"}` and print a confirmation

#### Scenario: Set balance JSON output
- **WHEN** `ledger accounts balance set live 50000 --json` is run
- **THEN** the CLI SHALL print the raw JSON response from the API

#### Scenario: Invalid amount argument
- **WHEN** `ledger accounts balance set live notanumber` is run
- **THEN** the CLI SHALL print an error and exit non-zero without calling the API

### Requirement: accounts balance get subcommand
The CLI SHALL provide a `ledger accounts balance get <account-id>` subcommand that calls `GET /api/v1/accounts/{accountId}/balance` and prints the current balance. It SHALL accept an optional `--currency` flag (default `USD`). With `--json` it SHALL print the raw JSON response. When the API returns HTTP 404 the CLI SHALL print `no balance set for <account-id>` and exit non-zero.

#### Scenario: Get balance table output
- **WHEN** `ledger accounts balance get live` is run and a USD balance of 48000 exists
- **THEN** the CLI SHALL print a summary showing account ID, currency, and amount (reflecting any automatic adjustments from ingestion)

#### Scenario: Get balance with explicit currency
- **WHEN** `ledger accounts balance get live --currency EUR` is run and a EUR balance exists
- **THEN** the CLI SHALL call the API with `?currency=EUR` and display the result

#### Scenario: Get balance JSON output
- **WHEN** `ledger accounts balance get live --json` is run
- **THEN** the CLI SHALL print the raw JSON response from the API

#### Scenario: Balance not set
- **WHEN** `ledger accounts balance get live` is run and no balance has been set
- **THEN** the CLI SHALL print `no balance set for live` and exit non-zero

## MODIFIED Requirements

### Requirement: accounts show subcommand
The CLI SHALL provide a `ledger accounts show <account-id>` subcommand that calls `GET /api/v1/accounts/{accountId}/stats` and renders a concise summary of the account's all-time aggregate statistics. The output SHALL display: account ID, total trades, closed trades, win count, loss count, win rate (as a percentage), total realized P&L, open positions, and current USD balance when set. With `--json` it SHALL print the raw JSON response from the stats endpoint.

#### Scenario: Show account stats table output
- **WHEN** `ledger accounts show paper` is run and the account has trade history
- **THEN** the CLI SHALL print a summary showing total trades, win count, loss count, win rate percentage, and total realized P&L

#### Scenario: Show account stats with balance
- **WHEN** `ledger accounts show paper` is run and the account has a USD balance set
- **THEN** the CLI SHALL include the balance in the printed summary

#### Scenario: Show account stats without balance
- **WHEN** `ledger accounts show paper` is run and no balance has been set for the account
- **THEN** the CLI SHALL omit the balance row from the summary or display `not set`

#### Scenario: Show account stats JSON output
- **WHEN** `ledger accounts show paper --json` is run
- **THEN** the CLI SHALL print the raw JSON object returned by `GET /api/v1/accounts/paper/stats`

#### Scenario: Show account not found
- **WHEN** `ledger accounts show nonexistent` is run and the API returns HTTP 404
- **THEN** the CLI SHALL print `account not found` and exit non-zero

#### Scenario: Show account with no trades
- **WHEN** `ledger accounts show paper` is run and the account exists but has zero trades
- **THEN** the CLI SHALL print the summary with all counts at zero and win rate at 0.0%
