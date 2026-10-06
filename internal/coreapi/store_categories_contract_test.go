package coreapi

// Store-scoped category contract tests. These pin the exact JSON shapes the
// Seller BFF's coreclient must decode, and the isolation behaviour of the
// store-scoped category endpoints.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"core/internal/serviceauth"
	"core/internal/testdb"
	"core/modules/commerce"
	"core/modules/markets"
	"core/modules/storefront"
	"core/packages/database"
)

type storeCategoryContractEnv struct {
	ctx          context.Context
	db           *database.Pool
	repo         commerce.Repository
	handler      http.Handler
	storeAID     string
	storeBID     string
	subjectA     string
	subjectB     string
}

func setupStoreCategoryContract(t *testing.T) storeCategoryContractEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)

	migrationNames := []string{
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000025_seller_catalog_phase_b",
		"000026_seller_catalog_phase_c",
		"000044_store_scoped_categories",
	}
	migrationPaths := make([]string, 0, len(migrationNames))
	for _, name := range migrationNames {
		migrationPaths = append(migrationPaths, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrationPaths...)

	repo := commerce.NewRepository(db.Pool)
	service := commerce.NewService(repo)
	resolver := storefront.NewStoreResolver(repo)
	deps := Dependencies{
		Commerce:  service,
		Repo:      repo,
		Markets:   markets.NewService(markets.NewRepository(db.Pool)),
		Catalog:   storefront.NewCatalogRepository(db.Pool),
		Stores:    resolver,
		Revisions: storefront.NewRevisionReader(resolver, repo),
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	sellerA, err := repo.CreateSeller(ctx, "cat-contract-a-"+suffix, "Contract Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	subjectA := "cat-contract-subject-a-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerA.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("create member A: %v", err)
	}
	storeA, _, err := repo.CreateStoreWithDomain(ctx, sellerA.ID, "EG", "cat-a-"+suffix, "Category Store A", "active", nil, "cat-a-"+suffix+".matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}

	sellerB, err := repo.CreateSeller(ctx, "cat-contract-b-"+suffix, "Contract Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	subjectB := "cat-contract-subject-b-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerB.ID, subjectB, "owner", "active"); err != nil {
		t.Fatalf("create member B: %v", err)
	}
	storeB, _, err := repo.CreateStoreWithDomain(ctx, sellerB.ID, "EG", "cat-b-"+suffix, "Category Store B", "active", nil, "cat-b-"+suffix+".matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}

	return storeCategoryContractEnv{
		ctx:      ctx,
		db:       db,
		repo:     repo,
		handler:  serviceauth.Middleware(testAuthConfig())(NewRouter(deps)),
		storeAID: storeA.ID,
		storeBID: storeB.ID,
		subjectA: subjectA,
		subjectB: subjectB,
	}
}

func (e storeCategoryContractEnv) do(t *testing.T, caller serviceauth.Caller, token, subject, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(serviceauth.HeaderService, string(caller))
	req.Header.Set("Authorization", "Bearer "+token)
	if subject != "" {
		req.Header.Set(serviceauth.HeaderSubject, subject)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e storeCategoryContractEnv) requireStatus(t *testing.T, rec *httptest.ResponseRecorder, wantCode int) map[string]any {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, wantCode, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
	}
	return payload
}

func TestStoreCategoryContractLifecycle(t *testing.T) {
	env := setupStoreCategoryContract(t)
	base := "/internal/v1/stores/" + env.storeAID + "/categories"

	// Create returns 201 with the full node shape.
	createRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodPost, base, map[string]any{
		"slug": "winter-jackets",
		"translations": map[string]any{
			"en": map[string]any{"name": "Winter Jackets", "description": "Cold weather"},
			"ar": map[string]any{"name": "جاكيتات الشتاء"},
		},
	})
	createBody := env.requireStatus(t, createRec, http.StatusCreated)
	jsonKeys(t, "create", createBody, "id", "store_id", "slug", "status", "sort_order", "translations", "product_count", "child_count", "created_at", "updated_at")
	categoryID, _ := createBody["id"].(string)
	if categoryID == "" {
		t.Fatalf("create response missing id: %v", createBody)
	}
	translations, _ := createBody["translations"].(map[string]any)
	if _, ok := translations["en"]; !ok {
		t.Fatalf("missing en translation in %v", translations)
	}

	// List is a CollectionResponse envelope.
	listRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodGet, base, nil)
	listBody := env.requireStatus(t, listRec, http.StatusOK)
	jsonKeys(t, "list", listBody, "items", "total", "limit", "offset")
	items, _ := listBody["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list items = %d, want 1", len(items))
	}

	// Get by id.
	getRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodGet, base+"/"+categoryID, nil)
	getBody := env.requireStatus(t, getRec, http.StatusOK)
	if getBody["id"] != categoryID {
		t.Fatalf("get id = %v, want %s", getBody["id"], categoryID)
	}

	// Update.
	updateRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodPut, base+"/"+categoryID, map[string]any{
		"slug": "winter-outerwear",
		"translations": map[string]any{
			"en": map[string]any{"name": "Winter Outerwear"},
		},
	})
	updateBody := env.requireStatus(t, updateRec, http.StatusOK)
	if updateBody["slug"] != "winter-outerwear" {
		t.Fatalf("update slug = %v", updateBody["slug"])
	}

	// Duplicate slug conflicts.
	dupRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodPost, base, map[string]any{
		"slug":         "winter-outerwear",
		"translations": map[string]any{"en": map[string]any{"name": "Dup"}},
	})
	dupBody := env.requireStatus(t, dupRec, http.StatusConflict)
	if dupBody["error"] == nil {
		t.Fatalf("conflict response missing error envelope: %v", dupBody)
	}

	// Validation error for a bad slug.
	badRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodPost, base, map[string]any{
		"slug":         "Bad Slug!",
		"translations": map[string]any{"en": map[string]any{"name": "X"}},
	})
	env.requireStatus(t, badRec, http.StatusBadRequest)

	// The store-capabilities group admits seller and admin callers; an admin
	// call still carries the end-user subject and passes the same membership
	// checks. A call without any subject is unauthorized.
	adminRec := env.do(t, serviceauth.CallerAdmin, testAdminToken, env.subjectA, http.MethodGet, base, nil)
	env.requireStatus(t, adminRec, http.StatusOK)
	noSubjectRec := env.do(t, serviceauth.CallerSeller, testSellerToken, "", http.MethodGet, base, nil)
	env.requireStatus(t, noSubjectRec, http.StatusUnauthorized)
}

