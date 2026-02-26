-- Migration 004: Rebuild all positions with margin-adjusted P&L.
--
-- Previously, futures realized_pnl was stored as full notional P&L:
--   pnl = (entry - exit) * qty
-- This inflated P&L by the leverage factor (e.g. 2x leverage → 2x P&L).
--
-- The fix: realized_pnl = notional_pnl / leverage (account-impact P&L).
--
-- Since we only have paper-trading data this migration wipes all positions
-- and rebuilds them from ledger_trades via the application RebuildPositions
-- logic. The SQL here only clears the positions; the application's startup
-- or the `ledger positions rebuild` CLI command must be run afterward.
--
-- Why clear here and not rebuild in SQL?
-- The rebuild requires the same weighted-average-entry logic that lives in
-- Go (upsertFuturesPosition / upsertSpotPosition), so it must be driven by
-- the application layer. The migration just ensures the stale data is gone
-- and cannot be served until the rebuild completes.

TRUNCATE TABLE ledger_positions;
