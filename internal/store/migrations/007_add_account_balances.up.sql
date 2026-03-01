-- Migration 007: add ledger_account_balances table for per-account cash balance tracking.
-- Balance rows are optional — accounts without a balance row are unaffected.
-- Primary key is (tenant_id, account_id, currency) to support multiple currencies per account.

CREATE TABLE IF NOT EXISTS ledger_account_balances (
    tenant_id   UUID             NOT NULL,
    account_id  TEXT             NOT NULL,
    currency    TEXT             NOT NULL DEFAULT 'USD',
    amount      DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, account_id, currency)
);
