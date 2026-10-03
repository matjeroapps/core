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

	sellerID := uuid.New().String()
	_, err = db.Exec(ctx, "INSERT INTO sellers (id, code, name, status, merchant_id, created_at, updated_at) VALUES ($1, $2, $3, 'active', $4, NOW(), NOW())", sellerID, "S-"+sellerID[:8], "Seller One", merchant1.ID)
	if err != nil {
		t.Fatalf("failed to insert seller: %v", err)
	}

	// Insert legacy connection for Seller One, carrying the canonical
	// settings key that is the only accepted source of a verified external
	// account identity (delta D2c).
	legacyConnID := "leg-conn-1"
	_, err = db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, legacyConnID, "seller", sellerID, "shopify", "Legacy Store", "active", `{"external_account_id":"ext-shop-777"}`)
	if err != nil {
		t.Fatalf("failed to insert legacy connection: %v", err)
	}

	// Insert unlinked legacy connection (unlinked supplier: a valid but
	// nonexistent supplier UUID, so the merchant link resolves to nothing)
	unlinkedConnID := "leg-conn-unlinked"
	unlinkedSupplierID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, unlinkedConnID, "supplier", unlinkedSupplierID, "salla", "Legacy Unlinked", "active")
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
	if dryRunSummary.ReusedExistingCount != 0 {
		t.Errorf("expected dry-run reused_existing_count 0, got %d", dryRunSummary.ReusedExistingCount)
	}
	if dryRunSummary.QuarantinedCount != 1 {
		t.Errorf("expected dry-run quarantined 1, got %d", dryRunSummary.QuarantinedCount)
	}
	if dryRunSummary.ScannedCount != dryRunSummary.MappedCount+dryRunSummary.QuarantinedCount+dryRunSummary.DeferredCount+dryRunSummary.FailedCount {
		t.Errorf("accounting equation violated: scanned=%d mapped=%d quarantined=%d deferred=%d failed=%d",
			dryRunSummary.ScannedCount, dryRunSummary.MappedCount, dryRunSummary.QuarantinedCount, dryRunSummary.DeferredCount, dryRunSummary.FailedCount)
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
	if applySummary.ReusedExistingCount != 0 {
		t.Errorf("expected apply reused_existing_count 0, got %d", applySummary.ReusedExistingCount)
	}
	if applySummary.QuarantinedCount != 1 {
		t.Errorf("expected apply quarantined 1, got %d", applySummary.QuarantinedCount)
	}
	if applySummary.FailedCount != 0 {
		t.Errorf("expected apply failed 0, got %d", applySummary.FailedCount)
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

	// The migrated canonical row must carry the resolved external account and
	// the created (not reused) crosswalk flag.
	var migratedExtAcc string
	var migratedReused bool
	if err := db.QueryRow(ctx, `
		SELECT c.external_account_id, x.reused_existing_connection
		FROM merchant_integration_connections c
		JOIN merchant_integration_migration_crosswalk x ON x.target_connection_id = c.id
		WHERE x.legacy_connection_id = $1
	`, legacyConnID).Scan(&migratedExtAcc, &migratedReused); err != nil {
		t.Fatalf("failed to query migrated connection: %v", err)
	}
	if migratedExtAcc != "ext-shop-777" {
		t.Errorf("expected migrated external_account_id ext-shop-777, got %q", migratedExtAcc)
	}
	if migratedReused {
		t.Errorf("expected created crosswalk row to have reused_existing_connection=false")
	}

	// Verify quarantined record details
	var qReason string
	_ = db.QueryRow(ctx, "SELECT quarantine_reason_code FROM merchant_integration_migration_crosswalk WHERE legacy_connection_id = $1", unlinkedConnID).Scan(&qReason)
	if qReason != integration.QuarantineMissingMerchantLink {
		t.Errorf("expected reason %s, got %s", integration.QuarantineMissingMerchantLink, qReason)
	}

	// Exactly one migration.completed event for the apply run.
	var migrationEvents int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type = 'merchant.integration.migration.completed.v1' AND aggregate_id = $1
	`, applySummary.RunID.String()).Scan(&migrationEvents); err != nil {
		t.Fatalf("failed to count migration events: %v", err)
	}
	if migrationEvents != 1 {
		t.Errorf("expected exactly 1 migration.completed event for the apply run, got %d", migrationEvents)
	}

	// No credential material may leak into any event payload (delta D1).
	var vaultLeaks int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type LIKE 'merchant.integration.%' AND payload::text ILIKE '%vault%'
	`).Scan(&vaultLeaks); err != nil {
		t.Fatalf("failed to scan payloads: %v", err)
	}
	if vaultLeaks != 0 {
		t.Errorf("expected no vault references in integration event payloads, got %d", vaultLeaks)
	}
}

