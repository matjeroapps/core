package migrations

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func ensureMetadata(ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	_, err := exec.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations metadata: %w", err)
	}
	return nil
}

func loadApplied(ctx context.Context, queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]AppliedMigration, error) {
	rows, err := queryer.Query(ctx, `
		SELECT version, name, checksum, applied_at
		FROM schema_migrations
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]AppliedMigration{}
	for rows.Next() {
		var migration AppliedMigration
		if err := rows.Scan(&migration.ID, &migration.Name, &migration.Checksum, &migration.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[migration.Name] = migration
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}
