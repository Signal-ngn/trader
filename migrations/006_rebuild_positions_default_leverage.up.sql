-- Migration 006: Rebuild positions with corrected default leverage.
--
-- Migration 005 fixed margin-scale logic but used unreliable margin/cost_basis
-- ratio. This migration defaults to 2x leverage (matching all current trading
-- configs) when neither the position nor the closing trade carries an explicit
-- leverage value. This correctly scales AVAX-USD and other symbols where the
-- bot omits the leverage field.

TRUNCATE TABLE ledger_positions;
