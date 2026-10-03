package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"core/internal/integration"
	"core/internal/merchants"
	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

func setupMerchantTestDB(t *testing.T) *database.Pool {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for database-backed integration tests")
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

func createTestMerchant(t *testing.T, db *database.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	repo := merchants.NewPostgresRepository(db.Pool)
	svc := merchants.NewService(repo)

	m, err := svc.CreateMerchant(ctx, "M-TEST-"+uuid.NewString()[:8], "Test Merchant Ltd", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant: %v", err)
	}
	return m.ID
}

// createTestStore inserts the seller and store rows required by RETAIL_CHANNEL
// connections (store_id is mandatory for that connection type) and returns the
// store ID.
func createTestStore(t *testing.T, db *database.Pool, merchantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	sellerID := uuid.New()
	storeID := uuid.New()

	if _, err := db.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status, merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', $4, NOW(), NOW())
	`, sellerID, "S-"+sellerID.String()[:8], "Test Seller", merchantID); err != nil {
		t.Fatalf("failed to insert seller: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status, created_at, updated_at)
		VALUES ($1, $2, 'EG', $3, 'Test Store', 'active', NOW(), NOW())
	`, storeID, sellerID, "ST-"+storeID.String()[:8]); err != nil {
		t.Fatalf("failed to insert store: %v", err)
	}
	return storeID
}

func TestMerchantConnectionPersistenceAndOutbox(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)
	storeID := createTestStore(t, db, merchantID)

	extAcc := "ext-shop-123"
	input := integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "My Shopify Store",
		StoreID:           &storeID,
		Settings:          json.RawMessage(`{"auto_sync":true}`),
	}

	conn, err := svc.CreateMerchantConnection(ctx, input, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("CreateMerchantConnection failed: %v", err)
	}

	if conn.ID == uuid.Nil {
		t.Errorf("expected non-nil UUID")
	}
	if conn.Status != integration.MerchantStatusDraft {
		t.Errorf("expected DRAFT status, got %s", conn.Status)
	}

	// Fetch connection
	fetched, err := svc.GetMerchantConnection(ctx, conn.ID)
	if err != nil {
		t.Fatalf("GetMerchantConnection failed: %v", err)
	}
	if fetched.Name != "My Shopify Store" {
		t.Errorf("expected name 'My Shopify Store', got %s", fetched.Name)
	}

	// Update Status
	updated, err := svc.UpdateMerchantConnectionStatus(ctx, conn.ID, integration.MerchantStatusActive, "corr-2", "caus-2")
	if err != nil {
		t.Fatalf("UpdateMerchantConnectionStatus failed: %v", err)
	}
	if updated.Status != integration.MerchantStatusActive {
		t.Errorf("expected ACTIVE status, got %s", updated.Status)
	}

	// Verify outbox events
	var outboxCount int
	err = db.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1", conn.ID.String()).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Errorf("expected 2 outbox events (created + status_changed), got %d", outboxCount)
	}
}

func TestMerchantConnectionUniqueness(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)
	storeID := createTestStore(t, db, merchantID)
	extAcc := "ext-shop-dup"

	input1 := integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Connection 1",
		StoreID:           &storeID,
	}

	_, err := svc.CreateMerchantConnection(ctx, input1, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Second connection with same merchant, provider, external_account_id should fail
	input2 := integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeSupplySource,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Connection 2",
	}

	_, err = svc.CreateMerchantConnection(ctx, input2, "corr-2", "caus-2")
	if err == nil {
		t.Fatalf("expected duplicate connection error, got nil")
	}
}

