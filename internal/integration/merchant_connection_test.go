package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestMerchantConnectionPersistenceAndOutbox(t *testing.T) {
	db := setupMerchantTestDB(t)
	ctx := context.Background()
	repo := integration.NewMerchantRepository(db.Pool)
	svc := integration.NewMerchantService(repo, db.Pool)

	merchantID := createTestMerchant(t, db)

	extAcc := "ext-shop-123"
	input := integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "My Shopify Store",
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
	extAcc := "ext-shop-dup"

	input1 := integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Connection 1",
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
		t.Fatalf("failed to create connection: %v", err)
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
