## ADDED Requirements

### Requirement: Account stats endpoint
The system SHALL expose a `GET /api/v1/accounts/{accountId}/stats` endpoint that returns pre-computed all-time aggregate statistics for the specified account, computed directly from the database. The response SHALL include: `total_trades` (count of all trades), `closed_trades` (count of trades with `exit_reason` set), `win_count` (closed trades with `realized_pnl > 0`), `loss_count` (closed trades with `realized_pnl <= 0`), `win_rate` (win_count / closed_trades as a float 0–1, or 0 if no closed trades), `total_realized_pnl` (sum of all `realized_pnl` across all trades), and `open_positions` (count of positions with status = 'open').

#### Scenario: Account with trades and closed positions
- **WHEN** `GET /api/v1/accounts/paper/stats` is called and the account has 10 total trades, 6 with `exit_reason` set, 4 of which have `realized_pnl > 0`, and 2 open positions
- **THEN** the system SHALL return HTTP 200 with `{"total_trades":10,"closed_trades":6,"win_count":4,"loss_count":2,"win_rate":0.6667,"total_realized_pnl":<sum>,"open_positions":2}`

#### Scenario: Account with no trades
- **WHEN** `GET /api/v1/accounts/paper/stats` is called and the account exists but has zero trades
- **THEN** the system SHALL return HTTP 200 with all counts zero and `win_rate` 0

#### Scenario: Account not found
- **WHEN** `GET /api/v1/accounts/nonexistent/stats` is called and no such account exists for the tenant
- **THEN** the system SHALL return HTTP 404 with an error message

#### Scenario: Requires authentication
- **WHEN** `GET /api/v1/accounts/paper/stats` is called without an `Authorization` header
- **THEN** the system SHALL return HTTP 401

#### Scenario: Tenant isolation
- **WHEN** two tenants each have an account named `"paper"` with different trade histories
- **THEN** each tenant's stats request SHALL return only their own account's aggregated data
