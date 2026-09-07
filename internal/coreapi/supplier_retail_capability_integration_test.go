package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
)

func setupSupplierRetailAPI(t *testing.T) (context.Context, commerce.Repository, commerce.Service, http.Handler) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)

	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000001_event_delivery_foundation.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000002_market_reference_data.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000003_commerce_domain_schema.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000004_admin_supplier_seller_platforms.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000005_store_domain_lifecycle.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000006_store_domain_integrity.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000007_theme_engine_schema.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000008_storefront_revisions.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000009_supplier_retail_capability.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000010_customer_cart_domain.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000011_checkout_sessions.up.sql"))
	applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", "000018_supplier_retail_affiliation.up.sql"))

	repo := commerce.NewRepository(db.Pool)
	svc := commerce.NewService(repo)
	svc.PlatformDomain = "matjero.com"

	router := NewRouter(Dependencies{
		Commerce: svc,
		Repo:     repo,
	})

	cfg := serviceauth.Config{
		Tokens: map[serviceauth.Caller]string{
			serviceauth.CallerSupplier: testSupplierToken,
			serviceauth.CallerSeller:   testSellerToken,
			serviceauth.CallerAdmin:    testAdminToken,
		},
	}

	handler := serviceauth.Middleware(cfg)(router)

	return context.Background(), repo, svc, handler
}