func TestStoreCategoryContractIsolation(t *testing.T) {
	env := setupStoreCategoryContract(t)

	createRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodPost, "/internal/v1/stores/"+env.storeAID+"/categories", map[string]any{
		"slug":         "store-a-secret",
		"translations": map[string]any{"en": map[string]any{"name": "Store A Secret"}},
	})
	createBody := env.requireStatus(t, createRec, http.StatusCreated)
	categoryID, _ := createBody["id"].(string)

	foreignBody := func(rec *httptest.ResponseRecorder) map[string]any {
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode body: %v (body %q)", err, rec.Body.String())
		}
		return payload
	}

	// Seller B listing store A: 404.
	foreignList := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectB, http.MethodGet, "/internal/v1/stores/"+env.storeAID+"/categories", nil)
	if foreignList.Code != http.StatusNotFound {
		t.Fatalf("foreign list status = %d, want 404", foreignList.Code)
	}

	// Seller B reading store A's category: 404, byte-identical to an unknown id.
	foreignGet := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectB, http.MethodGet, "/internal/v1/stores/"+env.storeAID+"/categories/"+categoryID, nil)
	unknownGet := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodGet, "/internal/v1/stores/"+env.storeAID+"/categories/00000000-0000-0000-0000-000000000000", nil)
	if foreignGet.Code != http.StatusNotFound || unknownGet.Code != http.StatusNotFound {
		t.Fatalf("foreign get = %d, unknown get = %d, want both 404", foreignGet.Code, unknownGet.Code)
	}
	if string(foreignGet.Body.Bytes()) != string(unknownGet.Body.Bytes()) {
		t.Fatalf("foreign and unknown 404 bodies differ: %q vs %q", foreignGet.Body.String(), unknownGet.Body.String())
	}
	if payload := foreignBody(foreignGet); payload["error"] == nil {
		t.Fatalf("404 missing error envelope: %v", payload)
	}

	// Seller B cannot create a category in store A, and cross-store parents 404.
	foreignCreate := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectB, http.MethodPost, "/internal/v1/stores/"+env.storeAID+"/categories", map[string]any{
		"slug":         "hijack",
		"translations": map[string]any{"en": map[string]any{"name": "Hijack"}},
	})
	if foreignCreate.Code != http.StatusNotFound {
		t.Fatalf("foreign create status = %d, want 404", foreignCreate.Code)
	}
	crossParent := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectB, http.MethodPost, "/internal/v1/stores/"+env.storeBID+"/categories", map[string]any{
		"slug":                "cross-parent",
		"parent_category_id":  categoryID,
		"translations":        map[string]any{"en": map[string]any{"name": "Cross"}},
	})
	if crossParent.Code != http.StatusNotFound {
		t.Fatalf("cross-store parent status = %d, want 404 (body %q)", crossParent.Code, crossParent.Body.String())
	}

	// The hidden category is unchanged after every rejected attempt.
	verifyRec := env.do(t, serviceauth.CallerSeller, testSellerToken, env.subjectA, http.MethodGet, "/internal/v1/stores/"+env.storeAID+"/categories/"+categoryID, nil)
	verifyBody := env.requireStatus(t, verifyRec, http.StatusOK)
	if verifyBody["slug"] != "store-a-secret" {
		t.Fatalf("category mutated by foreign attempts: %v", verifyBody["slug"])
	}
}
