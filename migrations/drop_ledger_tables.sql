-- Run manually against the platform DB after deploying this version.
-- These tables were used by the traderd ledger layer which has been removed.
-- All trade/position data is now served exclusively by the platform API.

DROP TABLE IF EXISTS ledger_trades;
DROP TABLE IF EXISTS ledger_positions;
DROP TABLE IF EXISTS ledger_account_balances;
DROP TABLE IF EXISTS ledger_accounts;
DROP TABLE IF EXISTS ledger_orders;
DROP TABLE IF EXISTS ledger_schema_migrations;
DROP TABLE IF EXISTS engine_position_state;