// TestReconcileQuarantinesMissingExternalAccountID verifies the clarified
// rule: linked legacy records without a resolvable verified external account
// identity quarantine with MISSING_EXTERNAL_ACCOUNT_ID (delta D2c).
func TestReconcileQuarantinesMissingExternalAccountID(t *testing.T) {
	db := setupReconcileDB(t)
	ctx := context.Background()

	mRepo := merchants.NewPostgresRepository(db.Pool)
	mSvc := merchants.NewService(mRepo)
	intRepo := integration.NewMerchantRepository(db.Pool)
	intSvc := integration.NewMerchantService(intRepo, db.Pool)

	merchant, err := mSvc.CreateMerchant(ctx, "M-REC-EXT", "Merchant Ext", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant: %v", err)
	}

	sellerID := uuid.New().String()
	if _, err := db.Exec(ctx, "INSERT INTO sellers (id, code, name, status, merchant_id, created_at, updated_at) VALUES ($1, $2, $3, 'active', $4, NOW(), NOW())", sellerID, "S-"+sellerID[:8], "Seller Ext", merchant.ID); err != nil {
		t.Fatalf("failed to insert seller: %v", err)
	}

	legacyConnID := "leg-conn-no-ext"
	if _, err := db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, settings, created_at, updated_at)
		VALUES ($1, 'seller', $2, 'shopify', 'No External Account', 'active', '{}', NOW(), NOW())
	`, legacyConnID, sellerID); err != nil {
		t.Fatalf("failed to insert legacy connection: %v", err)
	}

	summary, err := intSvc.ReconcileLegacyConnections(ctx, false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if summary.QuarantinedCount != 1 {
		t.Errorf("expected 1 quarantined record, got %d", summary.QuarantinedCount)
	}
	if summary.MappedCount != 0 {
		t.Errorf("expected 0 mapped records, got %d", summary.MappedCount)
	}

	var reason string
	if err := db.QueryRow(ctx, "SELECT quarantine_reason_code FROM merchant_integration_migration_crosswalk WHERE legacy_connection_id = $1", legacyConnID).Scan(&reason); err != nil {
		t.Fatalf("failed to query quarantine reason: %v", err)
	}
	if reason != integration.QuarantineMissingExternalAccount {
		t.Errorf("expected reason %s, got %s", integration.QuarantineMissingExternalAccount, reason)
	}

	var connCount int
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_connections").Scan(&connCount)
	if connCount != 0 {
		t.Errorf("expected no canonical connections for unverified legacy records, got %d", connCount)
	}
}

// TestReconcileQuarantinesUnsupportedActorType verifies the
// AMBIGUOUS_OWNER_MISMATCH classification for unsupported legacy actor types.
func TestReconcileQuarantinesUnsupportedActorType(t *testing.T) {
	db := setupReconcileDB(t)
	ctx := context.Background()

	intSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)

	legacyConnID := "leg-conn-admin"
	if _, err := db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, settings, created_at, updated_at)
		VALUES ($1, 'admin', 'admin-1', 'shopify', 'Admin Owned', 'active', '{"external_account_id":"ext-1"}', NOW(), NOW())
	`, legacyConnID); err != nil {
		t.Fatalf("failed to insert legacy connection: %v", err)
	}

	summary, err := intSvc.ReconcileLegacyConnections(ctx, false)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if summary.QuarantinedCount != 1 {
		t.Errorf("expected 1 quarantined record, got %d", summary.QuarantinedCount)
	}

	var reason string
	if err := db.QueryRow(ctx, "SELECT quarantine_reason_code FROM merchant_integration_migration_crosswalk WHERE legacy_connection_id = $1", legacyConnID).Scan(&reason); err != nil {
		t.Fatalf("failed to query quarantine reason: %v", err)
	}
	if reason != integration.QuarantineAmbiguousOwnerMismatch {
		t.Errorf("expected reason %s, got %s", integration.QuarantineAmbiguousOwnerMismatch, reason)
	}
}

