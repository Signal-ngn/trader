## ADDED Requirements

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
