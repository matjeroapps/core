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
	"core/internal/testdb"
	"core/packages/database"
)

func setupSupplyTestDB(t *testing.T) *database.Pool {
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
		"000038_merchant_integration_external_account_lifecycle",
		"000039_integration_supply_staging",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)
	return db
}

func newSupplyService(t *testing.T, db *database.Pool) integration.SupplyService {
	t.Helper()
	return integration.NewSupplyService(db.Pool)
}

func createSupplyConnection(t *testing.T, svc integration.MerchantService, merchantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	extID := "ext-" + uuid.NewString()[:12]
	conn, err := svc.CreateMerchantConnection(context.Background(), integration.CreateMerchantConnectionInput{
		MerchantID:        merchantID,
		ConnectionType:    integration.ConnectionTypeSupplySource,
		Provider:          integration.ProviderSalla,
		ExternalAccountID: &extID,
		Name:              name,
	}, "", "")
	if err != nil {
		t.Fatalf("create supply connection: %v", err)
	}
	updated, err := svc.UpdateMerchantConnectionStatus(context.Background(), conn.ID, integration.MerchantStatusActive, "", "")
	if err != nil {
		t.Fatalf("activate supply connection: %v", err)
	}
	return updated.ID
}

func supplyRecord(entityType, externalProductID, sku string) integration.StagedRecordInput {
	s := sku
	return integration.StagedRecordInput{
		EntityType:        entityType,
		ExternalProductID: externalProductID,
		SKU:               &s,
		ContentDigest:     "digest-" + externalProductID,
		Payload:           json.RawMessage(`{"title":"staged product"}`),
	}
}

func countOutboxEvents(t *testing.T, db *database.Pool, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM outbox_events WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	return n
}

func TestSupplyFirstImportStagesAndNeverPublishes(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "first import connection")

	svc := newSupplyService(t, db)
	batch, records, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID:     merchantID,
		ConnectionID:   connID,
		BatchType:      integration.SupplyBatchTypeFirstImport,
		IdempotencyKey: "import-1",
		Records: []integration.StagedRecordInput{
			supplyRecord(integration.SupplyEntityTypeProduct, "prod-1", "SKU-1"),
			supplyRecord(integration.SupplyEntityTypeVariant, "prod-1", "SKU-1V"),
		},
	}, "corr-1", "cause-1")
	if err != nil {
		t.Fatalf("CreateImportBatch: %v", err)
	}
	if !batch.FirstImport {
		t.Errorf("expected first_import=true")
	}
	if batch.Status != integration.SupplyBatchStatusStaged {
		t.Errorf("batch status = %q, want STAGED (first imports are never published automatically)", batch.Status)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	for _, rec := range records {
		if rec.Status != integration.SupplyRecordStatusStaged {
			t.Errorf("record %s status = %q, want STAGED", rec.ExternalProductID, rec.Status)
		}
	}

	if got := countOutboxEvents(t, db, "event_type = $1 AND aggregate_id = $2",
		integration.EventTypeSupplyBatchStaged, batch.ID.String()); got != 1 {
		t.Errorf("batch_staged events = %d, want 1", got)
	}

	// Staging emits only the integration-family event for this batch: no
	// canonical catalog publication event may exist from staging alone.
	if got := countOutboxEvents(t, db, "aggregate_type = 'merchant_integration_supply_import_batch' AND aggregate_id = $1", batch.ID.String()); got != 1 {
		t.Errorf("batch aggregate events = %d, want exactly 1 (batch_staged)", got)
	}
}

func TestSupplyImportBatchIdempotency(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "idempotent import")

	svc := newSupplyService(t, db)
	input := integration.CreateSupplyImportBatchInput{
		MerchantID:     merchantID,
		ConnectionID:   connID,
		IdempotencyKey: "import-retry",
		Records:        []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "prod-1", "SKU-R")},
	}

	batch1, _, err := svc.CreateImportBatch(ctx, input, "c1", "u1")
	if err != nil {
		t.Fatalf("first CreateImportBatch: %v", err)
	}
	batch2, recs2, err := svc.CreateImportBatch(ctx, input, "c2", "u2")
	if err != nil {
		t.Fatalf("retry CreateImportBatch: %v", err)
	}
	if batch1.ID != batch2.ID {
		t.Errorf("retry created a new batch %s, want %s", batch2.ID, batch1.ID)
	}
	if len(recs2) != 1 || recs2[0].ExternalProductID != "prod-1" {
		t.Errorf("retry records mismatch: %+v", recs2)
	}
	if got := countOutboxEvents(t, db, "event_type = $1 AND payload->>'batch_id' = $2",
		integration.EventTypeSupplyBatchStaged, batch1.ID.String()); got != 1 {
		t.Errorf("batch_staged events after retry = %d, want 1", got)
	}
}