// TestReconcileApplyIsIdempotent verifies the clarified reuse rule: re-running
// apply reuses existing canonical connections and records the crosswalk as
// migrated/idempotent instead of failing or quarantining (delta D3).
func TestReconcileApplyIsIdempotent(t *testing.T) {
	db := setupReconcileDB(t)
	ctx := context.Background()

	mRepo := merchants.NewPostgresRepository(db.Pool)
	mSvc := merchants.NewService(mRepo)
	intRepo := integration.NewMerchantRepository(db.Pool)
	intSvc := integration.NewMerchantService(intRepo, db.Pool)

	merchant, err := mSvc.CreateMerchant(ctx, "M-REC-IDEM", "Merchant Idem", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant: %v", err)
	}

	sellerID := uuid.New().String()
	if _, err := db.Exec(ctx, "INSERT INTO sellers (id, code, name, status, merchant_id, created_at, updated_at) VALUES ($1, $2, $3, 'active', $4, NOW(), NOW())", sellerID, "S-"+sellerID[:8], "Seller Idem", merchant.ID); err != nil {
		t.Fatalf("failed to insert seller: %v", err)
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, settings, created_at, updated_at)
		VALUES ('leg-conn-idem', 'seller', $1, 'shopify', 'Idempotent Store', 'active', '{"external_account_id":"ext-idem-1"}', NOW(), NOW())
	`, sellerID); err != nil {
		t.Fatalf("failed to insert legacy connection: %v", err)
	}

	first, err := intSvc.ReconcileLegacyConnections(ctx, false)
	if err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if first.MappedCount != 1 || first.ReusedExistingCount != 0 {
		t.Fatalf("expected first apply mapped=1 reused=0, got mapped=%d reused=%d", first.MappedCount, first.ReusedExistingCount)
	}

	second, err := intSvc.ReconcileLegacyConnections(ctx, false)
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if second.MappedCount != 1 {
		t.Errorf("expected second apply mapped 1 (created + reused), got %d", second.MappedCount)
	}
	if second.ReusedExistingCount != 1 {
		t.Errorf("expected second apply reused_existing_count 1, got %d", second.ReusedExistingCount)
	}
	if second.QuarantinedCount != 0 || second.FailedCount != 0 {
		t.Errorf("expected no quarantine or failure on re-run, got quarantined=%d failed=%d", second.QuarantinedCount, second.FailedCount)
	}

	var connCount int
	_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_connections").Scan(&connCount)
	if connCount != 1 {
		t.Errorf("expected still exactly 1 canonical connection after re-run, got %d", connCount)
	}

	var reusedCount int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM merchant_integration_migration_crosswalk
		WHERE run_id = $1 AND reused_existing_connection = TRUE
	`, second.RunID.String()).Scan(&reusedCount); err != nil {
		t.Fatalf("failed to count reused crosswalk rows: %v", err)
	}
	if reusedCount != 1 {
		t.Errorf("expected 1 crosswalk row with reused_existing_connection=true for the re-run, got %d", reusedCount)
	}

	var legacyEvents int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_type = 'merchant.integration.migration.completed.v1' AND aggregate_id = $1
	`, first.RunID.String()).Scan(&legacyEvents); err != nil {
		t.Fatalf("failed to count first-run events: %v", err)
	}
	if legacyEvents != 1 {
		t.Errorf("expected exactly 1 migration.completed event for the first run, got %d", legacyEvents)
	}
}
