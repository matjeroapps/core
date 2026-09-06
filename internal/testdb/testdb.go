package testdb

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matjeroapps/core/packages/database"
)

var nonIdentifier = regexp.MustCompile(`[^a-z0-9_]+`)

func Open(t testing.TB, dsn string) *database.Pool {
	t.Helper()

	ctx := context.Background()

	adminCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	schema := schemaName(t.Name())
	quotedSchema := pgx.Identifier{schema}.Sanitize()

	// Acquire a session-level advisory lock during DDL execution (CREATE SCHEMA,
	// CREATE EXTENSION, DROP SCHEMA) to serialize system catalog modifications
	// across parallel test packages sharing a single PostgreSQL server.
	const testDBAdvisoryLockID int64 = 746328746
	if _, err := adminPool.Exec(ctx, `SELECT pg_advisory_lock($1)`, testDBAdvisoryLockID); err != nil {
		adminPool.Close()
		t.Fatalf("acquire DDL advisory lock: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+quotedSchema); err != nil {
		_, _ = adminPool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, testDBAdvisoryLockID)
		adminPool.Close()
		t.Fatalf("create isolated schema %s: %v", schema, err)
	}

	// CREATE EXTENSION IF NOT EXISTS is not safe under concurrent execution
	// across parallel test packages that share one database: two callers can
	// both observe the extension as absent and race to insert it, and one fails
	// with a duplicate-key violation on pg_extension_name_index. Tolerate that
	// race by proceeding when the extension is present after the attempt.
	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		var exists bool
		if qErr := adminPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')`).Scan(&exists); qErr != nil || !exists {
			_, _ = adminPool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, testDBAdvisoryLockID)
			adminPool.Close()
			t.Fatalf("ensure pgcrypto extension: %v", err)
		}
	}
	_, _ = adminPool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, testDBAdvisoryLockID)

	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if _, err := adminPool.Exec(ctx, `SELECT pg_advisory_lock($1)`, testDBAdvisoryLockID); err == nil {
			if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+quotedSchema+` CASCADE`); err != nil {
				t.Logf("drop isolated schema %s: %v", schema, err)
			}
			_, _ = adminPool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, testDBAdvisoryLockID)
		}
		adminPool.Close()
	})

	isolatedCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse database url for isolated pool: %v", err)
	}
	ensureRuntimeParams(isolatedCfg)
	isolatedCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err = pgxpool.NewWithConfig(ctx, isolatedCfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	return &database.Pool{Pool: pool}
}

func schemaName(name string) string {
	base := strings.ToLower(name)
	base = nonIdentifier.ReplaceAllString(base, "_")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "testdb"
	}
	return fmt.Sprintf("%s_%d", base, time.Now().UnixNano())
}

func ensureRuntimeParams(cfg *pgxpool.Config) {
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
}
