## ADDED Requirements

### Requirement: Round-trip view sourced from positions
The ledger service SHALL provide a round-trip trade view by returning closed (and open) positions from `GET /api/v1/accounts/{accountId}/positions?status=all`. Each closed position represents one complete round-trip trade: the position row contains the entry price (`avg_entry_price`), exit price (`exit_price`), direction (`side`), size (`cost_basis`), realized P&L (`realized_pnl`), open time (`opened_at`), close time (`closed_at`), and exit reason (`exit_reason`). Open positions appear as incomplete (in-progress) round-trips.

This is the server-authoritative paired view — no client-side matching heuristic is needed or correct.

#### Scenario: Closed position is a complete round-trip
- **WHEN** a position has status `closed` with `avg_entry_price`, `exit_price`, `realized_pnl`, `opened_at`, `closed_at`, and `exit_reason` set
- **THEN** it represents one complete round-trip and SHALL contain all fields needed to display: symbol, direction, size, entry price, exit price, P&L, P&L%, open time, close time, exit reason

#### Scenario: Open position is an incomplete round-trip
- **WHEN** a position has status `open` with `avg_entry_price` set but no `exit_price` or `closed_at`
- **THEN** it represents an in-progress trade with entry data available but no exit data yet

#### Scenario: Two concurrent same-symbol positions are distinct round-trips
- **WHEN** an account has two closed positions for the same symbol opened at different times
- **THEN** each appears as a separate row in the round-trip view with its own entry/exit prices and P&L