func TestSupplyDuplicateSKUCreatesReviewCaseAndBlocksApproval(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connA := createSupplyConnection(t, merchantSvc, merchantID, "connection A")
	connB := createSupplyConnection(t, merchantSvc, merchantID, "connection B")

	svc := newSupplyService(t, db)
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connA, IdempotencyKey: "a-1",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "prod-A", "DUP-SKU")},
	}, "c", "u"); err != nil {
		t.Fatalf("import A: %v", err)
	}
	batchB, recsB, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connB, IdempotencyKey: "b-1",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "prod-B", "DUP-SKU")},
	}, "c", "u")
	if err != nil {
		t.Fatalf("import B: %v", err)
	}
	if len(recsB) != 1 || recsB[0].Status != integration.SupplyRecordStatusDuplicateReview {
		t.Fatalf("record B status = %+v, want DUPLICATE_REVIEW", recsB)
	}

	// Duplicate products are never merged and inventory is never summed: the
	// merchant must resolve a review case first.
	cases, err := svc.ListReviewCases(ctx, merchantID, &connB, integration.ReviewCaseStatusOpen)
	if err != nil {
		t.Fatalf("ListReviewCases: %v", err)
	}
	if len(cases) != 1 || cases[0].ReasonCode != integration.ReviewReasonDuplicateSKU {
		t.Fatalf("review cases = %+v, want one DUPLICATE_SKU case", cases)
	}

	_, err = svc.ApproveImportBatch(ctx, integration.ApproveSupplyImportBatchInput{
		BatchID: batchB.ID, MerchantID: merchantID,
		Decisions: []integration.RecordDecision{{
			RecordID: recsB[0].ID, Decision: "APPROVE", InternalID: "prod-canonical-1",
		}},
	}, "c", "u")
	if err == nil || err != integration.ErrRecordInDuplicateReview {
		t.Fatalf("approval of duplicate-review record error = %v, want ErrRecordInDuplicateReview", err)
	}

	resolved, err := svc.ResolveReviewCase(ctx, cases[0].ID, merchantID, "KEEP_SEPARATE", "merchant-owner")
	if err != nil {
		t.Fatalf("ResolveReviewCase: %v", err)
	}
	if resolved.Status != integration.ReviewCaseStatusResolved {
		t.Errorf("case status = %q, want RESOLVED", resolved.Status)
	}

	gotBatch, err := svc.ApproveImportBatch(ctx, integration.ApproveSupplyImportBatchInput{
		BatchID: batchB.ID, MerchantID: merchantID,
		Decisions: []integration.RecordDecision{{
			RecordID: recsB[0].ID, Decision: "APPROVE", InternalID: "prod-canonical-B",
			AuthoritySource: integration.MappingAuthorityManual,
		}},
	}, "c", "u")
	if err != nil {
		t.Fatalf("approval after resolution: %v", err)
	}
	if gotBatch.Status != integration.SupplyBatchStatusApproved {
		t.Errorf("batch status = %q, want APPROVED", gotBatch.Status)
	}

	mapping, err := svc.GetMappingByExternalID(ctx, connB, integration.SupplyEntityTypeProduct, "prod-B", nil)
	if err != nil {
		t.Fatalf("GetMappingByExternalID: %v", err)
	}
	if mapping == nil || mapping.InternalID != "prod-canonical-B" || mapping.AuthoritySource != integration.MappingAuthorityManual {
		t.Fatalf("mapping = %+v, want canonical-B with MANUAL authority", mapping)
	}
	if !strings.Contains(string(mapping.Provenance), batchB.ID.String()) {
		t.Errorf("mapping provenance missing approved batch id: %s", mapping.Provenance)
	}
}

func TestSupplyCrossMerchantSKUIsNotADuplicate(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchant1 := createTestMerchant(t, db)
	merchant2 := createTestMerchant(t, db)
	conn1 := createSupplyConnection(t, merchantSvc, merchant1, "m1 connection")
	conn2 := createSupplyConnection(t, merchantSvc, merchant2, "m2 connection")

	svc := newSupplyService(t, db)
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchant1, ConnectionID: conn1, IdempotencyKey: "m1",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "p1", "SHARED-SKU")},
	}, "c", "u"); err != nil {
		t.Fatalf("import m1: %v", err)
	}
	_, recs2, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchant2, ConnectionID: conn2, IdempotencyKey: "m2",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "p2", "SHARED-SKU")},
	}, "c", "u")
	if err != nil {
		t.Fatalf("import m2: %v", err)
	}
	if recs2[0].Status != integration.SupplyRecordStatusStaged {
		t.Errorf("cross-merchant SKU marked duplicate: %q; duplicates are scoped per Merchant", recs2[0].Status)
	}
}

