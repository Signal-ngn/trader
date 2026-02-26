package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies all pending up migrations in order.
// It uses a ledger_schema_migrations table to track which migrations have been applied.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := RunMigrationsWithReport(ctx, pool)
	return err
}

// RunMigrationsWithReport applies all pending up migrations in order and returns
// the list of version strings that were newly applied in this call.
func RunMigrationsWithReport(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	// Create migrations tracking table
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ledger_schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}

	// Read all migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	// Collect up migrations
	var upFiles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	var applied []string
	for _, filename := range upFiles {
		version := strings.TrimSuffix(filename, ".up.sql")

		// Check if already applied
		var count int
		err := pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM ledger_schema_migrations WHERE version = $1",
			version,
		).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("check migration %s: %w", version, err)
		}
		if count > 0 {
			log.Debug().Str("version", version).Msg("migration already applied, skipping")
			continue
		}

		// Read and apply migration
		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", filename, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin transaction for migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			tx.Rollback(ctx)
			return nil, fmt.Errorf("apply migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO ledger_schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			tx.Rollback(ctx)
			return nil, fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit migration %s: %w", version, err)
		}

		log.Info().Str("version", version).Msg("applied migration")
		applied = append(applied, version)
	}

	return applied, nil
}
