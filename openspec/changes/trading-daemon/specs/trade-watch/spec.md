## ADDED Requirements

### Requirement: `trader trades watch` streams live trade events to stdout
The CLI SHALL provide a `trader trades watch <account-id>` subcommand that connects to `GET /api/v1/accounts/{accountId}/trades/stream` (SSE) and writes each trade event as a JSON object on a new line (JSONL) to stdout. The command SHALL block until interrupted (Ctrl-C / SIGTERM).

#### Scenario: Trade event written to stdout
- **WHEN** a trade is recorded for the watched account
- **THEN** the CLI SHALL write a single JSON object followed by a newline to stdout

#### Scenario: Command exits cleanly on interrupt
- **WHEN** the user sends SIGINT (Ctrl-C)
- **THEN** the CLI SHALL close the SSE connection and exit with code 0

#### Scenario: Server disconnects — CLI reconnects
- **WHEN** the SSE connection is dropped by the server
- **THEN** the CLI SHALL attempt to reconnect after 5 seconds and resume streaming

### Requirement: Trade watch event payload
Each JSONL line written by `trader trades watch` SHALL contain: `trade_id`, `account_id`, `symbol`, `side`, `quantity`, `price`, `fee`, `market_type`, `timestamp`, and optional strategy metadata fields (`strategy`, `confidence`, `stop_loss`, `take_profit`, `entry_reason`, `exit_reason`) when present.

#### Scenario: Full trade event payload
- **WHEN** a futures trade with strategy metadata is recorded
- **THEN** the JSON line SHALL include all base fields plus the non-null metadata fields

#### Scenario: Metadata fields omitted when null
- **WHEN** a trade has no strategy metadata
- **THEN** the JSON line SHALL not include metadata keys

### Requirement: Trade watch supports `--account` filter via argument
The `account-id` positional argument is required. Only trades for that account SHALL be streamed.

#### Scenario: Only matching account trades streamed
- **WHEN** `trader trades watch paper` is running and a trade is recorded for account `live`
- **THEN** the CLI SHALL NOT write the `live` account trade to stdout
