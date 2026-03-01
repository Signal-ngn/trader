## Context

The ledger tracks trades and positions but has no concept of account capital. Balance tracking is added as a `ledger_account_balances` table: set explicitly by the caller for initial funding and manual corrections, and adjusted automatically by trade ingestion when a position is opened or closed.

The existing multi-tenancy pattern uses a `(tenant_id UUID, account_id TEXT)` composite key throughout all tables. The balance table follows the same pattern. All existing write endpoints (`POST /import`, `DELETE /trades/{tradeId}`) already break the "read-only API" framing in the REST spec — `PUT /balance` is consistent with that precedent.

The ingestion path already wraps trade insert + position upsert in a single transaction (`InsertTradeAndUpdatePosition`). Balance adjustment slots into that same transaction via `AdjustBalance`, keeping all three writes atomic.

## Goals / Non-Goals

**Goals:**
- Store a single cash balance per (tenant, account, currency) — defaulting to `USD`.
- Auto-adjust balance during trade ingestion: deduct cost on open, credit realised P&L on close (spot and futures). Adjustment is a no-op when no balance row exists.
- Expose `PUT` (set/overwrite) and `GET` endpoints on `/api/v1/accounts/{accountId}/balance` for initial setup and manual corrections.
- Surface balance in portfolio summary and account stats responses.
- Add `ledger accounts balance set` and `ledger accounts balance get` CLI subcommands.

**Non-Goals:**
- No enforcement or validation that balance covers a position's cost — the ledger is not a risk engine.
- No transaction history or audit log of balance changes.
- No multi-currency arithmetic or conversion.
- Balance is not rebuilt when positions are rebuilt from trade history — rebuild only reconstructs positions.

## Decisions

### D1: Single-row upsert per (tenant, account, currency) — not an event log

**Decision:** `ledger_account_balances` holds one row per (tenant_id, account_id, currency). `PUT /balance` overwrites it unconditionally (upsert). `AdjustBalance` applies a signed delta with an atomic `UPDATE ... SET amount = amount + $delta`.

**Rationale:** The table is a running balance, not a ledger of movements. Ingestion adjusts it incrementally; the caller can reset it absolutely. An append-only adjustment table would add complexity without benefit — we have no requirement to replay or audit balance history.

**Alternative considered:** Append-only adjustment table with a materialised view. Rejected — overkill; would complicate the ingestion hot path.

---

### D2: `AdjustBalance` is a no-op when no balance row exists

**Decision:** If no balance row exists for a (tenant, account, currency) when ingestion tries to adjust it, the `UPDATE` matches zero rows and returns without error. No implicit row is created.

**Rationale:** Accounts that have not had a balance configured should not silently receive a balance starting at zero with a negative deduction. The caller must explicitly set an initial balance via `PUT /balance` before ingestion adjustments take effect. This avoids confusing states (e.g. a balance of −50000 on an account that was never funded).

**Alternative considered:** Auto-create a balance row at zero and adjust from there. Rejected — a balance of −50000 has no meaning if no initial balance was ever set.

---

### D3: Balance adjustment amounts for spot vs. futures

**Decision:**
- **Spot buy (open):** deduct `quantity × price + fee` (cost basis including fee).
- **Spot sell (partial close):** credit `realised_pnl` as already computed by `upsertSpotPosition` — i.e. `(sell_price − avg_entry) × qty − fee`.
- **Spot sell (full close):** same as partial close.
- **Futures open:** deduct `margin` when provided; fall back to `cost_basis / leverage` when margin is absent; skip adjustment if neither is available.
- **Futures close (partial or full):** credit `realised_pnl` as computed by `upsertFuturesPosition` (already leverage- and fee-adjusted).

**Rationale:** These mirror what the position upsert already computes. Re-using the computed values avoids duplicating arithmetic and keeps balance consistent with the position ledger.

---

### D4: Balance adjustment is inside the existing transaction

**Decision:** `AdjustBalance` takes a `pgx.Tx` and runs inside the same transaction as `InsertTrade` + `UpsertPosition`. If the transaction rolls back, the balance adjustment rolls back too.