func TestSupplyFulfillmentRequestIdempotency(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "fulfillment connection")

	svc := newSupplyService(t, db)
	input := integration.CreateFulfillmentRequestInput{
		MerchantID:     merchantID,
		ConnectionID:   connID,
		IdempotencyKey: "fulfill-1",
		Payload:        json.RawMessage(`{"items":[{"sku":"SKU-1","quantity":2}]}`),
		CorrelationID:  "corr-1",
		CausationID:    "cause-1",
	}

	req1, err := svc.CreateFulfillmentRequest(ctx, input)
	if err != nil {
		t.Fatalf("CreateFulfillmentRequest: %v", err)
	}
	if req1.Status != "REQUESTED" {
		t.Errorf("status = %q, want REQUESTED", req1.Status)
	}
	req2, err := svc.CreateFulfillmentRequest(ctx, input)
	if err != nil {
		t.Fatalf("retry CreateFulfillmentRequest: %v", err)
	}
	if req1.ID != req2.ID {
		t.Errorf("retry created new request %s, want %s", req2.ID, req1.ID)
	}
	if got := countOutboxEvents(t, db, "event_type = $1 AND payload->>'idempotency_key' = $2",
		integration.EventTypeSupplyFulfillmentRequested, "fulfill-1"); got != 1 {
		t.Errorf("fulfillment_requested events = %d, want 1", got)
	}

	updated, err := svc.UpdateFulfillmentRequestStatus(ctx, req1.ID, "SHIPPED", strPtr("ext-fulfill-9"))
	if err != nil {
		t.Fatalf("UpdateFulfillmentRequestStatus: %v", err)
	}
	if updated.Status != "SHIPPED" || updated.ExternalFulfillmentID == nil || *updated.ExternalFulfillmentID != "ext-fulfill-9" {
		t.Errorf("updated request = %+v", updated)
	}
}

func TestSupplyTrackingEventIdempotency(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "tracking connection")

	svc := newSupplyService(t, db)
	req, err := svc.CreateFulfillmentRequest(ctx, integration.CreateFulfillmentRequestInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "fulfill-track",
	})
	if err != nil {
		t.Fatalf("CreateFulfillmentRequest: %v", err)
	}

	input := integration.RecordTrackingEventInput{
		RequestID:       req.ID,
		ConnectionID:    connID,
		ExternalEventID: "evt-1",
		Status:          "SHIPPED",
		Carrier:         strPtr("Aramex"),
		TrackingNumber:  strPtr("TRK-1"),
		Payload:         json.RawMessage(`{"state":"in_transit"}`),
	}
	ev1, isNew, err := svc.RecordTrackingEvent(ctx, input, "c", "u")
	if err != nil {
		t.Fatalf("RecordTrackingEvent: %v", err)
	}
	if !isNew {
		t.Errorf("first delivery isNew = false, want true")
	}
	ev2, isNew2, err := svc.RecordTrackingEvent(ctx, input, "c", "u")
	if err != nil {
		t.Fatalf("replay RecordTrackingEvent: %v", err)
	}
	if isNew2 {
		t.Errorf("replay isNew = true, want false")
	}
	if ev1.ID != ev2.ID {
		t.Errorf("replay returned different event %s, want %s", ev2.ID, ev1.ID)
	}
	if got := countOutboxEvents(t, db, "event_type = $1 AND payload->>'external_event_id' = $2",
		integration.EventTypeSupplyTrackingRecorded, "evt-1"); got != 1 {
		t.Errorf("tracking_recorded events = %d, want 1", got)
	}

	// Cross-connection replay must not be attributed to the wrong connection.
	otherConn := createSupplyConnection(t, merchantSvc, merchantID, "other connection")
	otherReq, err := svc.CreateFulfillmentRequest(ctx, integration.CreateFulfillmentRequestInput{
		MerchantID: merchantID, ConnectionID: otherConn, IdempotencyKey: "fulfill-other",
	})
	if err != nil {
		t.Fatalf("CreateFulfillmentRequest other: %v", err)
	}
	if _, _, err := svc.RecordTrackingEvent(ctx, integration.RecordTrackingEventInput{
		RequestID: otherReq.ID, ConnectionID: otherConn, ExternalEventID: "evt-1", Status: "SHIPPED",
	}, "c", "u"); err != nil {
		t.Fatalf("tracking for other connection: %v", err)
	}
}

