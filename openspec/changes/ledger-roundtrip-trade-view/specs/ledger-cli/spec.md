## MODIFIED Requirements

### Requirement: List trades
The CLI SHALL list trades for an account when `ledger trades list <account-id>` is run. It SHALL support: `--symbol`, `--side`, `--market-type`, `--start` (RFC3339), `--end` (RFC3339), `--limit` (default 50, 0 = all pages), and `--roundtrip`. It SHALL auto-follow cursor pagination up to the limit. With `--json` it SHALL print a JSON array of all fetched trades (or positions when `--roundtrip` is set). With `--roundtrip`, the data source switches to `GET /api/v1/accounts/{accountId}/positions?status=all` and the output columns change to the round-trip view.

#### Scenario: List trades default
- **WHEN** `ledger trades list live` is run
- **THEN** the CLI SHALL print the 50 most recent trades as a table with columns TRADE-ID, SYMBOL, SIDE, QTY, PRICE, FEE, MARKET, TIMESTAMP

#### Scenario: List trades with symbol filter
- **WHEN** `ledger trades list live --symbol BTC-USD` is run
- **THEN** the CLI SHALL return only BTC-USD trades

#### Scenario: List all trades by following pagination
- **WHEN** `ledger trades list live --limit 0` is run and there are 200 trades
- **THEN** the CLI SHALL follow all cursor pages and print all 200 trades

#### Scenario: List trades JSON output
- **WHEN** `ledger trades list live --json` is run
- **THEN** the CLI SHALL print a JSON array of the fetched trades

#### Scenario: Round-trip view table output
- **WHEN** `ledger trades list paper --roundtrip` is run
- **THEN** the CLI SHALL fetch positions (status=all) and print a table with columns RESULT, SYMBOL, DIR, SIZE, ENTRY, EXIT, P&L, P&L%, OPENED, CLOSED, EXIT-REASON
- **AND** closed positions SHALL show ✓ win or ✗ loss in the RESULT column based on whether realized_pnl > 0
- **AND** open positions SHALL show `open` in the RESULT column and dashes for EXIT, P&L, P&L%, CLOSED, EXIT-REASON

#### Scenario: Round-trip view follows all pages by default
- **WHEN** `ledger trades list paper --roundtrip` is run with no `--limit`
- **THEN** the CLI SHALL follow all cursor pages and display the complete position history (equivalent to `--limit 0` behaviour)

#### Scenario: Round-trip view JSON output
- **WHEN** `ledger trades list paper --roundtrip --json` is run
- **THEN** the CLI SHALL print a JSON array of position objects as returned by the positions endpoint

#### Scenario: Round-trip filters not applicable
- **WHEN** `ledger trades list paper --roundtrip --symbol BTC-USD` is run
- **THEN** the CLI SHALL ignore symbol/side/market-type/start/end filters (positions endpoint does not support them) and print a warning that filters are ignored in roundtrip mode
