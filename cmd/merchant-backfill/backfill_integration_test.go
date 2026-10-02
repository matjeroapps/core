package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/testdb"
	"core/packages/database"
)

func TestBackfillDryRunApplyAndIdempotency(t *testing.T) {
	db := openBackfillDB(t)
	ctx := context.Background()

	affSeller := uuid.New()
	affSupplier := uuid.New()
	standaloneSeller := uuid.New()
	standaloneSupplier := uuid.New()
	seedProfile(t, db, "sellers", affSeller, "seller-aff", "Affiliated Seller", "active")
	seedProfile(t, db, "suppliers", affSupplier, "supplier-aff", "Affiliated Supplier", "active")
	seedProfile(t, db, "sellers", standaloneSeller, "seller-alone", "Standalone Seller", "active")
	seedProfile(t, db, "suppliers", standaloneSupplier, "supplier-alone", "Standalone Supplier", "active")
	seedOwner(t, db, "seller_members", "seller_id", affSeller, "acct:affiliated-owner")
	seedOwner(t, db, "supplier_members", "supplier_id", affSupplier, "acct:affiliated-owner")
	seedOwner(t, db, "seller_members", "seller_id", standaloneSeller, "acct:seller-owner")
	seedOwner(t, db, "supplier_members", "supplier_id", standaloneSupplier, "acct:supplier-owner")
	if _, err := db.Exec(ctx, `INSERT INTO supplier_seller_affiliations (supplier_id, seller_id) VALUES ($1, $2)`, affSupplier, affSeller); err != nil {
		t.Fatalf("insert affiliation: %v", err)
	}

	dryRun, err := executeBackfill(ctx, db.Pool, BackfillOptions{DryRun: true, BatchSize: 2})
	if err != nil {
		t.Fatalf("dry-run backfill: %v", err)
	}
	if dryRun.Metrics.TotalScanned != 4 || dryRun.Metrics.MappedProfiles != 4 {
		t.Fatalf("dry-run metrics = %+v, want scanned=4 mapped=4", dryRun.Metrics)
	}
	assertTableCount(t, db, "merchants", 0)
	assertTableCount(t, db, "merchant_migration_crosswalk", 0)

	applied, err := executeBackfill(ctx, db.Pool, BackfillOptions{DryRun: false, BatchSize: 2})
	if err != nil {
		t.Fatalf("apply backfill: %v", err)
	}
	if applied.Metrics.TotalScanned != 4 || applied.Metrics.MappedProfiles != 4 {
		t.Fatalf("apply metrics = %+v, want scanned=4 mapped=4", applied.Metrics)
	}
	assertTableCount(t, db, "merchants", 3)
	assertTableCount(t, db, "merchant_memberships", 3)
	assertTableCount(t, db, "merchant_membership_permissions", 22)
	assertTableCount(t, db, "merchant_migration_crosswalk", 4)
	assertTableCount(t, db, "outbox_events", 14)

	retried, err := executeBackfill(ctx, db.Pool, BackfillOptions{DryRun: false, BatchSize: 2})
	if err != nil {
		t.Fatalf("idempotent backfill: %v", err)
	}
	if retried.Metrics.RetriedProfiles != 4 {
		t.Fatalf("retried profiles = %d, want 4", retried.Metrics.RetriedProfiles)
	}
	assertTableCount(t, db, "merchants", 3)
	assertTableCount(t, db, "merchant_migration_crosswalk", 4)
	assertTableCount(t, db, "merchant_memberships", 3)
}

func TestBackfillQuarantinePersistsInvalidStatus(t *testing.T) {
	db := openBackfillDB(t)
	ctx := context.Background()
	sellerID := uuid.New()
	seedProfile(t, db, "sellers", sellerID, "seller-bad", "Bad Seller", "deleted")

	report, err := executeBackfill(ctx, db.Pool, BackfillOptions{DryRun: false, BatchSize: 10})
	if err != nil {
		t.Fatalf("apply backfill with invalid status: %v", err)
	}
	if report.Metrics.QuarantinedProfiles != 1 {
		t.Fatalf("quarantined profiles = %d, want 1", report.Metrics.QuarantinedProfiles)
	}
	assertTableCount(t, db, "merchant_migration_quarantine", 1)
}

