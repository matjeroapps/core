package main_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/integration"
	"core/internal/merchants"
	"core/internal/testdb"
	"core/packages/database"
)

func setupReconcileDB(t *testing.T) *database.Pool {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for reconcile integration tests")
	}
	db := testdb.Open(t, dbURL)
	paths := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000009_supplier_retail_capability",
		"000018_supplier_retail_affiliation",
		"000027_integration_foundation",
		"000028_supplier_integrations",
		"000029_seller_integrations",
		"000034_merchant_identity_foundation",
		"000035_seller_supplier_merchant_linkage",
		"000036_merchant_profile_cardinality",
		"000037_merchant_owned_integration_connections",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)
	return db
}

func TestReconcileDryRunAndApply(t *testing.T) {
	db := setupReconcileDB(t)
	ctx := context.Background()

	mRepo := merchants.NewPostgresRepository(db.Pool)
	mSvc := merchants.NewService(mRepo)
	intRepo := integration.NewMerchantRepository(db.Pool)
	intSvc := integration.NewMerchantService(intRepo, db.Pool)

	// Create Merchant 1 and Seller linked to it
	merchant1, err := mSvc.CreateMerchant(ctx, "M-REC-1", "Merchant One", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant1: %v", err)
	}

	sellerID := "seller-" + uuid.NewString()[:8]
	_, err = db.Exec(ctx, "INSERT INTO sellers (id, merchant_id, name, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())", sellerID, merchant1.ID, "Seller One")
	if err != nil {
		t.Fatalf("failed to insert seller: %v", err)
	}

	// Insert legacy connection for Seller One
	legacyConnID := "leg-conn-1"
	_, err = db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, legacyConnID, "seller", sellerID, "shopify", "Legacy Store")
	if err != nil {
		t.Fatalf("failed to insert legacy connection: %v", err)
	}

	// Insert unlinked legacy connection (unlinked supplier)
	unlinkedConnID := "leg-conn-unlinked"
	_, err = db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, unlinkedConnID, "supplier", "non-existent-supplier", "salla", "Legacy Unlinked")
	if err != nil {
		t.Fatalf("failed to insert unlinked legacy connection: %v", err)
	}

	// --- STEP 1: Dry-Run ---
	dryRunSummary, err := intSvc.ReconcileLegacyConnections(ctx, true)
	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	if dryRunSummary.ScannedCount != 2 {
		t.Errorf("expected scanned 2, got %d", dryRunSummary.ScannedCount)
	}
	if dryRunSummary.MappedCount != 1 {
		t.Errorf("expected dry-run mapped 1, got %d", dryRunSummary.MappedCount)
	}
	if dryRunSummary.QuarantinedCount != 1 {
		t.Errorf("expected dry-run quarantined 1, got %d", dryRunSummary.QuarantinedCount)
	}

	// Verify ZERO database writes during dry-run
	var count int
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_connections").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 merchant connections after dry-run, got %d", count)
	}
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_migration_crosswalk").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 crosswalk records after dry-run, got %d", count)
	}

	// --- STEP 2: Apply Mode ---
	applySummary, err := intSvc.ReconcileLegacyConnections(ctx, false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if applySummary.MappedCount != 1 {
		t.Errorf("expected apply mapped 1, got %d", applySummary.MappedCount)
	}
	if applySummary.QuarantinedCount != 1 {
		t.Errorf("expected apply quarantined 1, got %d", applySummary.QuarantinedCount)
	}

	// Verify database writes
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_connections").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 merchant connection after apply, got %d", count)
	}
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_migration_crosswalk").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 crosswalk records after apply (1 mapped + 1 quarantined), got %d", count)
	}

	// Verify quarantined record details
	var qReason string
	_ = db.QueryRow(ctx, "SELECT quarantine_reason_code FROM merchant_integration_migration_crosswalk WHERE legacy_connection_id = $1", unlinkedConnID).Scan(&qReason)
	if qReason != integration.QuarantineMissingMerchantLink {
		t.Errorf("expected reason %s, got %s", integration.QuarantineMissingMerchantLink, qReason)
	}
}
