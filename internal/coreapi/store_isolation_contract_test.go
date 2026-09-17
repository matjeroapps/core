package coreapi

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

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/database"
)

const (
	subjectSellerA = "subject-seller-a-owner"
	subjectSellerB = "subject-seller-b-owner"
)

type isolationTestEnv struct {
	ctx        context.Context
	db         *database.Pool
	repo       commerce.Repository
	service    commerce.Service
	handler    http.Handler
	sellerAID  string
	sellerBID  string
	storeA1ID  string
	storeA2ID  string
	storeB1ID  string
	prodA1ID   string
	prodA2ID   string
	prodB1ID   string
	listA1ID   string
	listA2ID   string
	listB1ID   string
	assetA1ID  string
	assetA2ID  string
	assetB1ID  string
	refA1ID    string
	refA2ID    string
	refB1ID    string
	locA1ID    string
	locA2ID    string
	locB1ID    string
	skuA1ID    string
	skuA2ID    string
	skuB1ID    string
	snapA1ID   string
	snapA2ID   string
	snapB1ID   string
	supplierID string
	offerID    string
}

func setupIsolationContractEnv(t *testing.T) isolationTestEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)

	for _, name := range []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000007_theme_engine_schema",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000012_order_aggregate_schema",
		"000013_outbox_publish_claims",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000016_catalog_invariants",
		"000025_seller_catalog_phase_b",
		"000026_seller_catalog_phase_c",
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "migrations", name+".up.sql"))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	repo := commerce.NewRepository(db.Pool)
	service := commerce.NewService(repo)
	service.PlatformDomain = "matjero.test"
	storage := commerce.NewS3Storage(commerce.S3Config{URLTTL: 15 * time.Minute})
	storage.MockHeadObject = func(ctx context.Context, storageKey string) (*s3.HeadObjectOutput, error) {
		contentType := "image/webp"
		contentLength := int64(1024)
		return &s3.HeadObjectOutput{ContentType: &contentType, ContentLength: &contentLength}, nil
	}
	storage.MockPresignPutObject = func(ctx context.Context, storageKey, contentType string) (string, error) {
		return "https://s3.test/matjero-media/" + storageKey, nil
	}
	storage.MockGetObject = func(ctx context.Context, storageKey string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), 1024))), nil
	}
	service.S3Storage = storage

	themeService := themes.NewService(themes.NewRepository(db.Pool), repo, themes.Options{
		PreviewSecret: []byte("integration-preview-secret"),
	})
	if err := themeService.SeedBuiltInThemes(ctx); err != nil {
		t.Fatalf("seed built-in themes: %v", err)
	}
	resolver := storefront.NewStoreResolver(repo)
	deps := Dependencies{
		Commerce:  service,
		Repo:      repo,
		Markets:   markets.NewService(markets.NewRepository(db.Pool)),
		Catalog:   storefront.NewCatalogRepository(db.Pool),
		Stores:    resolver,
		Revisions: storefront.NewRevisionReader(resolver, repo),
		Themes:    themeService,
	}

	// 1. Create Seller A, Store A1, Store A2
	sellerA, err := repo.CreateSeller(ctx, "seller-a", "Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	if _, err := repo.CreateSellerMember(ctx, sellerA.ID, subjectSellerA, "owner", "active"); err != nil {
		t.Fatalf("create seller A member: %v", err)
	}
	storeA1, _, err := repo.CreateStoreWithDomain(ctx, sellerA.ID, "EG", "store-a1", "Store A1", "active", nil, "store-a1.matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store A1: %v", err)
	}
	storeA2, _, err := repo.CreateStoreWithDomain(ctx, sellerA.ID, "EG", "store-a2", "Store A2", "draft", nil, "store-a2.matjero.test", "platform", "active", false, nil, nil)
	if err != nil {
		t.Fatalf("create store A2: %v", err)
	}

	// 2. Create Seller B, Store B1
	sellerB, err := repo.CreateSeller(ctx, "seller-b", "Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	if _, err := repo.CreateSellerMember(ctx, sellerB.ID, subjectSellerB, "owner", "active"); err != nil {
		t.Fatalf("create seller B member: %v", err)
	}
	storeB1, _, err := repo.CreateStoreWithDomain(ctx, sellerB.ID, "EG", "store-b1", "Store B1", "active", nil, "store-b1.matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store B1: %v", err)
	}

	// 3. Helper to populate a store's product, listing, media asset, media ref, location, SKU, snapshot
	populateStoreData := func(subj, storeID, prefix string) (prodID, listID, assetID, refID, locID, skuID, snapID string) {
		detail, err := service.CreateSellerProductForSubject(ctx, subj, storeID, commerce.SellerProductDraft{
			Slug: prefix + "-product",
			Translations: []commerce.ProductTranslation{
				{Locale: "en", Name: prefix + " Product"},
			},
		})
		if err != nil {
			t.Fatalf("create product for %s: %v", prefix, err)
		}
		prodID = detail.Product.ID
		listID = detail.Listing.ID

		v, err := service.CreateVariantForSubject(ctx, subj, storeID, prodID, "default", "active")
		if err != nil {
			t.Fatalf("create variant for %s: %v", prefix, err)
		}
		sku, err := service.CreateSKUForSubject(ctx, subj, storeID, prodID, v.ID, "SKU-"+prefix, "BAR-"+prefix, "active")
		if err != nil {
			t.Fatalf("create sku for %s: %v", prefix, err)
		}
		skuID = sku.ID

		intentID := "intent-" + prefix
		intent, err := repo.CreateMediaUploadIntent(ctx, commerce.MediaUploadIntent{
			StoreID:          storeID,
			ClientUploadID:   &intentID,
			OriginalFilename: prefix + ".webp",
			ContentType:      "image/webp",
			ByteSize:         1024,
			ChecksumSHA256:   fmt.Sprintf("%064d", time.Now().UnixNano()),
			StorageKey:       fmt.Sprintf("stores/%s/media/%s.webp", storeID, prefix),
			TokenDigest:      "dummy",
			ExpiresAt:        time.Now().Add(1 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create media upload intent for %s: %v", prefix, err)
		}

		asset, err := repo.CompleteMediaUploadAndCreateAsset(ctx, intent.ID, commerce.StoreMediaAsset{
			StoreID:          storeID,
			ChecksumSHA256:   intent.ChecksumSHA256,
			StorageKey:       intent.StorageKey,
			ContentType:      "image/webp",
			ByteSize:         1024,
			OriginalFilename: prefix + ".webp",
			CreatedBySubject: subj,
		})
		if err != nil {
			t.Fatalf("create asset for %s: %v", prefix, err)
		}
		assetID = asset.ID

		ref, err := service.AttachProductMediaReference(ctx, subj, storeID, prodID, commerce.AttachMediaReferenceRequest{
			AssetID:   assetID,
			AltText:   prefix + " image",
			SortOrder: 1,
			IsPrimary: true,
		})
		if err != nil {
			t.Fatalf("attach media ref for %s: %v", prefix, err)
		}
		refID = ref.ID

		loc, err := repo.CreateStoreFulfillmentLocation(ctx, storeID, "EG", "LOC-"+prefix, prefix+" Warehouse", "warehouse", "active")
		if err != nil {
			t.Fatalf("create location for %s: %v", prefix, err)
		}
		locID = loc.ID

		snap, err := service.CreateInventorySnapshotForSubject(ctx, subj, storeID, locID, skuID, 50)
		if err != nil {
			t.Fatalf("create snapshot for %s: %v", prefix, err)
		}
		snapID = snap.ID

		return
	}

	pA1, lA1, aA1, rA1, locA1, sA1, snA1 := populateStoreData(subjectSellerA, storeA1.ID, "A1")
	pA2, lA2, aA2, rA2, locA2, sA2, snA2 := populateStoreData(subjectSellerA, storeA2.ID, "A2")
	pB1, lB1, aB1, rB1, locB1, sB1, snB1 := populateStoreData(subjectSellerB, storeB1.ID, "B1")

	// 4. Supplier and Offer
	supplier, err := repo.CreateSupplier(ctx, "supplier-test", "Test Supplier", "active", nil)
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	supMarket, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market: %v", err)
	}
	_, sp, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, commerce.ProductDraft{
		Slug:         "supplier-prod",
		Status:       "active",
		SupplierCode: "SUPP-PROD-1",
		Translations: []commerce.ProductTranslation{{Locale: "en", Name: "Supplier Prod"}},
	})
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	offer, err := repo.CreateSupplierOfferAtomically(ctx, supplier.ID, commerce.OfferDraft{
		SupplierProductID: sp.ID,
		SupplierMarketID:  supMarket.ID,
		MarketCode:        "EG",
		Status:            "active",
	})
	if err != nil {
		t.Fatalf("create supplier offer: %v", err)
	}

	return isolationTestEnv{
		ctx:        ctx,
		db:         db,
		repo:       repo,
		service:    service,
		handler:    serviceauth.Middleware(testAuthConfig())(NewRouter(deps)),
		sellerAID:  sellerA.ID,
		sellerBID:  sellerB.ID,
		storeA1ID:  storeA1.ID,
		storeA2ID:  storeA2.ID,
		storeB1ID:  storeB1.ID,
		prodA1ID:   pA1,
		prodA2ID:   pA2,
		prodB1ID:   pB1,
		listA1ID:   lA1,
		listA2ID:   lA2,
		listB1ID:   lB1,
		assetA1ID:  aA1,
		assetA2ID:  aA2,
		assetB1ID:  aB1,
		refA1ID:    rA1,
		refA2ID:    rA2,
		refB1ID:    rB1,
		locA1ID:    locA1,
		locA2ID:    locA2,
		locB1ID:    locB1,
		skuA1ID:    sA1,
		skuA2ID:    sA2,
		skuB1ID:    sB1,
		snapA1ID:   snA1,
		snapA2ID:   snA2,
		snapB1ID:   snB1,
		supplierID: supplier.ID,
		offerID:    offer.ID,
	}
}