func TestBackfillQuarantinesOwnerMismatch(t *testing.T) {
	db := openBackfillDB(t)
	ctx := context.Background()
	sellerID := uuid.New()
	supplierID := uuid.New()
	seedProfile(t, db, "sellers", sellerID, "seller-owner-mismatch", "Seller Owner Mismatch", "active")
	seedProfile(t, db, "suppliers", supplierID, "supplier-owner-mismatch", "Supplier Owner Mismatch", "active")
	seedOwner(t, db, "seller_members", "seller_id", sellerID, "acct:seller-owner")
	seedOwner(t, db, "supplier_members", "supplier_id", supplierID, "acct:supplier-owner")
	if _, err := db.Exec(ctx, `INSERT INTO supplier_seller_affiliations (supplier_id, seller_id) VALUES ($1, $2)`, supplierID, sellerID); err != nil {
		t.Fatalf("insert affiliation: %v", err)
	}

	report, err := executeBackfill(ctx, db.Pool, BackfillOptions{DryRun: false, BatchSize: 10})
	if err != nil {
		t.Fatalf("apply backfill with owner mismatch: %v", err)
	}
	if report.Metrics.QuarantinedProfiles != 2 {
		t.Fatalf("quarantined profiles = %d, want 2", report.Metrics.QuarantinedProfiles)
	}
	assertTableCount(t, db, "merchant_migration_quarantine", 1)
	var reason string
	if err := db.QueryRow(ctx, `SELECT reason_code FROM merchant_migration_quarantine`).Scan(&reason); err != nil {
		t.Fatalf("query quarantine reason: %v", err)
	}
	if reason != "OWNER_SUBJECT_MISMATCH" {
		t.Fatalf("reason = %q, want OWNER_SUBJECT_MISMATCH", reason)
	}
	assertTableCount(t, db, "merchants", 0)
}

func TestMigration000036CardinalityConstraint(t *testing.T) {
	db := openBackfillDB(t)
	ctx := context.Background()
	merchantID := uuid.New()
	sellerA := uuid.New()
	sellerB := uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO merchants (id, code, legal_name, status) VALUES ($1, 'mer_constraint', 'Constraint Merchant', 'active')`, merchantID); err != nil {
		t.Fatalf("insert merchant: %v", err)
	}
	seedProfile(t, db, "sellers", sellerA, "seller-a", "Seller A", "active")
	if _, err := db.Exec(ctx, `UPDATE sellers SET merchant_id = $1 WHERE id = $2`, merchantID, sellerA); err != nil {
		t.Fatalf("link first seller: %v", err)
	}
	seedProfile(t, db, "sellers", sellerB, "seller-b", "Seller B", "active")
	if _, err := db.Exec(ctx, `UPDATE sellers SET merchant_id = $1 WHERE id = $2`, merchantID, sellerB); err == nil {
		t.Fatal("expected cardinality constraint to reject second active seller")
	}

	down := filepath.Join("..", "..", "migrations", "000036_merchant_profile_cardinality.down.sql")
	up := filepath.Join("..", "..", "migrations", "000036_merchant_profile_cardinality.up.sql")
	testdb.ApplyMigrations(t, db, down, up)
}

func openBackfillDB(t *testing.T) *database.Pool {
	t.Helper()
	dsn := getenvRequired(t, "TEST_DATABASE_URL")
	db := testdb.Open(t, dsn)
	paths := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000009_supplier_retail_capability",
		"000018_supplier_retail_affiliation",
		"000034_merchant_identity_foundation",
		"000035_seller_supplier_merchant_linkage",
		"000036_merchant_profile_cardinality",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)
	return db
}

func getenvRequired(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s is required for database-backed Feature 022 validation", key)
	}
	return value
}

func seedProfile(t *testing.T, db *database.Pool, table string, id uuid.UUID, code, name, status string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), "INSERT INTO "+table+" (id, code, name, status) VALUES ($1, $2, $3, $4)", id, code, name, status); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func seedOwner(t *testing.T, db *database.Pool, table, profileColumn string, profileID uuid.UUID, subject string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), "INSERT INTO "+table+" (id, "+profileColumn+", principal_subject, role, status) VALUES ($1, $2, $3, 'owner', 'active')", uuid.New(), profileID, subject); err != nil {
		t.Fatalf("seed %s owner: %v", table, err)
	}
}

func assertTableCount(t *testing.T, db *database.Pool, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
