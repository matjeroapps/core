package coreapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/coreapi"
	"core/internal/integration"
	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

// Synthetic, valueless fixture strings for staging inputs.
const (
	fixtureIdemImportA = "int-console-import"
	fixtureIdemFulA    = "int-console-fulfillment"
	fixtureIdemImportB = "int-other-import"
	fixtureIdemFulB    = "int-other-fulfillment"
)

type consoleFixture struct {
	db                *database.Pool
	handler           http.Handler
	merchantID        uuid.UUID
	otherID           uuid.UUID
	subject           string
	connectionID      uuid.UUID
	batchID           uuid.UUID
	reviewCaseID      uuid.UUID
	mappingID         uuid.UUID
	requestID         uuid.UUID
	otherBatchID      uuid.UUID
	otherReviewCaseID uuid.UUID
	otherMappingID    uuid.UUID
	otherRequestID    uuid.UUID
}

func setupMerchantConsoleAPITest(t *testing.T) *consoleFixture {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for Core API integration tests")
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
		"000040_merchant_membership_subject_index",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)

	ctx := context.Background()
	mRepo := merchants.NewPostgresRepository(db.Pool)
	mSvc := merchants.NewService(mRepo)
	authorizer := merchants.NewAuthorizer(mRepo, merchants.AuthModeCanonical)

	merchant, err := mSvc.CreateMerchant(ctx, "CONSOLE-A", "Console Merchant A", merchants.CapabilityTypeSupply)
	if err != nil {
		t.Fatalf("create merchant: %v", err)
	}
	other, err := mSvc.CreateMerchant(ctx, "CONSOLE-B", "Console Merchant B", merchants.CapabilityTypeSupply)
	if err != nil {
		t.Fatalf("create other merchant: %v", err)
	}

	subject := "console-owner-subject"
	if _, err := mSvc.AddMember(ctx, merchant.ID, subject, []string{
		merchants.PermissionMerchantManage,
		merchants.PermissionSupplyCatalogManage,
		merchants.PermissionSupplyFulfillment,
		merchants.PermissionRetailStoresManage,
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := mSvc.AddMember(ctx, other.ID, "console-other-subject", []string{
		merchants.PermissionMerchantManage,
		merchants.PermissionSupplyCatalogManage,
	}); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	// Supply data for the primary merchant, created through the authoritative
	// services (the same way the Integration Hub would create it).
	supplySvc := integration.NewSupplyService(db.Pool)
	intSvc := integration.NewMerchantService(integration.NewMerchantRepository(db.Pool), db.Pool)
	extAccount := "ext-console-a"
	conn, err := intSvc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:        merchant.ID,
		ConnectionType:    integration.ConnectionTypeSupplySource,
		Provider:          integration.ProviderSalla,
		ExternalAccountID: &extAccount,
		Name:              "console supply source",
	}, "", "")
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	if _, err := intSvc.UpdateMerchantConnectionStatus(ctx, conn.ID, integration.MerchantStatusActive, "", ""); err != nil {
		t.Fatalf("activate connection: %v", err)
	}

	sku := "SKU-CONSOLE-1"
	batch, _, err := supplySvc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID:     merchant.ID,
		ConnectionID:   conn.ID,
		BatchType:      integration.SupplyBatchTypeFirstImport,
		IdempotencyKey: fixtureIdemImportA,
		Records: []integration.StagedRecordInput{{
			EntityType:        integration.SupplyEntityTypeProduct,
			ExternalProductID: "prod-1",
			SKU:               &sku,
			ContentDigest:     "digest-prod-1",
		}},
	}, "", "")
	if err != nil {
		t.Fatalf("create import batch: %v", err)
	}

	// Review case + mapping rows are created directly through the persisted
	// state the services themselves write.
	reviewCaseID := uuid.New()
	if _, err := db.Exec(ctx, `
		INSERT INTO merchant_integration_review_cases (id, merchant_id, connection_id, case_type, status, reason_code, details)
		VALUES ($1, $2, $3, 'DUPLICATE_SKU', 'OPEN', 'DUPLICATE_SKU', '{}'::jsonb)
	`, reviewCaseID, merchant.ID, conn.ID); err != nil {
		t.Fatalf("insert review case: %v", err)
	}
	mappingID := uuid.New()
	if _, err := db.Exec(ctx, `
		INSERT INTO merchant_integration_entity_mappings (id, merchant_id, connection_id, entity_type, internal_id, external_product_id, authority_source, provenance, status)
		VALUES ($1, $2, $3, 'PRODUCT', 'internal-1', 'prod-1', 'MANUAL', '{}'::jsonb, 'ACTIVE')
	`, mappingID, merchant.ID, conn.ID); err != nil {
		t.Fatalf("insert mapping: %v", err)
	}
	if _, err := supplySvc.UpsertSyncCursor(ctx, integration.MerchantSyncCursor{
		ConnectionID: conn.ID,
		EntityType:   integration.SupplyEntityTypeProduct,
	}); err != nil {
		t.Fatalf("upsert cursor: %v", err)
	}
	fulfillment, err := supplySvc.CreateFulfillmentRequest(ctx, integration.CreateFulfillmentRequestInput{
		MerchantID:     merchant.ID,
		ConnectionID:   conn.ID,
		IdempotencyKey: fixtureIdemFulA,
		Payload:        []byte(`{"order_ref":"ord-1"}`),
	})
	if err != nil {
		t.Fatalf("create fulfillment request: %v", err)
	}
	if _, _, err := supplySvc.RecordTrackingEvent(ctx, integration.RecordTrackingEventInput{
		RequestID:       fulfillment.ID,
		ConnectionID:    conn.ID,
		ExternalEventID: "evt-1",
		Status:          "IN_TRANSIT",
	}, "", ""); err != nil {
		t.Fatalf("record tracking event: %v", err)
	}

	// Independent supply data owned by the other Merchant (cross-Merchant probe).
	otherExt := "ext-console-b"
	otherConn, err := intSvc.CreateMerchantConnection(ctx, integration.CreateMerchantConnectionInput{
		MerchantID:        other.ID,
		ConnectionType:    integration.ConnectionTypeSupplySource,
		Provider:          integration.ProviderSalla,
		ExternalAccountID: &otherExt,
		Name:              "other supply source",
	}, "", "")
	if err != nil {
		t.Fatalf("create other connection: %v", err)
	}
	if _, err := intSvc.UpdateMerchantConnectionStatus(ctx, otherConn.ID, integration.MerchantStatusActive, "", ""); err != nil {
		t.Fatalf("activate other connection: %v", err)
	}
	otherBatch, _, err := supplySvc.CreateImportBatch(ctx, integration.CreateSupplyImportBatchInput{
		MerchantID:     other.ID,
		ConnectionID:   otherConn.ID,
		BatchType:      integration.SupplyBatchTypeFirstImport,
		IdempotencyKey: fixtureIdemImportB,
	}, "", "")
	if err != nil {
		t.Fatalf("create other batch: %v", err)
	}
	otherCaseID := uuid.New()
	if _, err := db.Exec(ctx, `
		INSERT INTO merchant_integration_review_cases (id, merchant_id, connection_id, case_type, status, reason_code, details)
		VALUES ($1, $2, $3, 'DUPLICATE_SKU', 'OPEN', 'DUPLICATE_SKU', '{}'::jsonb)
	`, otherCaseID, other.ID, otherConn.ID); err != nil {
		t.Fatalf("insert other review case: %v", err)
	}
	otherMappingID := uuid.New()
	if _, err := db.Exec(ctx, `
		INSERT INTO merchant_integration_entity_mappings (id, merchant_id, connection_id, entity_type, internal_id, external_product_id, authority_source, provenance, status)
		VALUES ($1, $2, $3, 'PRODUCT', 'other-internal-1', 'other-prod-1', 'MANUAL', '{}'::jsonb, 'ACTIVE')
	`, otherMappingID, other.ID, otherConn.ID); err != nil {
		t.Fatalf("insert other mapping: %v", err)
	}
	otherFulfillment, err := supplySvc.CreateFulfillmentRequest(ctx, integration.CreateFulfillmentRequestInput{
		MerchantID:     other.ID,
		ConnectionID:   otherConn.ID,
		IdempotencyKey: fixtureIdemFulB,
	})
	if err != nil {
		t.Fatalf("create other fulfillment request: %v", err)
	}

	deps := coreapi.Dependencies{
		Commerce:            commerce.NewService(commerce.NewRepository(db.Pool)),
		Merchants:           mSvc,
		MerchantBootstrap:   merchants.NewBootstrapService(db.Pool, mRepo),
		MerchantAuthorizer:  authorizer,
		MerchantIntegration: intSvc,
		SupplyIntegration:   supplySvc,
	}
	router := coreapi.NewRouter(deps)

	handler := serviceauth.Middleware(serviceauth.Config{
		Tokens: map[serviceauth.Caller]string{
			serviceauth.CallerSeller:   "test-seller-token",
			serviceauth.CallerSupplier: "test-supplier-token",
			serviceauth.CallerPlatform: "test-platform-token",
			serviceauth.CallerAdmin:    "test-admin-token",
		},
	})(router)

	return &consoleFixture{
		db:                db,
		handler:           handler,
		merchantID:        merchant.ID,
		otherID:           other.ID,
		subject:           subject,
		connectionID:      conn.ID,
		batchID:           batch.ID,
		reviewCaseID:      reviewCaseID,
		mappingID:         mappingID,
		requestID:         fulfillment.ID,
		otherBatchID:      otherBatch.ID,
		otherReviewCaseID: otherCaseID,
		otherMappingID:    otherMappingID,
		otherRequestID:    otherFulfillment.ID,
	}
}