func TestMerchantJobIntent(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)

	conn, err := svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:     merchantID,
		ConnectionType: integration.ConnectionTypeSupplySource,
		Provider:       integration.ProviderCustomAPI,
		Name:           "Custom Supply Source",
	}, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("CreateJobIntent failed: %v", err)
	}

	jobInput := integration.CreateJobIntentInput{
		MerchantID:     merchantID,
		ConnectionID:   conn.ID,
		PipelineType:   "SUPPLY",
		JobType:        "CATALOG_IMPORT",
		IdempotencyKey: "job-idem-100",
	}

	job, err := svc.CreateJobIntent(ctx, jobInput)
	if err != nil {
		t.Fatalf("CreateJobIntent failed: %v", err)
	}

	if job.Status != integration.JobStatusRequested {
		t.Errorf("expected status REQUESTED, got %s", job.Status)
	}

	// List job intents
	jobs, err := svc.ListJobIntents(ctx, merchantID, &conn.ID, commerce.Page{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("ListJobIntents failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job intent, got %d", len(jobs))
	}
}

// TestMerchantConnectionPendingSetupUniqueness verifies the partial unique
// index: at most one in-flight setup row per (merchant, provider) while
// external_account_id is unknown (delta D2a).
func TestMerchantConnectionPendingSetupUniqueness(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)
	storeID := createTestStore(t, db, merchantID)

	first, err := svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:     merchantID,
		ConnectionType: integration.ConnectionTypeRetailChannel,
		Provider:       integration.ProviderShopify,
		Name:           "Pending Setup 1",
		StoreID:        &storeID,
	}, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("first pending-setup create failed: %v", err)
	}

	_, err = svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:     merchantID,
		ConnectionType: integration.ConnectionTypeRetailChannel,
		Provider:       integration.ProviderShopify,
		Name:           "Pending Setup 2",
		StoreID:        &storeID,
	}, "corr-2", "caus-2")
	if err == nil {
		t.Fatal("expected second pending-setup row for same (merchant, provider) to be rejected")
	}

	// The first row is untouched.
	if _, err := svc.GetMerchantConnection(ctx, first.ID); err != nil {
		t.Fatalf("first connection should remain readable: %v", err)
	}
}

// TestMerchantConnectionActiveRequiresExternalAccount verifies the database
// CHECK guard: ACTIVE without external_account_id is rejected for every
// writer, not just the service layer (delta D2b).
func TestMerchantConnectionActiveRequiresExternalAccount(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)

	conn, err := svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:     merchantID,
		ConnectionType: integration.ConnectionTypeSupplySource,
		Provider:       integration.ProviderSalla,
		Name:           "No Account Yet",
	}, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Service-level guard first.
	if _, err := svc.UpdateMerchantConnectionStatus(ctx, conn.ID, integration.MerchantStatusActive, "corr-2", "caus-2"); err == nil {
		t.Fatal("expected service to refuse ACTIVE without external_account_id")
	}

	// Database-level guard for any writer bypassing the service.
	if _, err := db.Exec(ctx, "UPDATE merchant_integration_connections SET status = $1 WHERE id = $2", integration.MerchantStatusActive, conn.ID); err == nil {
		t.Fatal("expected database CHECK to reject ACTIVE without external_account_id")
	}
}

// TestMerchantConnectionDownMigrationPreservesLegacy verifies that the
// 000037 down/up cycle drops only the I1 tables and leaves legacy tables
// untouched (spec boundary: no legacy table deletion or modification).
func TestMerchantConnectionDownMigrationPreservesLegacy(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	svc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)

	merchantID := createTestMerchant(t, db)
	storeID := createTestStore(t, db, merchantID)
	extAcc := "ext-down-up"
	conn, err := svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Down-Up Proof",
		StoreID:           &storeID,
	}, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	legacyID := "leg-down-up-" + uuid.NewString()[:8]
	if _, err := db.Exec(ctx, `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, created_at, updated_at)
		VALUES ($1, 'seller', 'seller-down-up', 'shopify', 'Legacy Survivor', 'active', NOW(), NOW())
	`, legacyID); err != nil {
		t.Fatalf("failed to insert legacy row: %v", err)
	}

	downSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000037_merchant_owned_integration_connections.down.sql"))
	if err != nil {
		t.Fatalf("failed to read down migration: %v", err)
	}
	upSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000037_merchant_owned_integration_connections.up.sql"))
	if err != nil {
		t.Fatalf("failed to read up migration: %v", err)
	}

	if _, err := db.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("down migration failed: %v", err)
	}
	if _, err := db.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("reapply (up) migration failed: %v", err)
	}

	var legacyCount int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM integration_connections WHERE id = $1", legacyID).Scan(&legacyCount); err != nil {
		t.Fatalf("legacy table should survive down/up: %v", err)
	}
	if legacyCount != 1 {
		t.Fatalf("expected legacy row to survive down/up, got count %d", legacyCount)
	}

	var canonicalCount int
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM merchant_integration_connections WHERE id = $1", conn.ID).Scan(&canonicalCount); err != nil {
		t.Fatalf("failed to query canonical table after reapply: %v", err)
	}
	if canonicalCount != 0 {
		t.Fatalf("expected canonical row to be dropped by down migration, got count %d", canonicalCount)
	}
}