**Rationale:** Atomicity is essential — a trade that fails to insert must not leave the balance permanently altered. The transaction boundary is already established in `InsertTradeAndUpdatePosition`; passing the `tx` down to `AdjustBalance` requires no structural change.

---

### D5: Currency stored as a column, defaulting to `USD`

**Decision:** `currency TEXT NOT NULL DEFAULT 'USD'` column on `ledger_account_balances`. API and CLI accept an optional `--currency` / `?currency=` parameter, defaulting to `USD`. Ingestion always adjusts the `USD` row.

**Rationale:** Most accounts trade a single quote currency. Keeping currency explicit in the schema allows future multi-currency support without a migration. Ingestion hard-codes `USD` because the trade event has no currency concept beyond `fee_currency`, which is the fee denomination rather than the account's base currency.

---

### D6: Balance is optional in portfolio and stats responses — omitted when not set

**Decision:** `handlePortfolioSummary` and `handleAccountStats` call `GetAccountBalance` and attach the result only when a balance row exists. The JSON field is omitted (`omitempty`) when nil.

**Rationale:** Many accounts will not have a balance set. Returning `null` or `0` would be misleading — a true balance of 0 is different from "not configured". Omitting the field keeps the contract clean.

---

### D7: `PUT /balance` uses full-overwrite semantics

**Decision:** `PUT /api/v1/accounts/{accountId}/balance` accepts `{"amount": 50000, "currency": "USD"}` and upserts that as the new balance, regardless of any automatic adjustments that have accumulated.

**Rationale:** This is the manual-correction escape hatch. The caller knows the real balance (e.g. from broker reconciliation) and sets it absolutely. A delta-based PATCH would be ambiguous alongside automatic ingestion adjustments.

---

### D8: `ledger accounts balance` as a sub-command group under `accounts`

**Decision:** New commands are `ledger accounts balance set <account-id> <amount>` and `ledger accounts balance get <account-id>`, grouped under the existing `accounts` cobra command.

**Rationale:** Balance is an account-level concern. Nesting under `accounts` is consistent with the existing `accounts list` / `accounts show` pattern and keeps the top-level command surface small.

## Risks / Trade-offs

- **Balance drift on rebuild**: `RebuildPositions` replays all trades and re-runs `UpsertPosition`. If `AdjustBalance` is called during rebuild it would double-adjust the balance. → `RebuildPositions` will skip balance adjustment (pass a flag or use a separate non-adjusting position path). Balance is not rebuilt — it retains whatever value it had before the rebuild.
- **Concurrent ingestion races**: Two simultaneous trades for the same account both doing `UPDATE amount = amount + delta` are safe (atomic delta update), but the order of adjustments is non-deterministic. → Acceptable; trades are expected to be processed sequentially per account via the durable NATS consumer.
- **Negative balance**: Deductions can push balance below zero if trades arrive before a balance is set, or if the initial balance is too low. → Accepted; the ledger does not enforce solvency.
- **Futures margin availability**: Some trade events omit the `margin` field. The fallback to `cost_basis / leverage` may be imprecise. → Accepted with the same pragmatism as the existing default-2x-leverage fallback in `upsertFuturesPosition`.

## Migration Plan

1. Add migration `007_add_account_balances.up.sql` — creates `ledger_account_balances` with `(tenant_id, account_id, currency)` primary key and `amount DOUBLE PRECISION NOT NULL DEFAULT 0`, `updated_at TIMESTAMPTZ` columns.
2. No data backfill needed — balance rows are optional and created on first `PUT`. Existing accounts with no balance row continue to work (ingestion adjustments skip silently).
3. Rollback: drop `ledger_account_balances` table (no other table references it).
4. Deploy service — new endpoints and CLI are additive; ingestion gains adjustment calls but they no-op for accounts without a balance row, so existing behaviour is unchanged until a balance is explicitly set.

## Open Questions

- None — scope is fully defined by the proposal and design decisions above.