// consoleGet issues a service-authenticated GET as the named caller with the
// forwarded subject. Browser-supplied internal headers are intentionally not
// set here except through caller/subject arguments, mirroring the actor side
// of the trusted boundary.
func consoleGet(t *testing.T, f *consoleFixture, caller serviceauth.Caller, token, subject, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, bytes.NewReader(nil))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(string(serviceauth.HeaderService), string(caller))
	if subject != "" {
		req.Header.Set(serviceauth.HeaderSubject, subject)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decode response %s: %v", string(body), err)
	}
}

func TestMerchantBootstrapEndpointAuthorization(t *testing.T) {
	f := setupMerchantConsoleAPITest(t)

	// Seller service caller (the console BFF) resolves the subject's workspaces.
	rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, "/internal/v1/merchants/bootstrap")
	if rec.Code != http.StatusOK {
		t.Fatalf("seller bootstrap: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var bs struct {
		Subject    string `json:"subject"`
		Workspaces []struct {
			MerchantID   uuid.UUID `json:"merchant_id"`
			Capabilities *struct {
				Supply *struct {
					Status string `json:"status"`
				} `json:"supply"`
			} `json:"capabilities"`
		} `json:"workspaces"`
	}
	decodeBody(t, rec, &bs)
	if bs.Subject != f.subject {
		t.Errorf("expected subject echo %s, got %s", f.subject, bs.Subject)
	}
	if len(bs.Workspaces) != 1 || bs.Workspaces[0].MerchantID != f.merchantID {
		t.Fatalf("expected exactly the member merchant, got %+v", bs.Workspaces)
	}
	if bs.Workspaces[0].Capabilities == nil || bs.Workspaces[0].Capabilities.Supply == nil || bs.Workspaces[0].Capabilities.Supply.Status != "active" {
		t.Errorf("expected active supply capability, got %+v", bs.Workspaces[0].Capabilities)
	}

	// Supplier caller is accepted (legacy compatibility eligibility only).
	rec = consoleGet(t, f, serviceauth.CallerSupplier, "test-supplier-token", f.subject, "/internal/v1/merchants/bootstrap")
	if rec.Code != http.StatusOK {
		t.Fatalf("supplier bootstrap: expected 200, got %d", rec.Code)
	}

	// Platform and admin callers are disallowed for this surface.
	for _, tc := range []struct {
		caller serviceauth.Caller
		token  string
	}{{serviceauth.CallerPlatform, "test-platform-token"}, {serviceauth.CallerAdmin, "test-admin-token"}} {
		rec = consoleGet(t, f, tc.caller, tc.token, f.subject, "/internal/v1/merchants/bootstrap")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s caller: expected 403, got %d", tc.caller, rec.Code)
		}
	}

	// A seller token presented under a different caller name is rejected
	// (credential/caller binding part of the spoofing boundary).
	rec = consoleGet(t, f, serviceauth.CallerPlatform, "test-seller-token", f.subject, "/internal/v1/merchants/bootstrap")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("caller/token mismatch: expected 401, got %d", rec.Code)
	}

	// Missing service credentials fail closed.
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/merchants/bootstrap", nil)
	req.Header.Set(serviceauth.HeaderSubject, f.subject)
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous request: expected 401, got %d", rec.Code)
	}
}

