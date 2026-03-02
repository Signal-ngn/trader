-- Engine position state: per-position risk metadata for the trading engine.
-- Separate from ledger_positions (which is owned by the ingest path).
-- The engine reads open positions from ledger_positions and overlays risk state
-- from this table.

CREATE TABLE IF NOT EXISTS engine_position_state (
    id            BIGSERIAL PRIMARY KEY,
    account_id    TEXT        NOT NULL,
    symbol        TEXT        NOT NULL,
    market_type   TEXT        NOT NULL,
    side          TEXT        NOT NULL,   -- 'long' or 'short'
    entry_price   DOUBLE PRECISION NOT NULL,
    stop_loss     DOUBLE PRECISION,
    take_profit   DOUBLE PRECISION,
    leverage      INTEGER,
    strategy      TEXT,
    opened_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    peak_price    DOUBLE PRECISION,
    trailing_stop DOUBLE PRECISION,
    tenant_id     UUID        NOT NULL,

    UNIQUE (account_id, symbol, market_type, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_engine_position_state_account
    ON engine_position_state (tenant_id, account_id);
