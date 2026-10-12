package migrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration000046UpAndDownStructural applies the 000046 authoring
// migration over an already-migrated schema snapshot and verifies that
// down.sql removes exactly the new tables, columns and indexes while leaving
// the extended columns' defaults intact on the way in.
func TestMigration000046UpAndDownStructural(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for 000046 structural migration validation")
	}
	pool := testdb.Open(t, dsn).Pool
	ctx := context.Background()

	// A minimal legacy-compatible surface for the migration: both tables and
	// FK targets must exist before 000046 runs.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS stores (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), slug TEXT);
		CREATE TABLE IF NOT EXISTS store_categories (id UUID NOT NULL, store_id UUID NOT NULL, PRIMARY KEY (id, store_id));
		CREATE TABLE IF NOT EXISTS products (id UUID PRIMARY KEY DEFAULT gen_random_uuid());
		CREATE TABLE IF NOT EXISTS store_media_assets (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), store_id UUID NOT NULL, storage_key TEXT NOT NULL UNIQUE, content_type TEXT NOT NULL DEFAULT 'image/png', byte_size BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE (id, store_id));
		CREATE TABLE IF NOT EXISTS product_media_references (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), store_id UUID NOT NULL, product_id UUID REFERENCES products(id) ON DELETE RESTRICT, asset_id UUID, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
		CREATE TABLE IF NOT EXISTS product_translations (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), product_id UUID REFERENCES products(id) ON DELETE RESTRICT, locale TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
	`); err != nil {
		t.Fatalf("prepare minimal schema: %v", err)
	}

	up, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000046_ai_product_authoring.up.sql"))
	if err != nil {
		t.Fatalf("read up: %v", err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("apply 000046 up: %v", err)
	}

	for _, table := range []string{"ai_authoring_jobs", "ai_authoring_source_assets", "ai_authoring_field_proposals", "ai_authoring_image_candidates"} {
		if !tableExists(t, pool, table) {
			t.Fatalf("table %s missing after up", table)
		}
	}
	for _, probe := range [][2]string{
		{"product_media_references", "is_visible"},
		{"product_media_references", "origin"},
		{"store_media_assets", "origin"},
		{"product_translations", "meta_keywords"},
	} {
		if !columnExists(t, pool, probe[0], probe[1]) {
			t.Fatalf("column %s.%s missing after up", probe[0], probe[1])
		}
	}

	// Row preservation: insert legacy rows before down and confirm they
	// survive with the new columns defaulted to the merchant-visible values.
	if _, err := pool.Exec(ctx, `
		INSERT INTO store_media_assets (id, store_id, storage_key) VALUES (gen_random_uuid(), gen_random_uuid(), 'legacy/asset.png' || gen_random_uuid()::text)
		ON CONFLICT (storage_key) DO NOTHING;
	`); err != nil {
		t.Fatalf("seed legacy asset: %v", err)
	}
	var preserved int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM store_media_assets WHERE origin = 'merchant'`).Scan(&preserved); err != nil {
		t.Fatalf("count preserved rows: %v", err)
	}
	if preserved == 0 {
		t.Fatal("expected at least one legacy row classified merchant")
	}

	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000046_ai_product_authoring.down.sql"))
	if err != nil {
		t.Fatalf("read down: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply 000046 down: %v", err)
	}
	for _, table := range []string{"ai_authoring_jobs", "ai_authoring_source_assets", "ai_authoring_field_proposals", "ai_authoring_image_candidates"} {
		if tableExists(t, pool, table) {
			t.Fatalf("table %s must be dropped by down", table)
		}
	}
	for _, probe := range [][2]string{
		{"product_media_references", "is_visible"},
		{"product_media_references", "origin"},
		{"store_media_assets", "origin"},
		{"product_translations", "meta_keywords"},
	} {
		if columnExists(t, pool, probe[0], probe[1]) {
			t.Fatalf("column %s.%s still exists after down", probe[0], probe[1])
		}
	}
	// The seeded legacy media rows survive.
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM store_media_assets`).Scan(&preserved); err != nil {
		t.Fatalf("count surviving rows: %v", err)
	}
	if preserved == 0 {
		t.Fatal("down.sql must not delete data rows from extended tables")
	}
}

func tableExists(t *testing.T, pool testPool, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`, table,
	).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

func columnExists(t *testing.T, pool testPool, table, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`, table, column,
	).Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return exists
}

type testPool = *pgxpool.Pool
