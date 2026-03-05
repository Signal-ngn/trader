-- Migration 007: Add hard_stop and granularity to engine_position_state.
--
-- hard_stop  — leverage-scaled circuit-breaker price computed at entry.
-- granularity — candle granularity from the trading config at entry time.
--
-- Both columns are nullable so existing rows are unaffected. The engine falls
-- back to safe defaults (hard_stop formula / 48h hold limit) when NULL.

ALTER TABLE engine_position_state
    ADD COLUMN IF NOT EXISTS hard_stop   NUMERIC,
    ADD COLUMN IF NOT EXISTS granularity TEXT;
