package coreapi_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestMerchantIntegrationAPIEndpointsAndAuthorization(t *testing.T) {
	_, handler, merchantID, subject := setupMerchantIntegrationAPITest(t)

	// 1. Create Connection (Retail Channel)
	createReq := coreapi.CreateMerchantConnectionHTTPRequest{
		ConnectionType: integration.ConnectionTypeRetailChannel,
		Provider:       integration.ProviderShopify,
		Name:           "Shopify Channel 1",
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