func TestMerchantBootstrapEndpointMembershipShapes(t *testing.T) {
	f := setupMerchantConsoleAPITest(t)

	// No canonical membership: empty workspaces, never a guessed merchant.
	rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", "console-unknown-subject", "/internal/v1/merchants/bootstrap")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var bs struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	decodeBody(t, rec, &bs)
	if len(bs.Workspaces) != 0 {
		t.Errorf("expected empty workspaces, got %+v", bs.Workspaces)
	}

	// Invited membership: workspace identity but no capability/store/plan data.
	ctx := context.Background()
	mRepo := merchants.NewPostgresRepository(f.db.Pool)
	mSvc := merchants.NewService(mRepo)
	invitedMerchant, err := mSvc.CreateMerchant(ctx, "CONSOLE-INV", "Invited Merchant", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("create invited merchant: %v", err)
	}
	mem, err := mSvc.AddMember(ctx, invitedMerchant.ID, "console-invited-subject", []string{merchants.PermissionRetailStoresManage})
	if err != nil {
		t.Fatalf("add invited member: %v", err)
	}
	if _, err := f.db.Exec(ctx, "UPDATE merchant_memberships SET status = 'invited' WHERE id = $1", mem.ID); err != nil {
		t.Fatalf("invite member: %v", err)
	}

	rec = consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", "console-invited-subject", "/internal/v1/merchants/bootstrap")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var invitedBS struct {
		Workspaces []struct {
			MerchantID     uuid.UUID        `json:"merchant_id"`
			Capabilities   map[string]any   `json:"capabilities"`
			Stores         []map[string]any `json:"stores"`
			PlanSummary    map[string]any   `json:"plan_summary"`
			PendingActions map[string]any   `json:"pending_actions"`
		} `json:"workspaces"`
	}
	decodeBody(t, rec, &invitedBS)
	if len(invitedBS.Workspaces) != 1 || invitedBS.Workspaces[0].MerchantID != invitedMerchant.ID {
		t.Fatalf("expected invited workspace present, got %+v", invitedBS.Workspaces)
	}
	ws := invitedBS.Workspaces[0]
	if ws.Capabilities != nil || len(ws.Stores) != 0 || ws.PlanSummary != nil || ws.PendingActions != nil {
		t.Errorf("invited membership leaked operable data: %+v", ws)
	}
}

