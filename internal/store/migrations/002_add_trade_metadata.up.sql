-- Add trade metadata columns to ledger_trades
ALTER TABLE ledger_trades ADD COLUMN strategy TEXT;
ALTER TABLE ledger_trades ADD COLUMN entry_reason TEXT;
ALTER TABLE ledger_trades ADD COLUMN exit_reason TEXT;
ALTER TABLE ledger_trades ADD COLUMN confidence DOUBLE PRECISION;
ALTER TABLE ledger_trades ADD COLUMN stop_loss DOUBLE PRECISION;
ALTER TABLE ledger_trades ADD COLUMN take_profit DOUBLE PRECISION;

-- Add position metadata columns to ledger_positions
ALTER TABLE ledger_positions ADD COLUMN exit_price DOUBLE PRECISION;
ALTER TABLE ledger_positions ADD COLUMN exit_reason TEXT;
ALTER TABLE ledger_positions ADD COLUMN stop_loss DOUBLE PRECISION;
ALTER TABLE ledger_positions ADD COLUMN take_profit DOUBLE PRECISION;
ALTER TABLE ledger_positions ADD COLUMN confidence DOUBLE PRECISION;
