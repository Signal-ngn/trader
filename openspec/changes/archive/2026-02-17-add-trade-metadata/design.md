## Context

The ledger service ingests trades via NATS and maintains positions, but stores no strategy-level metadata. The trading bot tracks strategy name, signal reasons, confidence scores, and stop loss/take profit levels locally (in `.position_state.json`), making post-trade analysis require cross-referencing external state. Exit price is reverse-engineered from `entry_price ± (realized_pnl / qty)`, which loses precision to floating-point rounding.

The codebase follows a clean layered structure: NATS `TradeEvent` → `domain.Trade` → `store.InsertTrade` + `store.UpsertPosition`. All new fields flow through this same pipeline.

## Goals / Non-Goals

**Goals:**
- Add strategy metadata fields to trades and positions with full backward compatibility
- Store exit price explicitly on closed positions, eliminating the lossy computation
- Store stop loss / take profit on positions so the ledger replaces local bot state
- Expose all new fields in REST API responses

**Non-Goals:**
- Updating stop loss / take profit in real-time via a separate NATS subject (future work — for now they're set at entry and carried through)
- Analytics or aggregation queries over strategy metadata (e.g., "average PnL by strategy")
- Modifying the position rebuild logic to reconstruct stop loss / take profit from trade history (these are point-in-time values that only make sense from the original trade event)

## Decisions

### 1. New fields on `ledger_trades` table

Add six nullable columns to `ledger_trades`:

| Column | Type | Description |
|---|---|---|
| `strategy` | `TEXT` | Strategy name (e.g., "macd-rsi-v2") |
| `entry_reason` | `TEXT` | Signal reason at entry (e.g., "MACD bullish crossover, RSI 42") |
| `exit_reason` | `TEXT` | Reason for exit (e.g., "stop loss hit", "take profit reached") |
| `confidence` | `DOUBLE PRECISION` | Signal confidence 0–1 at time of trade |
| `stop_loss` | `DOUBLE PRECISION` | Stop loss price at time of trade |
| `take_profit` | `DOUBLE PRECISION` | Take profit price at time of trade |

**Rationale:** All fields are nullable (`*string`, `*float64` in Go) because existing trade events don't include them and must continue working. Storing them on trades rather than only on positions preserves the per-trade signal context — a position may accumulate multiple entries with different reasons/confidence.

### 2. New fields on `ledger_positions` table

Add five nullable columns to `ledger_positions`:

| Column | Type | Description |
|---|---|---|
| `exit_price` | `DOUBLE PRECISION` | Actual exit price when position closes |
| `exit_reason` | `TEXT` | Why the position was closed |
| `stop_loss` | `DOUBLE PRECISION` | Current stop loss price |
| `take_profit` | `DOUBLE PRECISION` | Current take profit price |
| `confidence` | `DOUBLE PRECISION` | Signal confidence from entry trade |

**Rationale:** `exit_price` and `exit_reason` are set only on close. `stop_loss`, `take_profit`, and `confidence` are copied from the opening trade to the position. On subsequent trades that increase the position, stop loss / take profit are updated if the new trade provides them (using `COALESCE` to keep existing values when the new trade omits them).

### 3. Field flow through the pipeline

```
TradeEvent (NATS JSON)
  → TradeEvent.Validate() — no changes needed (new fields are optional)
  → TradeEvent.ToDomain() — maps new fields to domain.Trade
  → store.InsertTrade() — persists new trade columns
  → store.UpsertPosition() — sets position metadata:
      - On open: copy strategy, confidence, stop_loss, take_profit from trade
      - On increase: update stop_loss, take_profit if provided
      - On close: set exit_price = trade.Price, exit_reason = trade.ExitReason
```

**Rationale:** This follows the existing pipeline exactly. No new code paths, just wider structs and queries.

### 4. Migration strategy

A single new migration (`002_add_trade_metadata.up.sql`) adds all columns with `ALTER TABLE ... ADD COLUMN`. All columns are nullable with no defaults, so the migration is instant (no table rewrite) on PostgreSQL.

**Rationale:** Simple `ADD COLUMN ... NULL` is an online DDL operation in PostgreSQL — no locks beyond brief `ACCESS EXCLUSIVE` for catalog update, no data rewrite. One migration for all fields keeps it atomic.

### 5. Position rebuild behavior

The `RebuildPositions` function replays trades to reconstruct positions. After this change, rebuilt positions will carry `confidence`, `stop_loss`, and `take_profit` from the first trade and `exit_price` / `exit_reason` from the closing trade — matching what incremental processing produces.

`strategy` is not stored on positions (it's per-trade metadata), so rebuild is unaffected for that field.

**Rationale:** Since the new trade fields are persisted, rebuild can reconstruct position metadata from trade history. No separate handling needed.

## Risks / Trade-offs

- **Stop loss / take profit are snapshots, not live-updated** — The ledger only receives these values when a trade event arrives. If the bot adjusts SL/TP between trades, the ledger won't reflect it until the next trade. This is acceptable for now; a future change could add a dedicated NATS subject for SL/TP updates.
  → Mitigation: Document this as a known limitation. The bot can send a synthetic "update" trade event if needed.

- **Confidence on position is from entry only** — For positions built up over multiple buys, the confidence reflects the first entry. Weighted-average confidence across entries was considered but adds complexity for unclear analytical value.
  → Mitigation: Per-trade confidence is always available for detailed analysis. Position-level confidence is a convenience field.

- **Nullable fields increase scan verbosity** — Every SQL scan site (ListPositions, TradesForRebuild, ListTrades) needs to handle the new nullable columns. This is mechanical but touches several functions.
  → Mitigation: All scan sites follow the same pattern already (for leverage/margin/liquidation_price). The change is repetitive but low-risk.

## Open Questions

_None — scope and approach are clear._