func TestMerchantSupplyReadEndpoints(t *testing.T) {
	f := setupMerchantConsoleAPITest(t)
	base := "/internal/v1/merchants/" + f.merchantID.String() + "/integrations/supply"
	get := func(path string) *httptest.ResponseRecorder {
		return consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, path)
	}

	// Import batch list contains only the Merchant's own batch.
	rec := get(base + "/import-batches")
	if rec.Code != http.StatusOK {
		t.Fatalf("import batches list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var batches []map[string]any
	decodeBody(t, rec, &batches)
	if len(batches) != 1 || batches[0]["id"] != f.batchID.String() {
		t.Fatalf("expected exactly the merchant's batch, got %+v", batches)
	}

	// Empty collections return genuine empty results.
	rec = get(base + "/import-batches?connection_id=" + uuid.NewString())
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered list: expected 200, got %d", rec.Code)
	}
	decodeBody(t, rec, &batches)
	if batches == nil || len(batches) != 0 {
		t.Errorf("expected genuine empty list, got %+v", batches)
	}

	// Batch detail includes staged records.
	rec = get(base + "/import-batches/" + f.batchID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("batch detail: expected 200, got %d", rec.Code)
	}
	var detail struct {
		Records []map[string]any `json:"records"`
	}
	decodeBody(t, rec, &detail)
	if len(detail.Records) != 1 {
		t.Errorf("expected 1 staged record, got %d", len(detail.Records))
	}

	// Review-case detail.
	rec = get(base + "/review-cases/" + f.reviewCaseID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("review case detail: expected 200, got %d", rec.Code)
	}

	// Mappings list and detail with authority/provenance.
	rec = get(base + "/mappings")
	if rec.Code != http.StatusOK {
		t.Fatalf("mappings list: expected 200, got %d", rec.Code)
	}
	var mappings []map[string]any
	decodeBody(t, rec, &mappings)
	if len(mappings) != 1 || mappings[0]["id"] != f.mappingID.String() {
		t.Fatalf("expected exactly the merchant's mapping, got %+v", mappings)
	}
	if mappings[0]["authority_source"] != "MANUAL" {
		t.Errorf("expected authority/provenance in mapping surface, got %+v", mappings[0])
	}
	rec = get(base + "/mappings/" + f.mappingID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("mapping detail: expected 200, got %d", rec.Code)
	}

	// Sync cursors.
	rec = get(base + "/cursors")
	if rec.Code != http.StatusOK {
		t.Fatalf("cursors list: expected 200, got %d", rec.Code)
	}
	var cursors []map[string]any
	decodeBody(t, rec, &cursors)
	if len(cursors) != 1 {
		t.Errorf("expected 1 sync cursor, got %d", len(cursors))
	}

	// Fulfillment requests with tracking events.
	rec = get(base + "/fulfillment-requests")
	if rec.Code != http.StatusOK {
		t.Fatalf("fulfillment list: expected 200, got %d", rec.Code)
	}
	var requests []map[string]any
	decodeBody(t, rec, &requests)
	if len(requests) != 1 || requests[0]["id"] != f.requestID.String() {
		t.Fatalf("expected exactly the merchant's fulfillment request, got %+v", requests)
	}
	rec = get(base + "/fulfillment-requests/" + f.requestID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("fulfillment detail: expected 200, got %d", rec.Code)
	}
	var fulfillmentDetail struct {
		TrackingEvents []map[string]any `json:"tracking_events"`
	}
	decodeBody(t, rec, &fulfillmentDetail)
	if len(fulfillmentDetail.TrackingEvents) != 1 {
		t.Errorf("expected 1 tracking event, got %d", len(fulfillmentDetail.TrackingEvents))
	}
	rec = get(base + "/fulfillment-requests/" + f.requestID.String() + "/tracking-events")
	if rec.Code != http.StatusOK {
		t.Fatalf("tracking events list: expected 200, got %d", rec.Code)
	}

	// Pagination is bounded.
	rec = get(base + "/import-batches?limit=1000&page=0")
	if rec.Code != http.StatusOK {
		t.Fatalf("bounded list: expected 200, got %d", rec.Code)
	}
}