// TestMerchantConnectionOutboxAtomicityAndVersions verifies transactional
// outbox atomicity, monotonic aggregate versions, and privacy-clean payloads
// (deltas D7 and the outbox contract).
func TestMerchantConnectionOutboxAtomicityAndVersions(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	svc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)

	merchantID := createTestMerchant(t, db)
	storeID := createTestStore(t, db, merchantID)
	extAcc := "ext-outbox-versioning"
	conn, err := svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Outbox Proof",
		StoreID:           &storeID,
		Settings:          json.RawMessage(`{"auto_sync":true}`),
		GrantedScopes:     json.RawMessage(`["read_products"]`),
	}, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	for _, status := range []integration.MerchantConnectionStatus{integration.MerchantStatusActive, integration.MerchantStatusPaused} {
		if _, err := svc.UpdateMerchantConnectionStatus(ctx, conn.ID, status, "corr", "caus"); err != nil {
			t.Fatalf("status change to %s failed: %v", status, err)
		}
	}

	var versions []int64
	rows, err := db.Query(ctx, `
		SELECT aggregate_version FROM outbox_events
		WHERE aggregate_type = 'merchant_integration_connection' AND aggregate_id = $1
		ORDER BY aggregate_version ASC
	`, conn.ID.String())
	if err != nil {
		t.Fatalf("failed to query outbox versions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("failed to scan aggregate_version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("outbox version iteration failed: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 outbox events (created + 2 status changes), got %d", len(versions))
	}
	for i, v := range versions {
		if v != int64(i+1) {
			t.Fatalf("expected monotonic aggregate versions [1 2 3], got %v", versions)
		}
	}

	// Atomicity: a rolled-back mutation must leave no event. A duplicate
	// create for the same (merchant, provider, external account) fails inside
	// its transaction.
	dupExt := "ext-outbox-versioning"
	_, err = svc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &dupExt,
		Name:              "Duplicate Should Roll Back",
	}, "corr-dup", "caus-dup")
	if err == nil {
		t.Fatal("expected duplicate create to fail")
	}

	var eventCount int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE aggregate_type = 'merchant_integration_connection' AND aggregate_id = $1
	`, conn.ID.String()).Scan(&eventCount); err != nil {
		t.Fatalf("failed to count outbox events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("expected outbox count to remain 3 after rolled-back mutation, got %d", eventCount)
	}

	// Privacy: payloads carry identifiers and lifecycle metadata only.
	var payloads []string
	payloadRows, err := db.Query(ctx, `
		SELECT payload FROM outbox_events
		WHERE aggregate_type = 'merchant_integration_connection' AND aggregate_id = $1
	`, conn.ID.String())
	if err != nil {
		t.Fatalf("failed to query payloads: %v", err)
	}
	defer payloadRows.Close()
	for payloadRows.Next() {
		var p string
		if err := payloadRows.Scan(&p); err != nil {
			t.Fatalf("failed to scan payload: %v", err)
		}
		payloads = append(payloads, p)
	}
	for _, p := range payloads {
		for _, forbidden := range []string{"credentials", "vault_ref", "granted_scopes", "auto_sync"} {
			if strings.Contains(p, forbidden) {
				t.Errorf("outbox payload contains forbidden material %q: %s", forbidden, p)
			}
		}
	}
}