func TestSupplyWebhookInboxDeduplication(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()
	svc := newSupplyService(t, db)

	input := integration.RecordWebhookInboxInput{
		Provider:       integration.ProviderShopify,
		EventType:      "products/update",
		IdempotencyKey: "webhook-1",
		Payload:        json.RawMessage(`{"id":123}`),
	}
	item1, isNew, err := svc.RecordWebhookInbox(ctx, input)
	if err != nil {
		t.Fatalf("RecordWebhookInbox: %v", err)
	}
	if !isNew {
		t.Errorf("first delivery isNew = false")
	}
	item2, isNew2, err := svc.RecordWebhookInbox(ctx, input)
	if err != nil {
		t.Fatalf("replay RecordWebhookInbox: %v", err)
	}
	if isNew2 || item1.ID != item2.ID {
		t.Errorf("replay isNew=%v id1=%s id2=%s; want duplicate recognition", isNew2, item1.ID, item2.ID)
	}
}

func TestSupplyCursorRoundTrip(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "cursor connection")
	svc := newSupplyService(t, db)

	token := "page-2"
	saved, err := svc.UpsertSyncCursor(ctx, integration.MerchantSyncCursor{
		ConnectionID: connID, EntityType: integration.SupplyEntityTypeProduct, CursorToken: &token,
	})
	if err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}
	got, err := svc.GetSyncCursor(ctx, connID, integration.SupplyEntityTypeProduct)
	if err != nil {
		t.Fatalf("GetSyncCursor: %v", err)
	}
	if got == nil || got.CursorToken == nil || *got.CursorToken != "page-2" || got.ID != saved.ID {
		t.Fatalf("cursor round trip mismatch: %+v", got)
	}
	// Advance the cursor: restart-safe sync resumes here.
	token2 := "page-3"
	if _, err := svc.UpsertSyncCursor(ctx, integration.MerchantSyncCursor{
		ConnectionID: connID, EntityType: integration.SupplyEntityTypeProduct, CursorToken: &token2,
	}); err != nil {
		t.Fatalf("advance cursor: %v", err)
	}
	got2, _ := svc.GetSyncCursor(ctx, connID, integration.SupplyEntityTypeProduct)
	if got2.CursorToken == nil || *got2.CursorToken != "page-3" {
		t.Errorf("cursor did not advance: %+v", got2.CursorToken)
	}
}

func TestSupplyConnectionGuards(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	draftConn, err := merchantSvc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:     merchantID,
		ConnectionType: integration.ConnectionTypeSupplySource,
		Provider:       integration.ProviderCustomAPI,
		Name:           "not yet active",
	}, "", "")
	if err != nil {
		t.Fatalf("create draft connection: %v", err)
	}

	svc := newSupplyService(t, db)
	// Work may not proceed on a non-ACTIVE connection.
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: draftConn.ID, IdempotencyKey: "x",
	}, "c", "u"); err != integration.ErrConnectionNotActive {
		t.Fatalf("staging on DRAFT connection error = %v, want ErrConnectionNotActive", err)
	}
	// Cross-merchant staging cannot even see the connection.
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: uuid.New(), ConnectionID: draftConn.ID, IdempotencyKey: "y",
	}, "c", "u"); err != integration.ErrConnectionNotFound {
		t.Fatalf("cross-merchant staging error = %v, want ErrConnectionNotFound", err)
	}
}

func TestSupplyMigration000039DownUpReapplication(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for database-backed integration tests")
	}
	db := setupSupplyTestDB(t)

	downPath := filepath.Join("..", "..", "migrations", "000039_integration_supply_staging.down.sql")
	upPath := filepath.Join("..", "..", "migrations", "000039_integration_supply_staging.up.sql")
	testdb.ApplyMigrations(t, db, downPath)
	testdb.ApplyMigrations(t, db, upPath)

	// Reapplication must be safe: constraints and trigger exist and work.
	ctx := context.Background()
	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "reapply connection")
	svc := newSupplyService(t, db)
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "reapply",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "p", "S")},
	}, "c", "u"); err != nil {
		t.Fatalf("staging after down/up reapplication: %v", err)
	}
}

func TestSupplyConnectionTypeImmutableAfterFirstSync(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "immutable type")
	svc := newSupplyService(t, db)
	if _, _, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "first",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "p", "S-IMM")},
	}, "c", "u"); err != nil {
		t.Fatalf("first import: %v", err)
	}

	// After the first successful synchronization (staged batch exists) the
	// connection type can never change, at any boundary.
	_, err := db.Exec(ctx,
		`UPDATE merchant_integration_connections SET connection_type = 'RETAIL_CHANNEL' WHERE id = $1`, connID)
	if err == nil || !strings.Contains(err.Error(), "connection_type is immutable") {
		t.Fatalf("type flip error = %v, want immutability violation", err)
	}
}