func TestMerchantSupplyReadCrossMerchantDenied(t *testing.T) {
	f := setupMerchantConsoleAPITest(t)
	otherBase := "/internal/v1/merchants/" + f.otherID.String() + "/integrations/supply"

	// The console-owner subject has no membership in the other Merchant: every
	// cross-Merchant request is denied regardless of navigation state.
	paths := []string{
		otherBase + "/import-batches",
		otherBase + "/import-batches/" + f.batchID.String(),
		otherBase + "/review-cases",
		otherBase + "/mappings",
		otherBase + "/cursors",
		otherBase + "/fulfillment-requests",
	}
	for _, path := range paths {
		rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, path)
		if rec.Code != http.StatusForbidden {
			t.Errorf("cross-merchant %s: expected 403, got %d", path, rec.Code)
		}
	}

	// Foreign resource IDs inside the authorized Merchant are indistinguishable
	// from missing ones (no cross-Merchant lookup by raw ID).
	base := "/internal/v1/merchants/" + f.merchantID.String() + "/integrations/supply"
	foreignPaths := []string{
		base + "/import-batches/" + uuid.NewString(),
		base + "/review-cases/" + uuid.NewString(),
		base + "/mappings/" + uuid.NewString(),
		base + "/fulfillment-requests/" + uuid.NewString(),
	}
	for _, path := range foreignPaths {
		rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("foreign id %s: expected 404, got %d", path, rec.Code)
		}
	}

	// The other Merchant's own IDs do not resolve inside this Merchant either.
	otherIDs := []string{
		base + "/import-batches/" + f.otherBatchID.String(),
		base + "/review-cases/" + f.otherReviewCaseID.String(),
		base + "/mappings/" + f.otherMappingID.String(),
		base + "/fulfillment-requests/" + f.otherRequestID.String(),
	}
	for _, path := range otherIDs {
		rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("other merchant id %s: expected 404, got %d", path, rec.Code)
		}
	}
}

