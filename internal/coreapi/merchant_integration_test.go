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

func setupMerchantIntegrationAPITest(t *testing.T) (*database.Pool, http.Handler, uuid.UUID, string) {
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

	merchant, err := mSvc.CreateMerchant(ctx, "M-API-1", "API Merchant", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant: %v", err)
	}
	if _, err := mSvc.ActivateCapability(ctx, merchant.ID, merchants.CapabilityTypeSupply); err != nil {
		t.Fatalf("failed to activate supply capability: %v", err)
	}

	subject := "user-subject-123"
	_, err = mSvc.AddMember(ctx, merchant.ID, subject, []string{
		merchants.PermissionRetailStoresManage,
		merchants.PermissionSupplyCatalogManage,
		merchants.PermissionMerchantManage,
	})
	if err != nil {
		t.Fatalf("failed to add member: %v", err)
	}

	intRepo := integration.NewMerchantRepository(db.Pool)
	intSvc := integration.NewMerchantService(intRepo, db.Pool)

	deps := coreapi.Dependencies{
		Commerce:            commerce.NewService(commerce.NewRepository(db.Pool)),
		Merchants:           mSvc,
		MerchantIntegration: intSvc,
		MerchantAuthorizer:  authorizer,
	}

	router := coreapi.NewRouter(deps)

	authMiddleware := serviceauth.Middleware(serviceauth.Config{
		Tokens: map[serviceauth.Caller]string{
			serviceauth.CallerPlatform: "test-platform-token",
		},
	})

	handler := authMiddleware(router)

	return db, handler, merchant.ID, subject
}

// createTestStore inserts the seller and store rows required by RETAIL_CHANNEL
// connections and returns the store ID.
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

func TestMerchantIntegrationAPIEndpointsAndAuthorization(t *testing.T) {
	db, handler, merchantID, subject := setupMerchantIntegrationAPITest(t)
	storeID := createTestStore(t, db, merchantID)

	// 1. Create Connection (Retail Channel)
	extAccount := "ext-api-shop-1"
	createReq := coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAccount,
		Name:              "Shopify Channel 1",
		StoreID:           &storeID,
	}
	body, _ := json.Marshal(createReq)

	req := httptest.NewRequest("POST", "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-platform-token")
	req.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	req.Header.Set(serviceauth.HeaderSubject, subject)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var conn integration.MerchantIntegrationConnection
	if err := json.Unmarshal(rec.Body.Bytes(), &conn); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if conn.ID == uuid.Nil {
		t.Errorf("expected non-nil connection ID")
	}

	var jobIntentCount int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM merchant_integration_job_intents").Scan(&jobIntentCount); err != nil {
		t.Fatalf("failed to count job intents: %v", err)
	}
	if jobIntentCount != 0 {
		t.Errorf("expected create connection API to create zero runtime job intents in I1, got %d", jobIntentCount)
	}

	// 2. Get Connection
	getReq := httptest.NewRequest("GET", "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections/"+conn.ID.String(), nil)
	getReq.Header.Set("Authorization", "Bearer test-platform-token")
	getReq.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	getReq.Header.Set(serviceauth.HeaderSubject, subject)

	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// 3. List Connections
	listReq := httptest.NewRequest("GET", "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections", nil)
	listReq.Header.Set("Authorization", "Bearer test-platform-token")
	listReq.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	listReq.Header.Set(serviceauth.HeaderSubject, subject)

	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var conns []integration.MerchantIntegrationConnection
	_ = json.Unmarshal(listRec.Body.Bytes(), &conns)
	if len(conns) != 1 {
		t.Errorf("expected 1 connection in list, got %d", len(conns))
	}

	// 4. Update Status
	statusReq := coreapi.UpdateMerchantConnectionStatusHTTPRequest{
		Status: integration.MerchantStatusActive,
	}
	sBody, _ := json.Marshal(statusReq)
	patchReq := httptest.NewRequest("PATCH", "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections/"+conn.ID.String()+"/status", bytes.NewReader(sBody))
	patchReq.Header.Set("Authorization", "Bearer test-platform-token")
	patchReq.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	patchReq.Header.Set(serviceauth.HeaderSubject, subject)

	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)

	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status patch, got %d: %s", patchRec.Code, patchRec.Body.String())
	}

	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM merchant_integration_job_intents").Scan(&jobIntentCount); err != nil {
		t.Fatalf("failed to recount job intents: %v", err)
	}
	if jobIntentCount != 0 {
		t.Errorf("expected status patch API to create zero runtime job intents in I1, got %d", jobIntentCount)
	}

	// 5. Cross-Merchant Rejection
	otherMerchantID := uuid.New()
	crossReq := httptest.NewRequest("GET", "/internal/v1/merchants/"+otherMerchantID.String()+"/integrations/connections/"+conn.ID.String(), nil)
	crossReq.Header.Set("Authorization", "Bearer test-platform-token")
	crossReq.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	crossReq.Header.Set(serviceauth.HeaderSubject, subject)

	crossRec := httptest.NewRecorder()
	handler.ServeHTTP(crossRec, crossReq)

	if crossRec.Code != http.StatusNotFound && crossRec.Code != http.StatusForbidden {
		t.Errorf("expected 404 or 403 for cross-merchant request, got %d", crossRec.Code)
	}

	// 6. Unauthorized Member Rejection
	unauthReq := httptest.NewRequest("GET", "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections", nil)
	unauthReq.Header.Set("Authorization", "Bearer test-platform-token")
	unauthReq.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	unauthReq.Header.Set(serviceauth.HeaderSubject, "unauthorized-subject")

	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)

	if unauthRec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for unauthorized subject, got %d", unauthRec.Code)
	}
}

