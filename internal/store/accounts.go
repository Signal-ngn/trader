package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

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
