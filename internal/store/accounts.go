package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// AccountStats holds all-time aggregate statistics for an account.
type AccountStats struct {
	TotalTrades      int      `json:"total_trades"`
	ClosedTrades     int      `json:"closed_trades"`
	WinCount         int      `json:"win_count"`
	LossCount        int      `json:"loss_count"`
	WinRate          float64  `json:"win_rate"`
	TotalRealizedPnL float64  `json:"total_realized_pnl"`
	OpenPositions    int      `json:"open_positions"`
	Balance          *float64 `json:"balance,omitempty"`
}

// GetAccountStats returns aggregate statistics for the given (tenantID, accountID).
// Returns nil if the account does not exist.
func (r *Repository) GetAccountStats(ctx context.Context, tenantID uuid.UUID, accountID string) (*AccountStats, error) {
	// Check account exists first
	exists, err := r.AccountExists(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("check account: %w", err)
	}
	if !exists {
		return nil, nil
	}

	var stats AccountStats

	// Single conditional aggregation query over positions.
	// Each position is a round-trip (entry + exit combined), matching how
	// the dashboard counts trades. Closed positions have realized_pnl set.
	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'closed')                       AS closed_trades,
			COUNT(*) FILTER (WHERE status = 'closed' AND realized_pnl > 0)  AS win_count,
			COUNT(*) FILTER (WHERE status = 'closed' AND realized_pnl <= 0) AS loss_count,
			COALESCE(SUM(realized_pnl) FILTER (WHERE status = 'closed'), 0) AS total_realized_pnl,
			COUNT(*) FILTER (WHERE status = 'open')                         AS open_positions
		FROM ledger_positions
		WHERE tenant_id = $1 AND account_id = $2
	`, tenantID, accountID).Scan(
		&stats.ClosedTrades,
		&stats.WinCount,
		&stats.LossCount,
		&stats.TotalRealizedPnL,
		&stats.OpenPositions,
	)
	if err != nil {
		return nil, fmt.Errorf("get position stats: %w", err)
	}

	stats.TotalTrades = stats.ClosedTrades + stats.OpenPositions

	// Compute WinRate in Go
	if stats.ClosedTrades > 0 {
		stats.WinRate = float64(stats.WinCount) / float64(stats.ClosedTrades)
	}

	// Attach balance when set (omitted from response when nil)
	balance, err := r.GetAccountBalance(ctx, tenantID, accountID, "USD")
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	stats.Balance = balance

	return &stats, nil
}

// GetOrCreateAccount looks up an account by (tenantID, id). If it doesn't exist, creates it.
func (r *Repository) GetOrCreateAccount(ctx context.Context, tenantID uuid.UUID, id string, accountType domain.AccountType) (*domain.Account, error) {
	var acct domain.Account
	var acctType string
	err := r.pool.QueryRow(ctx,
		"SELECT id, name, type, created_at FROM ledger_accounts WHERE tenant_id = $1 AND id = $2",
		tenantID, id,
	).Scan(&acct.ID, &acct.Name, &acctType, &acct.CreatedAt)

	if err == pgx.ErrNoRows {
		// Auto-create account
		name := id
		_, err := r.pool.Exec(ctx,
			"INSERT INTO ledger_accounts (tenant_id, id, name, type) VALUES ($1, $2, $3, $4)",
			tenantID, id, name, string(accountType),
		)
		if err != nil {
			return nil, fmt.Errorf("create account: %w", err)
		}

		return r.GetOrCreateAccount(ctx, tenantID, id, accountType)
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	acct.Type = domain.AccountType(acctType)
	return &acct, nil
}

// AccountExists checks if an account with the given (tenantID, id) exists.
func (r *Repository) AccountExists(ctx context.Context, tenantID uuid.UUID, id string) (bool, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM ledger_accounts WHERE tenant_id = $1 AND id = $2",
		tenantID, id,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check account: %w", err)
	}
	return count > 0, nil
}

// ListAccounts returns all accounts for the given tenant.
func (r *Repository) ListAccounts(ctx context.Context, tenantID uuid.UUID) ([]domain.Account, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, name, type, created_at FROM ledger_accounts WHERE tenant_id = $1 ORDER BY created_at",
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var acct domain.Account
		var acctType string
		if err := rows.Scan(&acct.ID, &acct.Name, &acctType, &acct.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		acct.Type = domain.AccountType(acctType)
		accounts = append(accounts, acct)
	}

	if accounts == nil {
		accounts = []domain.Account{}
	}
	return accounts, nil
}

// SetAccountBalance upserts the cash balance for a (tenant, account, currency) key.
// Overwrites any existing value unconditionally.
func (r *Repository) SetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string, amount float64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ledger_account_balances (tenant_id, account_id, currency, amount, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, account_id, currency)
		DO UPDATE SET amount = EXCLUDED.amount, updated_at = EXCLUDED.updated_at
	`, tenantID, accountID, currency, amount, time.Now())
	if err != nil {
		return fmt.Errorf("set account balance: %w", err)
	}
	return nil
}

// GetAccountBalance returns the current balance for a (tenant, account, currency) key,
// or nil when no balance row exists.
func (r *Repository) GetAccountBalance(ctx context.Context, tenantID uuid.UUID, accountID, currency string) (*float64, error) {
	var amount float64
	err := r.pool.QueryRow(ctx,
		"SELECT amount FROM ledger_account_balances WHERE tenant_id = $1 AND account_id = $2 AND currency = $3",
		tenantID, accountID, currency,
	).Scan(&amount)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account balance: %w", err)
	}
	return &amount, nil
}

// AdjustBalance atomically applies a signed delta to the balance for a (tenant, account, currency) key.
// Must be called within an existing transaction. Is a no-op when no balance row exists.
func (r *Repository) AdjustBalance(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, accountID, currency string, delta float64) error {
	_, err := tx.Exec(ctx, `
		UPDATE ledger_account_balances
		SET amount = amount + $1, updated_at = NOW()
		WHERE tenant_id = $2 AND account_id = $3 AND currency = $4
	`, delta, tenantID, accountID, currency)
	if err != nil {
		return fmt.Errorf("adjust balance: %w", err)
	}
	return nil
}