func doMerchantIntegrationRequest(t *testing.T, handler http.Handler, method, path string, body any, subject string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer test-platform-token")
	req.Header.Set(serviceauth.HeaderService, string(serviceauth.CallerPlatform))
	req.Header.Set(serviceauth.HeaderSubject, subject)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func createConnectionForTest(t *testing.T, handler http.Handler, merchantID uuid.UUID, subject string, externalAccountID *string, provider integration.Provider, connType integration.MerchantConnectionType, storeID *uuid.UUID) integration.MerchantIntegrationConnection {
	t.Helper()

	req := coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType:    connType,
		Provider:          provider,
		ExternalAccountID: externalAccountID,
		Name:              "Connection " + uuid.NewString()[:8],
		StoreID:           storeID,
	}
	rec := doMerchantIntegrationRequest(t, handler, http.MethodPost, "/internal/v1/merchants/"+merchantID.String()+"/integrations/connections", req, subject)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	var conn integration.MerchantIntegrationConnection
	if err := json.Unmarshal(rec.Body.Bytes(), &conn); err != nil {
		t.Fatalf("failed to decode created connection: %v", err)
	}
	return conn
}

// TestMerchantConnectionStatusPatchSemantics verifies the PATCH /status
// contract: only caller-driven statuses are accepted, every operational status
// requires an external account, and terminal statuses conflict.
func TestMerchantConnectionStatusPatchSemantics(t *testing.T) {
	db, handler, merchantID, subject := setupMerchantIntegrationAPITest(t)
	base := "/internal/v1/merchants/" + merchantID.String() + "/integrations/connections"

	// A connection without an external account cannot enter an operational
	// caller-managed state.
	storeID := createTestStore(t, db, merchantID)
	noAcc := createConnectionForTest(t, handler, merchantID, subject, nil, integration.ProviderShopify, integration.ConnectionTypeRetailChannel, &storeID)

	for _, status := range []integration.MerchantConnectionStatus{
		integration.MerchantStatusError,
		integration.MerchantStatusDraft,
		integration.MerchantStatusAuthorizing,
		integration.MerchantStatusConfiguring,
	} {
		rec := doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+noAcc.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: status}, subject)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("system-controlled status %s should be rejected 422, got %d: %s", status, rec.Code, rec.Body.String())
		}
	}

	for _, status := range []integration.MerchantConnectionStatus{
		integration.MerchantStatusActive,
		integration.MerchantStatusPaused,
		integration.MerchantStatusRevoked,
		integration.MerchantStatusRetired,
	} {
		rec := doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+noAcc.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: status}, subject)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s without external_account_id should be 422, got %d: %s", status, rec.Code, rec.Body.String())
		}
	}

	blankExternalAccountID := "   "
	blankAccount := createConnectionForTest(t, handler, merchantID, subject, &blankExternalAccountID, integration.ProviderWooCommerce, integration.ConnectionTypeRetailChannel, &storeID)
	rec := doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+blankAccount.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: integration.MerchantStatusActive}, subject)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("ACTIVE with a blank external_account_id should be 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Connection with a verified external account can go ACTIVE, then RETIRED
	// is terminal.
	extAcc := "ext-status-flow"
	withAcc := createConnectionForTest(t, handler, merchantID, subject, &extAcc, integration.ProviderSalla, integration.ConnectionTypeRetailChannel, &storeID)

	rec = doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+withAcc.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: integration.MerchantStatusActive}, subject)
	if rec.Code != http.StatusOK {
		t.Fatalf("ACTIVE with external account should succeed, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+withAcc.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: integration.MerchantStatusRetired}, subject)
	if rec.Code != http.StatusOK {
		t.Fatalf("RETIRED should be accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doMerchantIntegrationRequest(t, handler, http.MethodPatch, base+"/"+withAcc.ID.String()+"/status", coreapi.UpdateMerchantConnectionStatusHTTPRequest{Status: integration.MerchantStatusActive}, subject)
	if rec.Code != http.StatusConflict {
		t.Errorf("transition out of terminal RETIRED should be 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMerchantConnectionCreateValidation verifies create-flow validation and
// conflict mapping: semantic violations 422, canonical and pending-setup
// duplicates 409 (deltas D2a/D6, per the internal API contract).
func TestMerchantConnectionCreateValidation(t *testing.T) {
	db, handler, merchantID, subject := setupMerchantIntegrationAPITest(t)
	storeID := createTestStore(t, db, merchantID)
	base := "/internal/v1/merchants/" + merchantID.String() + "/integrations/connections"

	// RETAIL_CHANNEL without store_id.
	rec := doMerchantIntegrationRequest(t, handler, http.MethodPost, base, coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType: integration.ConnectionTypeRetailChannel,
		Provider:       integration.ProviderShopify,
		Name:           "Retail Without Store",
	}, subject)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("RETAIL_CHANNEL without store_id should be 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Unknown connection_type.
	badType := integration.MerchantConnectionType("UNKNOWN_TYPE")
	rec = doMerchantIntegrationRequest(t, handler, http.MethodPost, base, coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType: badType,
		Provider:       integration.ProviderShopify,
		Name:           "Bad Type",
	}, subject)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown connection_type should be 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Canonical duplicate.
	extAcc := "ext-duplicate-account"
	createConnectionForTest(t, handler, merchantID, subject, &extAcc, integration.ProviderShopify, integration.ConnectionTypeRetailChannel, &storeID)
	rec = doMerchantIntegrationRequest(t, handler, http.MethodPost, base, coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType:    integration.ConnectionTypeRetailChannel,
		Provider:          integration.ProviderShopify,
		ExternalAccountID: &extAcc,
		Name:              "Canonical Duplicate",
		StoreID:           &storeID,
	}, subject)
	if rec.Code != http.StatusConflict {
		t.Errorf("canonical duplicate should be 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// Pending-setup duplicate: two rows without an external account for the
	// same (merchant, provider) violate the partial unique index.
	otherProvider := integration.ProviderWooCommerce
	createConnectionForTest(t, handler, merchantID, subject, nil, otherProvider, integration.ConnectionTypeRetailChannel, &storeID)
	rec = doMerchantIntegrationRequest(t, handler, http.MethodPost, base, coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType: integration.ConnectionTypeRetailChannel,
		Provider:       otherProvider,
		Name:           "Pending Duplicate",
		StoreID:        &storeID,
	}, subject)
	if rec.Code != http.StatusConflict {
		t.Errorf("pending-setup duplicate should be 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestMerchantConnectionReadScoping verifies type-derived capability and
// permission enforcement on reads: capability-scoped callers need the
// matching scope, unfiltered lists require merchant.manage, and
// cross-Merchant rows look identical to missing ones (delta D5).
func TestMerchantConnectionReadScoping(t *testing.T) {
	db, handler, merchantID, adminSubject := setupMerchantIntegrationAPITest(t)
	base := "/internal/v1/merchants/" + merchantID.String() + "/integrations/connections"

	// A retail-only member: no supply permission and no merchant.manage.
	retailSubject := "retail-only-" + uuid.NewString()[:8]
	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	if _, err := mSvc.AddMember(context.Background(), merchantID, retailSubject, []string{merchants.PermissionRetailStoresManage}); err != nil {
		t.Fatalf("failed to add retail-only member: %v", err)
	}

	storeID := createTestStore(t, db, merchantID)
	_ = createConnectionForTest(t, handler, merchantID, adminSubject, nil, integration.ProviderShopify, integration.ConnectionTypeRetailChannel, &storeID)
	extAcc := "ext-supply-scope"
	supplyConn := createConnectionForTest(t, handler, merchantID, adminSubject, &extAcc, integration.ProviderSalla, integration.ConnectionTypeSupplySource, nil)

	// Retail-only member: unfiltered list spans both scopes → 403.
	rec := doMerchantIntegrationRequest(t, handler, http.MethodGet, base, nil, retailSubject)
	if rec.Code != http.StatusForbidden {
		t.Errorf("unfiltered list without merchant.manage should be 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Retail-only member: retail-filtered list is allowed.
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, base+"?connection_type=RETAIL_CHANNEL", nil, retailSubject)
	if rec.Code != http.StatusOK {
		t.Errorf("retail-filtered list for retail-scoped member should be 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Retail-only member: supply-filtered list requires the supply scope → 403.
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, base+"?connection_type=SUPPLY_SOURCE", nil, retailSubject)
	if rec.Code != http.StatusForbidden {
		t.Errorf("supply-filtered list without supply scope should be 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Retail-only member: reading the supply connection requires the supply scope → 403.
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, base+"/"+supplyConn.ID.String(), nil, retailSubject)
	if rec.Code != http.StatusForbidden {
		t.Errorf("reading supply connection without supply scope should be 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Merchant admin (merchant.manage satisfies any scope): unfiltered list allowed.
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, base, nil, adminSubject)
	if rec.Code != http.StatusOK {
		t.Errorf("unfiltered list for merchant admin should be 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Invalid connection_type filter → 422.
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, base+"?connection_type=BOGUS", nil, adminSubject)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid connection_type filter should be 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// Cross-Merchant read is indistinguishable from missing → 404.
	otherMerchant := uuid.New()
	rec = doMerchantIntegrationRequest(t, handler, http.MethodGet, "/internal/v1/merchants/"+otherMerchant.String()+"/integrations/connections/"+supplyConn.ID.String(), nil, adminSubject)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-merchant read should be 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