func strPtr(s string) *string { return &s }

func TestSupplyApprovedMappingReceivesAutomaticUpdates(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "auto-update connection")
	svc := newSupplyService(t, db)

	// First import: staged then approved with an explicit mapping.
	batch1, recs1, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "auto-1",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypePrice, "prod-1", "AUTO-SKU")},
	}, "c", "u")
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := svc.ApproveImportBatch(ctx, integration.ApproveSupplyImportBatchInput{
		BatchID: batch1.ID, MerchantID: merchantID,
		Decisions: []integration.RecordDecision{{RecordID: recs1[0].ID, Decision: "APPROVE", InternalID: "offer-1"}},
	}, "c", "u"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Subsequent price update for the approved mapping: no manual review.
	updated := supplyRecord(integration.SupplyEntityTypePrice, "prod-1", "AUTO-SKU")
	updated.ContentDigest = "digest-v2"
	updated.ExternalVersion = strPtr("v2")
	batch2, recs2, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "auto-2",
		Records: []integration.StagedRecordInput{updated},
	}, "c", "u")
	if err != nil {
		t.Fatalf("subsequent update: %v", err)
	}
	if batch2.FirstImport {
		t.Errorf("second batch must not be first_import")
	}
	if recs2[0].Status != integration.SupplyRecordStatusApproved {
		t.Fatalf("approved-mapping update status = %q, want automatic APPROVED", recs2[0].Status)
	}
	mapping, err := svc.GetMappingByExternalID(ctx, connID, integration.SupplyEntityTypePrice, "prod-1", nil)
	if err != nil {
		t.Fatalf("GetMappingByExternalID: %v", err)
	}
	if mapping.ExternalVersion == nil || *mapping.ExternalVersion != "v2" {
		t.Errorf("mapping version not refreshed: %+v", mapping.ExternalVersion)
	}
}

func TestSupplyStructuralChangeReturnsToReview(t *testing.T) {
	db := setupSupplyTestDB(t)
	ctx := context.Background()

	merchantSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	merchantID := createTestMerchant(t, db)
	connID := createSupplyConnection(t, merchantSvc, merchantID, "structural connection")
	svc := newSupplyService(t, db)

	batch1, recs1, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "struct-1",
		Records: []integration.StagedRecordInput{supplyRecord(integration.SupplyEntityTypeProduct, "prod-1", "STRUCT-SKU")},
	}, "c", "u")
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := svc.ApproveImportBatch(ctx, integration.ApproveSupplyImportBatchInput{
		BatchID: batch1.ID, MerchantID: merchantID,
		Decisions: []integration.RecordDecision{{RecordID: recs1[0].ID, Decision: "APPROVE", InternalID: "canon-1"}},
	}, "c", "u"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// A structural change (new SKU for the mapped external product) must not
	// update automatically: it returns to review.
	changed := supplyRecord(integration.SupplyEntityTypeProduct, "prod-1", "STRUCT-SKU-CHANGED")
	changed.ContentDigest = "digest-structural-change"
	batch2, recs2, err := svc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID: merchantID, ConnectionID: connID, IdempotencyKey: "struct-2",
		Records: []integration.StagedRecordInput{changed},
	}, "c", "u")
	if err != nil {
		t.Fatalf("structural update: %v", err)
	}
	if recs2[0].Status != integration.SupplyRecordStatusReviewRequired {
		t.Fatalf("structural-change status = %q, want REVIEW_REQUIRED", recs2[0].Status)
	}
	cases, err := svc.ListReviewCases(ctx, merchantID, &connID, integration.ReviewCaseStatusOpen)
	if err != nil {
		t.Fatalf("ListReviewCases: %v", err)
	}
	found := false
	for _, c := range cases {
		if c.ReasonCode == integration.ReviewReasonStructuralChange {
			found = true
		}
	}
	if !found {
		t.Errorf("no STRUCTURAL_CHANGE review case created")
	}
	mapping, err := svc.GetMappingByExternalID(ctx, connID, integration.SupplyEntityTypeProduct, "prod-1", nil)
	if err != nil {
		t.Fatalf("GetMappingByExternalID: %v", err)
	}
	if mapping.Status != integration.MappingStatusReviewRequired {
		t.Errorf("mapping status = %q, want REVIEW_REQUIRED", mapping.Status)
	}
	_ = batch2
}
