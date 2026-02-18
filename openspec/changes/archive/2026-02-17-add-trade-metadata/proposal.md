## Why

The trading bot tracks strategy context (strategy name, entry/exit reasons, confidence scores, stop loss/take profit levels) locally but the ledger has no fields for this metadata. This forces clients to reverse-engineer exit prices from `entry_price ± (realized_pnl / qty)` — losing precision to rounding — and makes post-trade analysis (e.g., "do high-confidence trades perform better?") impossible without cross-referencing local bot state files.

## What Changes

- Add **strategy metadata** to trades: strategy name and signal reason text at the time of the trade (entry reason on buys, exit reason on sells)
- Add **confidence score** (0–1) to trades: the signal confidence at entry, carried forward to the closing trade for analysis
- Add **exit price** to positions: stored explicitly when a position closes, eliminating the lossy reverse-engineered calculation
- Add **stop loss / take profit prices** to positions: tracked per position so the ledger becomes the single source of truth, replacing the bot's local `.position_state.json`
- Add **exit reason** to positions: the reason text when a position is closed (e.g., "stop loss hit", "take profit reached", "manual close")
- All new fields are **optional** (nullable) to maintain backward compatibility — existing trade events without these fields continue to work unchanged

## Capabilities

### New Capabilities

_None — all changes extend existing capabilities._

### Modified Capabilities

- `trade-ingestion`: Accept new optional fields in trade events: `strategy`, `entry_reason`, `exit_reason`, `confidence`, `stop_loss`, `take_profit`
- `portfolio-tracking`: Store exit price, exit reason, stop loss, and take profit on positions; carry confidence from entry trade
- `order-history`: Include new trade metadata fields in trade storage and query results
- `rest-api`: Expose new fields in trade and position API responses

## Impact

- **Schema**: New database migration adding nullable columns to `ledger_trades` and `ledger_positions`
- **NATS contract**: Trade event payload gains optional fields — purely additive, no breaking change
- **API responses**: Trade and position JSON responses gain new fields — additive, no breaking change
- **Bot integration**: The trading bot can start sending the new fields immediately; existing events without them are accepted as-is
- **Eliminates**: Bot's local `.position_state.json` for stop loss/take profit tracking (bot can migrate to reading from ledger)