func TestMerchantSupplyReadAuthorizationChain(t *testing.T) {
	f := setupMerchantConsoleAPITest(t)
	ctx := context.Background()

	mRepo := merchants.NewPostgresRepository(f.db.Pool)
	mSvc := merchants.NewService(mRepo)
	base := "/internal/v1/merchants/" + f.merchantID.String() + "/integrations/supply/import-batches"

	// A member without supply permissions is denied even with active membership.
	if _, err := mSvc.AddMember(ctx, f.merchantID, "console-retail-only-subject", []string{merchants.PermissionRetailStoresManage}); err != nil {
		t.Fatalf("add retail-only member: %v", err)
	}
	rec := consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", "console-retail-only-subject", base)
	if rec.Code != http.StatusForbidden {
		t.Errorf("retail-only member: expected 403, got %d", rec.Code)
	}

	// A suspended SUPPLY capability denies supply reads.
	if _, err := mSvc.SuspendCapability(ctx, f.merchantID, merchants.CapabilityTypeSupply); err != nil {
		t.Fatalf("suspend supply capability: %v", err)
	}
	rec = consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, base)
	if rec.Code != http.StatusForbidden {
		t.Errorf("suspended capability: expected 403, got %d", rec.Code)
	}
	if _, err := mSvc.ActivateCapability(ctx, f.merchantID, merchants.CapabilityTypeSupply); err != nil {
		t.Fatalf("reactivate supply capability: %v", err)
	}

	// A suspended membership denies supply reads.
	mem, err := mRepo.GetMembership(ctx, f.merchantID, f.subject)
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if _, err := f.db.Exec(ctx, "UPDATE merchant_memberships SET status = 'suspended' WHERE id = $1", mem.ID); err != nil {
		t.Fatalf("suspend membership: %v", err)
	}
	rec = consoleGet(t, f, serviceauth.CallerSeller, "test-seller-token", f.subject, base)
	if rec.Code != http.StatusForbidden {
		t.Errorf("suspended membership: expected 403, got %d", rec.Code)
	}
}