func TestSupplierRetailAPI_SecurityAndCapabilities(t *testing.T) {
	ctx, repo, _, handler := setupSupplierRetailAPI(t)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	subjectA := "sub-api-supplier-a-" + suffix
	subjectB := "sub-api-supplier-b-" + suffix

	// Create Supplier A & Member
	supA, err := repo.CreateSupplier(ctx, "sup-api-a-"+suffix, "Supplier API A", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier A: %v", err)
	}
	if _, err := repo.CreateSupplierMember(ctx, supA.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("CreateSupplierMember A: %v", err)
	}

	// Create Supplier B & Member
	supB, err := repo.CreateSupplier(ctx, "sup-api-b-"+suffix, "Supplier API B", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier B: %v", err)
	}
	if _, err := repo.CreateSupplierMember(ctx, supB.ID, subjectB, "owner", "active"); err != nil {
		t.Fatalf("CreateSupplierMember B: %v", err)
	}

	// 1. Missing service auth -> 401
	req1 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing service auth, got %d", rec1.Code)
	}

	// 2. Seller caller attempting Supplier Retail endpoint -> 403 Forbidden
	req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), nil)
	req2.Header.Set("Authorization", "Bearer "+testSellerToken)
	req2.Header.Set("X-Matjero-Service", "seller")
	req2.Header.Set("X-Matjero-Subject", subjectA)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for CallerSeller on supplier endpoint, got %d", rec2.Code)
	}

	// 2b. Admin caller attempting Supplier Retail endpoint -> 403 Forbidden (Supplier-service only self-service capability)
	req2b := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), nil)
	req2b.Header.Set("Authorization", "Bearer "+testAdminToken)
	req2b.Header.Set("X-Matjero-Service", "admin")
	req2b.Header.Set("X-Matjero-Subject", subjectA)
	rec2b := httptest.NewRecorder()
	handler.ServeHTTP(rec2b, req2b)
	if rec2b.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for CallerAdmin on supplier retail endpoint, got %d", rec2b.Code)
	}

	// 3. Supplier Manager (non-owner) attempting to provision Retail Capability -> Authorization Failure (404/Domain Error)
	subjectMgr := "sub-api-supplier-mgr-" + suffix
	if _, err := repo.CreateSupplierMember(ctx, supA.ID, subjectMgr, "manager", "active"); err != nil {
		t.Fatalf("CreateSupplierMember Manager: %v", err)
	}
	mgrBody, _ := json.Marshal(SupplierRetailCapabilityRequest{
		Code: "sel-mgr-attempt-" + suffix,
		Name: "Manager Attempt Profile",
	})
	reqMgr := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), bytes.NewReader(mgrBody))
	reqMgr.Header.Set("Authorization", "Bearer "+testSupplierToken)
	reqMgr.Header.Set("X-Matjero-Service", "supplier")
	reqMgr.Header.Set("X-Matjero-Subject", subjectMgr)
	reqMgr.Header.Set("Content-Type", "application/json")
	recMgr := httptest.NewRecorder()
	handler.ServeHTTP(recMgr, reqMgr)
	if recMgr.Code == http.StatusCreated || recMgr.Code == http.StatusOK {
		t.Fatalf("expected authorization failure for Manager subject provisioning retail capability, got %d", recMgr.Code)
	}

	// 3b. Supplier A attempting to act on Supplier B -> 404 Not Found (safe isolation)
	req3 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supB.ID), nil)
	req3.Header.Set("Authorization", "Bearer "+testSupplierToken)
	req3.Header.Set("X-Matjero-Service", "supplier")
	req3.Header.Set("X-Matjero-Subject", subjectA)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Supplier A acting on Supplier B id, got %d", rec3.Code)
	}

	// 4. Provision Supplier Retail Capability via Supplier A -> 201 Created
	capReqBody, _ := json.Marshal(SupplierRetailCapabilityRequest{
		Code:     "sel-api-a-" + suffix,
		Name:     "Seller API Profile A",
		Settings: map[string]any{"market": "EG"},
	})
	req4 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), bytes.NewReader(capReqBody))
	req4.Header.Set("Authorization", "Bearer "+testSupplierToken)
	req4.Header.Set("X-Matjero-Service", "supplier")
	req4.Header.Set("X-Matjero-Subject", subjectA)
	req4.Header.Set("Content-Type", "application/json")
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)

	if rec4.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for provisioning retail capability, got %d: %s", rec4.Code, rec4.Body.String())
	}

	var capResp SupplierRetailCapabilityResponse
	if err := json.Unmarshal(rec4.Body.Bytes(), &capResp); err != nil {
		t.Fatalf("unmarshal capability response: %v", err)
	}
	if capResp.Affiliation.SupplierID != supA.ID || capResp.Seller.Code != "sel-api-a-"+suffix {
		t.Fatalf("unexpected capability response payload: %+v", capResp)
	}

	// 5. GET Supplier Retail Capability -> 200 OK
	req5 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/suppliers/%s/retail-capability", supA.ID), nil)
	req5.Header.Set("Authorization", "Bearer "+testSupplierToken)
	req5.Header.Set("X-Matjero-Service", "supplier")
	req5.Header.Set("X-Matjero-Subject", subjectA)
	rec5 := httptest.NewRecorder()
	handler.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET retail capability, got %d", rec5.Code)
	}

	// 6. POST Supplier Store -> 201 Created
	storeReqBody, _ := json.Marshal(StoreCreateRequest{
		MarketCode: "EG",
		Code:       "supstore" + suffix,
		Name:       "Supplier Retail Store A",
		Status:     "active",
	})
	req6 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/internal/v1/suppliers/%s/stores", supA.ID), bytes.NewReader(storeReqBody))
	req6.Header.Set("Authorization", "Bearer "+testSupplierToken)
	req6.Header.Set("X-Matjero-Service", "supplier")
	req6.Header.Set("X-Matjero-Subject", subjectA)
	req6.Header.Set("Content-Type", "application/json")
	rec6 := httptest.NewRecorder()
	handler.ServeHTTP(rec6, req6)

	if rec6.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for POST supplier store, got %d: %s", rec6.Code, rec6.Body.String())
	}

	var createdStore commerce.Store
	if err := json.Unmarshal(rec6.Body.Bytes(), &createdStore); err != nil {
		t.Fatalf("unmarshal store response: %v", err)
	}
	if createdStore.SellerID != capResp.Seller.ID {
		t.Fatalf("expected store seller_id to be %s, got %s", capResp.Seller.ID, createdStore.SellerID)
	}

	// 7. GET Supplier Stores -> 200 OK
	req7 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/suppliers/%s/stores", supA.ID), nil)
	req7.Header.Set("Authorization", "Bearer "+testSupplierToken)
	req7.Header.Set("X-Matjero-Service", "supplier")
	req7.Header.Set("X-Matjero-Subject", subjectA)
	rec7 := httptest.NewRecorder()
	handler.ServeHTTP(rec7, req7)

	if rec7.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for GET supplier stores, got %d", rec7.Code)
	}

	var storesColl CollectionResponse[commerce.Store]
	if err := json.Unmarshal(rec7.Body.Bytes(), &storesColl); err != nil {
		t.Fatalf("unmarshal stores collection: %v", err)
	}
	if len(storesColl.Items) != 1 || storesColl.Items[0].ID != createdStore.ID {
		t.Fatalf("unexpected stores collection items: %+v", storesColl.Items)
	}
}
