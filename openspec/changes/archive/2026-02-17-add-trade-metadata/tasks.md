## 1. Database Migration

- [x] 1.1 Create `migrations/002_add_trade_metadata.up.sql` adding nullable columns to `ledger_trades`: `strategy TEXT`, `entry_reason TEXT`, `exit_reason TEXT`, `confidence DOUBLE PRECISION`, `stop_loss DOUBLE PRECISION`, `take_profit DOUBLE PRECISION`
- [x] 1.2 In the same migration, add nullable columns to `ledger_positions`: `exit_price DOUBLE PRECISION`, `exit_reason TEXT`, `stop_loss DOUBLE PRECISION`, `take_profit DOUBLE PRECISION`, `confidence DOUBLE PRECISION`
- [x] 1.3 Copy the migration to `internal/store/migrations/002_add_trade_metadata.up.sql` (embedded migrations)

## 2. Domain Types

- [x] 2.1 Add metadata fields to `domain.Trade`: `Strategy *string`, `EntryReason *string`, `ExitReason *string`, `Confidence *float64`, `StopLoss *float64`, `TakeProfit *float64` (all pointer types with `omitempty` JSON tags)
- [x] 2.2 Add metadata fields to `domain.Position`: `ExitPrice *float64`, `ExitReason *string`, `StopLoss *float64`, `TakeProfit *float64`, `Confidence *float64` (all pointer types with `omitempty` JSON tags)

## 3. Trade Ingestion

- [x] 3.1 Add metadata fields to `ingest.TradeEvent` struct: `Strategy *string`, `EntryReason *string`, `ExitReason *string`, `Confidence *float64`, `StopLoss *float64`, `TakeProfit *float64` (all `omitempty` JSON)
- [x] 3.2 Update `TradeEvent.ToDomain()` to map the new fields to `domain.Trade`
- [x] 3.3 Update `ingest.TradeEvent` tests to cover events with and without metadata fields

## 4. Trade Store

- [x] 4.1 Update `store.InsertTrade` INSERT statement to include the 6 new trade columns
- [x] 4.2 Update `store.ListTrades` SELECT and Scan to include the 6 new trade columns
- [x] 4.3 Update `store.TradesForRebuild` SELECT and Scan to include the 6 new trade columns

## 5. Position Store

- [x] 5.1 Update `store.upsertSpotPosition` — on new position: set stop_loss, take_profit, confidence from trade
- [x] 5.2 Update `store.upsertSpotPosition` — on position increase (buy): update stop_loss and take_profit with COALESCE
- [x] 5.3 Update `store.upsertSpotPosition` — on full close: set exit_price to trade price and exit_reason from trade
- [x] 5.4 Update `store.upsertFuturesPosition` — on new position: set stop_loss, take_profit, confidence from trade
- [x] 5.5 Update `store.upsertFuturesPosition` — on position increase: update stop_loss and take_profit with COALESCE
- [x] 5.6 Update `store.upsertFuturesPosition` — on full close: set exit_price to trade price and exit_reason from trade
- [x] 5.7 Update `store.ListPositions` SELECT and Scan to include the 5 new position columns
- [x] 5.8 Update `store.GetPortfolioSummary` to include new position fields (flows through ListPositions)

## 6. REST API Responses

- [x] 6.1 Verify trade list endpoint returns new metadata fields (omitted when NULL via `omitempty` — no handler changes needed, just confirm domain struct tags are correct)
- [x] 6.2 Verify position list endpoint returns new metadata fields
- [x] 6.3 Verify portfolio summary endpoint returns new position metadata fields

## 7. Tests

- [x] 7.1 Add integration test: ingest trade with full metadata → verify trade stored with all fields
- [x] 7.2 Add integration test: ingest trade without metadata → verify trade stored with NULL metadata
- [x] 7.3 Add integration test: open position with SL/TP/confidence → verify position has metadata
- [x] 7.4 Add integration test: close position → verify exit_price and exit_reason set on position
- [x] 7.5 Add integration test: increase position with new SL → verify SL updated, TP and confidence unchanged
- [x] 7.6 Add integration test: rebuild positions → verify metadata preserved on rebuilt positions
- [x] 7.7 Add API test: GET trades returns metadata fields when present, omits when NULL
- [x] 7.8 Add API test: GET positions returns metadata fields when present, omits when NULL
