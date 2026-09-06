package coreapi

// P5.8 cross-repository contract tests. These exercise the real HTTP handlers
// of the internal Seller API and pin the exact JSON shapes the Seller app's
// coreclient must decode. The fixture bodies produced here are the source of
// truth for the Seller repository's contract fixtures.

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
	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/money"
)

const p58Subject = "subject-of-seller-a"

type p58ContractEnv struct {
	ctx     context.Context
	db      *database.Pool
	repo    commerce.Repository
	service commerce.Service
	handler http.Handler
	storeID string
}

func setupP58Contract(t *testing.T) p58ContractEnv {
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

	seller, err := repo.CreateSeller(ctx, "contract-seller", "Contract Seller", "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	if _, err := repo.CreateSellerMember(ctx, seller.ID, p58Subject, "owner", "active"); err != nil {
		t.Fatalf("create seller member: %v", err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "contract-store", "Contract Store", "active", nil, "contract-store.matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	return p58ContractEnv{
		ctx:     ctx,
		db:      db,
		repo:    repo,
		service: service,
		handler: serviceauth.Middleware(testAuthConfig())(NewRouter(deps)),
		storeID: store.ID,
	}
}

func (e p58ContractEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
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
	req.Header.Set(serviceauth.HeaderService, "seller")
	req.Header.Set("Authorization", "Bearer "+testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, p58Subject)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e p58ContractEnv) requireOK(t *testing.T, rec *httptest.ResponseRecorder, wantCode int) map[string]any {
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

func jsonKeys(t *testing.T, path string, payload map[string]any, want ...string) {
	t.Helper()
	for _, key := range want {
		if _, ok := payload[key]; !ok {
			t.Fatalf("%s: missing key %q in %v", path, key, payload)
		}
	}
}

// TestP58SellerContractShapes drives the real handlers and pins the exact
// Seller API contract shapes. When P58_FIXTURE_DIR is set, the raw response
// bodies are written there as cross-repository contract fixtures.
func TestP58SellerContractShapes(t *testing.T) {
	e := setupP58Contract(t)
	fixtureDir := os.Getenv("P58_FIXTURE_DIR")
	writeFixture := func(name string, body []byte) {
		if fixtureDir == "" {
			return
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, name), body, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	// --- Product create: detail shape ---
	createRec := e.do(t, http.MethodPost, "/internal/v1/stores/"+e.storeID+"/products", commerce.SellerProductDraft{
		Slug: "contract-product",
		Translations: []commerce.ProductTranslation{
			{Locale: "en", Name: "Contract Product", Description: "English description"},
			{Locale: "ar", Name: "منتج العقد", Description: "وصف عربي"},
		},
	})
	createBody := createRec.Body.Bytes()
	detail := e.requireOK(t, createRec, http.StatusCreated)
	writeFixture("product-detail.json", createBody)
	jsonKeys(t, "product detail", detail,
		"product", "source", "translations", "category_ids", "variants", "skus",
		"media", "listing", "current_price", "inventory_summary", "presentation",
		"purchase_behavior", "publish_readiness")
	if detail["source"] != "seller_owned" {
		t.Fatalf("source = %v", detail["source"])
	}
	if _, ok := detail["categories"]; ok {
		t.Fatalf("product detail must not expose raw categories; use category_ids")
	}
	invSummary := detail["inventory_summary"].(map[string]any)
	jsonKeys(t, "inventory_summary", invSummary, "total_on_hand", "total_reserved", "total_available", "locations")

	productID := detail["product"].(map[string]any)["id"].(string)
	listingID := detail["listing"].(map[string]any)["id"].(string)

	// --- Variant + SKU ---
	vRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/variants", e.storeID, productID),
		VariantCreateRequest{Code: "default", Status: "active"})
	variant := e.requireOK(t, vRec, http.StatusCreated)
	variantID := variant["id"].(string)

	sRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/variants/%s/skus", e.storeID, productID, variantID),
		SKUCreateRequest{Code: "SKU-CONTRACT-1", Status: "active"})
	sku := e.requireOK(t, sRec, http.StatusCreated)
	skuID := sku["id"].(string)

	// --- Media presign + complete ---
	presignRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media/uploads", e.storeID, productID),
		commerce.MediaUploadRequest{Filename: "front.webp", ContentType: "image/webp", SizeBytes: 1024})
	presignBody := presignRec.Body.Bytes()
	presign := e.requireOK(t, presignRec, http.StatusOK)
	writeFixture("media-presign.json", presignBody)
	jsonKeys(t, "media presign", presign, "upload_url", "storage_key", "upload_token", "expires_at")

	completeRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/media", e.storeID, productID),
		commerce.CompleteMediaUploadRequest{
			StorageKey:  presign["storage_key"].(string),
			UploadToken: presign["upload_token"].(string),
			AltText:     "Front",
			SortOrder:   1,
			IsPrimary:   true,
		})
	completeBody := completeRec.Body.Bytes()
	media := e.requireOK(t, completeRec, http.StatusCreated)
	writeFixture("media-complete.json", completeBody)
	jsonKeys(t, "media complete", media, "id", "product_id", "media_type", "uri", "alt_text", "sort_order", "is_primary", "created_at")

	// --- Locations + inventory envelopes ---
	locRec := e.do(t, http.MethodPost, "/internal/v1/stores/"+e.storeID+"/locations", map[string]any{
		"code": "main", "name": "Main Warehouse", "location_type": "warehouse", "status": "active",
	})
	locBody := locRec.Body.Bytes()
	location := e.requireOK(t, locRec, http.StatusCreated)
	writeFixture("location.json", locBody)
	locationID := location["id"].(string)

	locationsRec := e.do(t, http.MethodGet, "/internal/v1/stores/"+e.storeID+"/locations", nil)
	locationsBody := locationsRec.Body.Bytes()
	locations := e.requireOK(t, locationsRec, http.StatusOK)
	writeFixture("locations.json", locationsBody)
	if _, ok := locations["locations"]; !ok {
		t.Fatalf("locations response must use the {locations: [...]} envelope, got %v", locations)
	}
	if _, ok := locations["items"]; ok {
		t.Fatalf("locations response must not use the items envelope")
	}

	snapRec := e.do(t, http.MethodPost, "/internal/v1/stores/"+e.storeID+"/inventory/snapshots", map[string]any{
		"location_id": locationID, "sku_id": skuID, "on_hand_qty": 30,
	})
	snapshot := e.requireOK(t, snapRec, http.StatusCreated)

	invRec := e.do(t, http.MethodGet, "/internal/v1/stores/"+e.storeID+"/inventory", nil)
	invBody := invRec.Body.Bytes()
	inventory := e.requireOK(t, invRec, http.StatusOK)
	writeFixture("inventory.json", invBody)
	if _, ok := inventory["inventory"]; !ok {
		t.Fatalf("inventory response must use the {inventory: [...]} envelope, got %v", inventory)
	}
	if _, ok := inventory["items"]; ok {
		t.Fatalf("inventory response must not use the items envelope")
	}

	// --- Price, presentation, publish readiness ---
	if _, err := e.repo.SetSellerListingPrice(e.ctx, listingID, money.MustNew(15000, "EGP")); err != nil {
		t.Fatalf("set listing price: %v", err)
	}

	// --- Product list shape ---
	listRec := e.do(t, http.MethodGet, "/internal/v1/stores/"+e.storeID+"/products", nil)
	listBody := listRec.Body.Bytes()
	list := e.requireOK(t, listRec, http.StatusOK)
	writeFixture("product-list.json", listBody)
	jsonKeys(t, "product list", list, "products", "total", "limit", "offset")
	products := list["products"].([]any)
	if len(products) != 1 {
		t.Fatalf("products = %d, want 1", len(products))
	}
	item := products[0].(map[string]any)
	jsonKeys(t, "product list item", item,
		"product", "source", "name", "listing_id", "listing_status", "current_price", "inventory_summary", "publish_readiness")
	itemSummary := item["inventory_summary"].(map[string]any)
	jsonKeys(t, "list inventory_summary", itemSummary, "total_on_hand", "total_reserved", "total_available", "locations")

	pres := commerce.SellerListingPresentation{
		PurchaseBehavior: "add_to_cart",
		Sections: []commerce.ProductPageSection{
			{
				ID: "sec-1", Type: "description", Enabled: true, SortOrder: 1,
				Content: map[string]any{
					"en": map[string]any{"heading": "About", "body": "Body"},
					"ar": map[string]any{"heading": "نبذة", "body": "نص"},
				},
			},
			{
				ID: "sec-2", Type: "faq", Enabled: true, SortOrder: 2,
				Content: map[string]any{
					"en": map[string]any{"items": []any{map[string]any{"question": "Q?", "answer": "A."}}},
				},
			},
		},
	}
	presRec := e.do(t, http.MethodPut, fmt.Sprintf("/internal/v1/stores/%s/listings/%s/presentation", e.storeID, listingID), pres)
	presBody := presRec.Body.Bytes()
	presentation := e.requireOK(t, presRec, http.StatusOK)
	writeFixture("presentation.json", presBody)
	jsonKeys(t, "presentation", presentation, "seller_listing_id", "schema_version", "purchase_behavior", "sections")

	pubRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/products/%s/publish", e.storeID, productID), nil)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d (body %q)", pubRec.Code, pubRec.Body.String())
	}

	// Published product detail is the canonical detail fixture.
	detailRec := e.do(t, http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/products/%s", e.storeID, productID), nil)
	detailBody := detailRec.Body.Bytes()
	detail = e.requireOK(t, detailRec, http.StatusOK)
	writeFixture("product-detail.json", detailBody)
	if detail["publish_readiness"].(map[string]any)["is_ready"] != true {
		t.Fatalf("published product must be ready: %v", detail["publish_readiness"])
	}
	agg := detail["inventory_summary"].(map[string]any)
	if agg["total_available"].(float64) != 30 {
		t.Fatalf("inventory aggregate total_available = %v, want 30", agg["total_available"])
	}
	if len(agg["locations"].([]any)) != 1 {
		t.Fatalf("inventory aggregate locations = %v", agg["locations"])
	}

	// --- Order detail: contact email + timeline, no internal leaks ---
	_, token, err := e.repo.CreateCart(e.ctx, e.storeID, "EG", nil)
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := e.repo.AddCartItem(e.ctx, e.storeID, token, skuID, 2); err != nil {
		t.Fatalf("AddCartItem: %v", err)
	}
	session, _, err := e.repo.CreateCheckoutSession(e.ctx, e.storeID, token, nil, 30*time.Minute)
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	order, err := e.repo.FinalizeCheckout(e.ctx, e.storeID, commerce.FinalizeRequest{
		SessionID: session.ID,
		ShippingAddress: commerce.ShippingAddress{
			RecipientName: "Test Buyer",
			AddressLine1:  "1 Main St",
			City:          "Cairo",
			CountryCode:   "EG",
		},
		ContactEmail: "buyer@contract.test",
	}, "corr-contract")
	if err != nil {
		t.Fatalf("FinalizeCheckout: %v", err)
	}

	orderRec := e.do(t, http.MethodGet, fmt.Sprintf("/internal/v1/stores/%s/orders/%s", e.storeID, order.ID), nil)
	orderBody := orderRec.Body.Bytes()
	orderDetail := e.requireOK(t, orderRec, http.StatusOK)
	writeFixture("order-detail.json", orderBody)
	jsonKeys(t, "order detail", orderDetail,
		"id", "order_number", "status", "currency", "subtotal", "total", "item_count",
		"shipping_address", "contact_email", "items", "timeline", "allowed_next_actions", "created_at", "updated_at")
	if orderDetail["contact_email"] != "buyer@contract.test" {
		t.Fatalf("contact_email = %v", orderDetail["contact_email"])
	}
	if len(orderDetail["timeline"].([]any)) == 0 {
		t.Fatalf("timeline must be real, not empty")
	}
	// Buyer-safe DTO: no supplier cost or internal handles.
	raw := string(orderBody)
	for _, leak := range []string{"supplier_cost", "reservation", "fulfillment_location_id", "guest_capability", "actor_subject"} {
		if bytes.Contains([]byte(raw), []byte(leak)) {
			t.Fatalf("order detail leaks internal field %q", leak)
		}
	}

	// --- Transition: allowed_next_actions drive the lifecycle ---
	actions := orderDetail["allowed_next_actions"].([]any)
	if len(actions) == 0 || actions[0].(string) != "confirmed" {
		t.Fatalf("allowed_next_actions = %v", actions)
	}
	transRec := e.do(t, http.MethodPost, fmt.Sprintf("/internal/v1/stores/%s/orders/%s/transition", e.storeID, order.ID),
		OrderTransitionRequest{TargetStatus: "confirmed"})
	transBody := transRec.Body.Bytes()
	transitioned := e.requireOK(t, transRec, http.StatusOK)
	writeFixture("order-transition.json", transBody)
	if transitioned["status"] != "confirmed" {
		t.Fatalf("status after transition = %v", transitioned["status"])
	}
	if len(transitioned["timeline"].([]any)) < 2 {
		t.Fatalf("timeline must contain the transition event")
	}
	_ = snapshot
}