func (env isolationTestEnv) doRequest(subj, method, path string, body any) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(serviceauth.HeaderService, "seller")
	req.Header.Set("Authorization", "Bearer "+testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, subj)
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	return rec
}

func TestStoreCatalogIsolationContract(t *testing.T) {
	env := setupIsolationContractEnv(t)
	randomUUID := uuid.NewString()

	// --- 1. Correct Scope (A1 resources under A1 store path by Seller A owner) ---
	t.Run("Correct A1 Scope Succeeded", func(t *testing.T) {
		rec := env.doRequest(subjectSellerA, http.MethodGet, "/internal/v1/stores/"+env.storeA1ID+"/products/"+env.prodA1ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get A1 product under A1 path = %d, want 200", rec.Code)
		}

		recList := env.doRequest(subjectSellerA, http.MethodGet, "/internal/v1/stores/"+env.storeA1ID+"/listings/"+env.listA1ID, nil)
		if recList.Code != http.StatusOK {
			t.Fatalf("get A1 listing under A1 path = %d, want 200", recList.Code)
		}

		recMedia := env.doRequest(subjectSellerA, http.MethodGet, "/internal/v1/stores/"+env.storeA1ID+"/media", nil)
		if recMedia.Code != http.StatusOK {
			t.Fatalf("get A1 media under A1 path = %d, want 200", recMedia.Code)
		}

		recInv := env.doRequest(subjectSellerA, http.MethodGet, "/internal/v1/stores/"+env.storeA1ID+"/inventory", nil)
		if recInv.Code != http.StatusOK {
			t.Fatalf("get A1 inventory under A1 path = %d, want 200", recInv.Code)
		}
	})

	// --- 2. Same Seller Cross-Store Isolation (A2 resource under A1 store path by Seller A owner) ---
	t.Run("Same Seller Cross-Store Resource Under A1 Path Returns 404", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			body   any
		}{
			// Products / Catalog
			{"GET A2 Product under A1", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s", env.storeA1ID, env.prodA2ID), nil},
			{"PUT A2 Product under A1", http.MethodPut, fmt.Sprintf("/internal/v1/stores/%s/products/%s", env.storeA1ID, env.prodA2ID), StoreProductUpdateRequest{Slug: "hacked"}},
			{"Create Variant on A2 Product under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/variants", env.storeA1ID, env.prodA2ID), VariantCreateRequest{Code: "v2"}},
			{"Transition Status on A2 Product under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/status", env.storeA1ID, env.prodA2ID), ProductStatusUpdateRequest{Status: "active"}},
			{"Archive A2 Product under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/archive", env.storeA1ID, env.prodA2ID), nil},
			{"Publish A2 Product under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/publish", env.storeA1ID, env.prodA2ID), nil},
			{"Unpublish A2 Product under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/unpublish", env.storeA1ID, env.prodA2ID), nil},

			// Listings
			{"GET A2 Listing under A1", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings/%s", env.storeA1ID, env.listA2ID), nil},
			{"PUT A2 Listing Price under A1", http.MethodPut, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/price", env.storeA1ID, env.listA2ID), PriceUpdateRequest{AmountMinor: 1000, Currency: "EGP"}},
			{"GET A2 Listing Readiness under A1", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/readiness", env.storeA1ID, env.listA2ID), nil},
			{"Publish A2 Listing under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/publish", env.storeA1ID, env.listA2ID), nil},
			{"Unpublish A2 Listing under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/unpublish", env.storeA1ID, env.listA2ID), nil},
			{"Archive A2 Listing under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/archive", env.storeA1ID, env.listA2ID), nil},
			{"GET A2 Listing Presentation under A1", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/presentation", env.storeA1ID, env.listA2ID), nil},
			{"PUT A2 Listing Presentation under A1", http.MethodPut, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/presentation", env.storeA1ID, env.listA2ID), commerce.SellerListingPresentation{PurchaseBehavior: "add_to_cart"}},

			// Media Library & References
			{"DELETE A2 Asset under A1", http.MethodDelete, fmt.Sprintf("/internal/v1/stores/%s/media/%s", env.storeA1ID, env.assetA2ID), nil},
			{"GET A2 Product Media Refs under A1", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media-references", env.storeA1ID, env.prodA2ID), nil},
			{"Attach A2 Asset to A1 Product", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media-references", env.storeA1ID, env.prodA1ID), commerce.AttachMediaReferenceRequest{AssetID: env.assetA2ID}},
			{"Update A2 Media Ref under A1", http.MethodPut, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media-references/%s", env.storeA1ID, env.prodA1ID, env.refA2ID), commerce.UpdateMediaReferenceRequest{AltText: "new"}},
			{"Detach A2 Media Ref under A1", http.MethodDelete, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media-references/%s", env.storeA1ID, env.prodA1ID, env.refA2ID), nil},

			// Inventory Operations
			{"Create Snapshot for A2 Location under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/inventory/snapshots", env.storeA1ID), StoreInventorySnapshotCreateRequest{LocationID: env.locA2ID, SKUID: env.skuA1ID, OnHandQty: 10}},
			{"Create Snapshot for A2 SKU under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/inventory/snapshots", env.storeA1ID), StoreInventorySnapshotCreateRequest{LocationID: env.locA1ID, SKUID: env.skuA2ID, OnHandQty: 10}},
			{"Adjust A2 Snapshot under A1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/inventory/%s/adjustments", env.storeA1ID, env.snapA2ID), StoreInventoryAdjustmentRequest{QuantityDelta: 5, Reason: "test"}},
		}

		for _, tc := range tests {
			rec := env.doRequest(subjectSellerA, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (body: %s)", tc.name, rec.Code, rec.Body.String())
			}
		}
	})

	// --- 3. Different Seller Isolation (Seller A requesting Store B1 path or B1 resources) ---
	t.Run("Cross-Seller Store B1 Access by Seller A Returns 404", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
		}{
			{"Seller A list B1 Products", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products", env.storeB1ID)},
			{"Seller A get B1 Product", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s", env.storeB1ID, env.prodB1ID)},
			{"Seller A list B1 Listings", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings", env.storeB1ID)},
			{"Seller A list B1 Media", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/media", env.storeB1ID)},
			{"Seller A list B1 Inventory", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/inventory", env.storeB1ID)},
			{"Seller A list B1 Supplier Offers", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/supplier-offers", env.storeB1ID)},
			{"Seller A import Offer into B1", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/supplier-offers/%s/imports", env.storeB1ID, env.offerID)},
		}

		for _, tc := range tests {
			rec := env.doRequest(subjectSellerA, tc.method, tc.path, nil)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (body: %s)", tc.name, rec.Code, rec.Body.String())
			}
		}
	})

	// --- 4. Reverse Seller B requesting Store A1 path ---
	t.Run("Cross-Seller Store A1 Access by Seller B Returns 404", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
		}{
			{"Seller B list A1 Products", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products", env.storeA1ID)},
			{"Seller B get A1 Product", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s", env.storeA1ID, env.prodA1ID)},
			{"Seller B list A1 Listings", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings", env.storeA1ID)},
			{"Seller B list A1 Media", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/media", env.storeA1ID)},
			{"Seller B list A1 Inventory", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/inventory", env.storeA1ID)},
		}

		for _, tc := range tests {
			rec := env.doRequest(subjectSellerB, tc.method, tc.path, nil)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (body: %s)", tc.name, rec.Code, rec.Body.String())
			}
		}
	})

	// --- 5. Random UUID non-existent resources under A1 path (Proves no ID existence oracle) ---
	t.Run("Non-existent Random Resource Under A1 Path Returns 404", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			path   string
			body   any
		}{
			{"Random Product ID", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s", env.storeA1ID, randomUUID), nil},
			{"Random Listing ID", http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/listings/%s", env.storeA1ID, randomUUID), nil},
			{"Random Asset ID", http.MethodDelete, fmt.Sprintf("/internal/v1/stores/%s/media/%s", env.storeA1ID, randomUUID), nil},
			{"Random Snapshot ID", http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/inventory/%s/adjustments", env.storeA1ID, randomUUID), StoreInventoryAdjustmentRequest{QuantityDelta: 5, Reason: "test"}},
		}

		for _, tc := range tests {
			rec := env.doRequest(subjectSellerA, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (body: %s)", tc.name, rec.Code, rec.Body.String())
			}
		}
	})
}
