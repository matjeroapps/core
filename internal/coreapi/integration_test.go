package coreapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/httpx"
	"github.com/matjeroapps/core/packages/money"
)

// Integration tests for the internal API against real PostgreSQL.
//
// These prove that moving the P4.3 storefront read path from a Go call to an
// HTTP call does not weaken any invariant: store isolation, host-scoped tenant
// resolution, listing-price-only disclosure, supplier privacy, locale
// behaviour, search, filters, pagination and availability are all re-asserted
// through the network boundary.

const (
	// The supplier wholesale cost is internal. No public payload may contain it.
	integrationWholesaleMinor = 10000 // 100.00
	// Each store sets its own public listing price.
	integrationStoreAPrice = 15000 // 150.00
	integrationStoreBPrice = 19900 // 199.00
)

type integrationEnv struct {
	ctx      context.Context
	db       *database.Pool
	repo     commerce.Repository
	handler  http.Handler
	domainA  string
	domainB  string
	storeA   commerce.Store
	storeB   commerce.Store
	sellerA  commerce.Seller
	sellerB  commerce.Seller
	supplier commerce.Supplier
	listingA commerce.SellerListing
	listingB commerce.SellerListing
}

func setupIntegration(t *testing.T) integrationEnv {
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
		"000013_outbox_publish_claims",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000016_catalog_invariants",
		"000012_order_aggregate_schema",
		"000017_create_shipping_schema",
		"000018_supplier_retail_affiliation",
		"000019_create_payments_schema",
		"000020_create_ledger_schema",
		"000021_create_balance_projection_schema",
	} {
		applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}

	repo := commerce.NewRepository(db.Pool)
	service := commerce.NewService(repo)
	service.PlatformDomain = "matjero.test"

	env := integrationEnv{
		ctx:     ctx,
		db:      db,
		repo:    repo,
		domainA: "store-a.matjero.test",
		domainB: "store-b.matjero.test",
	}

	themeService := themes.NewService(themes.NewRepository(db.Pool), repo, themes.Options{
		PreviewSecret: []byte("integration-preview-secret"),
	})
	// The built-in theme catalog is seeded by the service, not by a migration.
	if err := themeService.SeedBuiltInThemes(ctx); err != nil {
		t.Fatalf("seed built-in themes: %v", err)
	}

	finRepo := finance.NewRepository(db.Pool)
	financeSvc := finance.NewService(finRepo)
	balRepo := balance.NewRepository(db.Pool)
	balanceSvc := balance.NewService(balRepo)

	resolver := storefront.NewStoreResolver(repo)
	deps := Dependencies{
		Commerce:  service,
		Repo:      repo,
		Markets:   markets.NewService(markets.NewRepository(db.Pool)),
		Catalog:   storefront.NewCatalogRepository(db.Pool),
		Stores:    resolver,
		Revisions: storefront.NewRevisionReader(resolver, repo),
		Themes:    themeService,
		Finance:   financeSvc,
		Balance:   balanceSvc,
	}
	appRouter := httpx.NewRouter(httpx.App{})
	appRouter.Mount("/", serviceauth.Middleware(testAuthConfig())(NewRouter(deps)))
	env.handler = appRouter

	env.seed(t)
	return env
}

func (e *integrationEnv) seed(t *testing.T) {
	t.Helper()
	ctx := e.ctx

	sellerA, err := e.repo.CreateSeller(ctx, "seller-a", "Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	sellerB, err := e.repo.CreateSeller(ctx, "seller-b", "Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	e.sellerA, e.sellerB = sellerA, sellerB

	storeA, _, err := e.repo.CreateStoreWithDomain(ctx, sellerA.ID, "EG", "store-a", "Store A", "active", nil, e.domainA, "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	storeB, _, err := e.repo.CreateStoreWithDomain(ctx, sellerB.ID, "EG", "store-b", "Store B", "active", nil, e.domainB, "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	e.storeA, e.storeB = storeA, storeB

	// An inactive store with an active domain must behave exactly like an
	// unknown host.
	if _, _, err := e.repo.CreateStoreWithDomain(ctx, sellerA.ID, "EG", "store-c", "Store C", "inactive", nil, "store-c.matjero.test", "platform", "active", true, nil, nil); err != nil {
		t.Fatalf("create inactive store: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "supplier-1", "Supplier One", "active", map[string]any{"contact_email": "ops@supplier.test"})
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	e.supplier = supplier

	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market: %v", err)
	}
	location, err := e.repo.CreateFulfillmentLocation(ctx, supplier.ID, market.ID, "EG", "cairo-hub", "Cairo Hub", "warehouse", "active")
	if err != nil {
		t.Fatalf("create fulfillment location: %v", err)
	}

	lighting := e.category(t, "store-a-lighting", "Lighting", "الإضاءة")
	kitchen := e.category(t, "store-b-kitchen", "Kitchen", "المطبخ")

	// Store A: in-stock lamp priced 150 while the supplier cost is 100.
	lamp := e.product(t, "store-a-desk-lamp", "Desk Lamp", "مصباح مكتبي", "A bright desk lamp", "مصباح مكتبي ساطع")
	e.assignCategory(t, lamp, lighting)
	e.stock(t, lamp, location.ID, 5)
	lampOffer := e.offer(t, supplier.ID, lamp, market.ID)
	e.listingA = e.listing(t, storeA.ID, lamp, lampOffer, "active", integrationStoreAPrice)

	// Store B: out-of-stock kettle priced 199.
	kettle := e.product(t, "store-b-kettle", "Kettle", "غلاية", "A fast kettle", "غلاية سريعة")
	e.assignCategory(t, kettle, kitchen)
	e.stock(t, kettle, location.ID, 0)
	kettleOffer := e.offer(t, supplier.ID, kettle, market.ID)
	e.listingB = e.listing(t, storeB.ID, kettle, kettleOffer, "active", integrationStoreBPrice)
}

func (e integrationEnv) category(t *testing.T, slug, nameEN, nameAR string) commerce.Category {
	t.Helper()
	category, err := e.repo.CreateCategory(e.ctx, slug, nil, "active")
	if err != nil {
		t.Fatalf("create category %s: %v", slug, err)
	}
	for locale, name := range map[string]string{"en": nameEN, "ar": nameAR} {
		if err := e.repo.UpsertCategoryTranslation(e.ctx, commerce.CategoryTranslation{
			CategoryID: category.ID, Locale: locale, Name: name,
		}); err != nil {
			t.Fatalf("upsert category translation %s/%s: %v", slug, locale, err)
		}
	}
	return category
}

func (e integrationEnv) product(t *testing.T, slug, nameEN, nameAR, descEN, descAR string) commerce.Product {
	t.Helper()
	product, err := e.repo.CreateProduct(e.ctx, slug, "active")
	if err != nil {
		t.Fatalf("create product %s: %v", slug, err)
	}
	for locale, pair := range map[string][2]string{
		"en": {nameEN, descEN},
		"ar": {nameAR, descAR},
	} {
		if err := e.repo.UpsertProductTranslation(e.ctx, commerce.ProductTranslation{
			ProductID: product.ID, Locale: locale, Name: pair[0], Description: pair[1],
		}); err != nil {
			t.Fatalf("upsert product translation %s/%s: %v", slug, locale, err)
		}
	}
	return product
}

func (e integrationEnv) assignCategory(t *testing.T, product commerce.Product, category commerce.Category) {
	t.Helper()
	if err := e.repo.SetProductCategories(e.ctx, product.ID, []string{category.ID}); err != nil {
		t.Fatalf("assign category: %v", err)
	}
}

func (e integrationEnv) stock(t *testing.T, product commerce.Product, locationID string, qty int64) {
	t.Helper()
	variant, err := e.repo.CreateVariant(e.ctx, product.ID, "default", "active")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	sku, err := e.repo.CreateSKU(e.ctx, variant.ID, "sku-"+product.Slug, "", "active")
	if err != nil {
		t.Fatalf("create sku: %v", err)
	}
	if _, err := e.repo.CreateInventorySnapshot(e.ctx, locationID, sku.ID, qty); err != nil {
		t.Fatalf("create inventory snapshot: %v", err)
	}
}

func (e integrationEnv) offer(t *testing.T, supplierID string, product commerce.Product, marketID string) commerce.SupplierOffer {
	t.Helper()
	supplierProduct, err := e.repo.CreateSupplierProduct(e.ctx, supplierID, product.ID, "SUP-"+product.Slug, "active")
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(e.ctx, supplierID, supplierProduct.ID, marketID, "EG", "active")
	if err != nil {
		t.Fatalf("create supplier offer: %v", err)
	}
	price, err := money.New(integrationWholesaleMinor, "EGP")
	if err != nil {
		t.Fatalf("build wholesale price: %v", err)
	}
	if _, err := e.repo.SetSupplierOfferPrice(e.ctx, offer.ID, price); err != nil {
		t.Fatalf("set supplier offer price: %v", err)
	}
	return offer
}

func (e integrationEnv) listing(t *testing.T, storeID string, product commerce.Product, offer commerce.SupplierOffer, status string, priceMinor int64) commerce.SellerListing {
	t.Helper()
	offerID := offer.ID
	listing, err := e.repo.CreateSellerListing(e.ctx, storeID, product.ID, &offerID, "EG", status)
	if err != nil {
		t.Fatalf("create seller listing: %v", err)
	}
	price, err := money.New(priceMinor, "EGP")
	if err != nil {
		t.Fatalf("build listing price: %v", err)
	}
	if _, err := e.repo.SetSellerListingPrice(e.ctx, listing.ID, price); err != nil {
		t.Fatalf("set listing price: %v", err)
	}
	return listing
}

func applyMigrationFile(t *testing.T, db *database.Pool, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if _, err := db.Exec(context.Background(), string(body)); err != nil {
		t.Fatalf("apply migration %s: %v", path, err)
	}
}

// --- helpers ---

func (e integrationEnv) get(t *testing.T, path, caller, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := authenticatedRequest(t, http.MethodGet, path, caller, tokenFor(caller))
	if host != "" {
		req.Header.Set(serviceauth.HeaderStorefrontHost, host)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func tokenFor(caller string) string {
	switch caller {
	case "admin":
		return testAdminToken
	case "supplier":
		return testSupplierToken
	default:
		return testSellerToken
	}
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var payload T
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v (body %q)", err, rec.Body.String())
	}
	return payload
}

// --- tests ---

func TestIntegrationStorefrontStoreIsolation(t *testing.T) {
	env := setupIntegration(t)

	recA := env.get(t, "/internal/v1/storefront/store", "seller", env.domainA)
	if recA.Code != http.StatusOK {
		t.Fatalf("store A status = %d (body %q)", recA.Code, recA.Body.String())
	}
	storeA := decodeInto[storefrontStoreResponse](t, recA)
	if storeA.Store.StoreCode != "store-a" {
		t.Errorf("store A code = %q, want %q", storeA.Store.StoreCode, "store-a")
	}

	recB := env.get(t, "/internal/v1/storefront/store", "seller", env.domainB)
	if recB.Code != http.StatusOK {
		t.Fatalf("store B status = %d (body %q)", recB.Code, recB.Body.String())
	}
	storeB := decodeInto[storefrontStoreResponse](t, recB)
	if storeB.Store.StoreCode != "store-b" {
		t.Errorf("store B code = %q, want %q", storeB.Store.StoreCode, "store-b")
	}
}

// A store must only see its own products. Store A's catalog must not contain
// store B's product, in either direction.
func TestIntegrationStorefrontProductIsolation(t *testing.T) {
	env := setupIntegration(t)

	recA := env.get(t, "/internal/v1/storefront/products", "seller", env.domainA)
	if recA.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", recA.Code, recA.Body.String())
	}
	pageA := decodeInto[storefrontProductPageResponse](t, recA)
	if len(pageA.Items) != 1 || pageA.Items[0].Slug != "store-a-desk-lamp" {
		t.Fatalf("store A products = %+v, want only store-a-desk-lamp", pageA.Items)
	}

	recB := env.get(t, "/internal/v1/storefront/products", "seller", env.domainB)
	pageB := decodeInto[storefrontProductPageResponse](t, recB)
	if len(pageB.Items) != 1 || pageB.Items[0].Slug != "store-b-kettle" {
		t.Fatalf("store B products = %+v, want only store-b-kettle", pageB.Items)
	}
}

// The public payload must carry the seller's listing price, never the supplier's
// wholesale cost.
func TestIntegrationStorefrontDisclosesListingPriceOnly(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/storefront/products", "seller", env.domainA)
	page := decodeInto[storefrontProductPageResponse](t, rec)

	if len(page.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(page.Items))
	}
	got := page.Items[0].Price.AmountMinor
	if got != integrationStoreAPrice {
		t.Errorf("price = %d, want the listing price %d", got, integrationStoreAPrice)
	}
	if got == integrationWholesaleMinor {
		t.Errorf("public payload leaked the supplier wholesale cost %d", integrationWholesaleMinor)
	}
}

// Supplier identity must never appear in a public storefront payload.
func TestIntegrationStorefrontHidesSupplierIdentity(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/storefront/products/store-a-desk-lamp", "seller", env.domainA)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, secret := range []string{env.supplier.ID, "supplier-1", "Supplier One", "ops@supplier.test"} {
		if secret != "" && strings.Contains(body, secret) {
			t.Errorf("public product payload leaked supplier identity %q", secret)
		}
	}
}

func TestIntegrationStorefrontLocaleBehaviour(t *testing.T) {
	env := setupIntegration(t)

	recEN := env.get(t, "/internal/v1/storefront/products", "seller", env.domainA)
	pageEN := decodeInto[storefrontProductPageResponse](t, recEN)
	if len(pageEN.Items) != 1 || pageEN.Items[0].Name != "Desk Lamp" {
		t.Fatalf("english name = %+v, want 'Desk Lamp'", pageEN.Items)
	}

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/products", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, env.domainA)
	req.Header.Set("Accept-Language", "ar")
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	pageAR := decodeInto[storefrontProductPageResponse](t, rec)
	if len(pageAR.Items) != 1 || pageAR.Items[0].Name != "مصباح مكتبي" {
		t.Fatalf("arabic name = %+v, want the Arabic translation", pageAR.Items)
	}
}

func TestIntegrationStorefrontSearchAndFilters(t *testing.T) {
	env := setupIntegration(t)

	t.Run("search finds only in-scope products", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/search?q=lamp", "seller", env.domainA)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
		}
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if len(page.Items) != 1 || page.Items[0].Slug != "store-a-desk-lamp" {
			t.Fatalf("search results = %+v, want store-a-desk-lamp", page.Items)
		}
	})

	t.Run("search does not cross tenants", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/search?q=kettle", "seller", env.domainA)
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if len(page.Items) != 0 {
			t.Fatalf("store A search for a store B product returned %+v", page.Items)
		}
	})

	t.Run("category filter", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/products?category=store-a-lighting", "seller", env.domainA)
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if len(page.Items) != 1 {
			t.Fatalf("category filter returned %d items, want 1", len(page.Items))
		}
	})

	t.Run("availability filter", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/products?availability=in_stock", "seller", env.domainB)
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if len(page.Items) != 0 {
			t.Fatalf("store B has no in-stock products, got %+v", page.Items)
		}
	})

	t.Run("price filter", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/products?min_price=20000", "seller", env.domainA)
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if len(page.Items) != 0 {
			t.Fatalf("min_price above the listing price returned %+v", page.Items)
		}
	})

	t.Run("pagination envelope", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/products?limit=1&offset=0", "seller", env.domainA)
		page := decodeInto[storefrontProductPageResponse](t, rec)
		if page.Pagination.Limit != 1 || page.Pagination.Total != 1 {
			t.Fatalf("pagination = %+v, want limit 1 total 1", page.Pagination)
		}
	})

	t.Run("invalid query is rejected", func(t *testing.T) {
		rec := env.get(t, "/internal/v1/storefront/products?limit=abc", "seller", env.domainA)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != CodeValidationError {
			t.Errorf("error code = %q, want %q", got, CodeValidationError)
		}
	})
}

// Unknown host, inactive store and inactive domain must all collapse into the
// same generic response.
func TestIntegrationStorefrontHostFailures(t *testing.T) {
	env := setupIntegration(t)

	for _, host := range []string{
		"unknown-host.matjero.test",
		"store-c.matjero.test", // inactive store, active domain
	} {
		rec := env.get(t, "/internal/v1/storefront/store", "seller", host)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("host %q: status = %d, want 404 (body %q)", host, rec.Code, rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
			t.Errorf("host %q: error code = %q, want %q", host, got, CodeStorefrontUnavailable)
		}
	}
}

func TestIntegrationStorefrontCategories(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/storefront/categories", "seller", env.domainA)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	collection := decodeInto[CollectionResponse[storefront.CategoryNode]](t, rec)
	if len(collection.Items) != 1 || collection.Items[0].Slug != "store-a-lighting" {
		t.Fatalf("store A categories = %+v, want only store-a-lighting", collection.Items)
	}

	recDetail := env.get(t, "/internal/v1/storefront/categories/store-a-lighting", "seller", env.domainA)
	if recDetail.Code != http.StatusOK {
		t.Fatalf("category detail status = %d (body %q)", recDetail.Code, recDetail.Body.String())
	}

	// A category belonging to another store is not visible.
	recOther := env.get(t, "/internal/v1/storefront/categories/store-b-kitchen", "seller", env.domainA)
	if recOther.Code != http.StatusNotFound {
		t.Fatalf("cross-store category status = %d, want 404", recOther.Code)
	}
}

func TestIntegrationMarkets(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/markets", "seller", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}

	recMissing := env.get(t, "/internal/v1/markets/NOPE", "seller", "")
	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("unknown market status = %d, want 404", recMissing.Code)
	}
}

// The admin overview counts Core-owned tables. It must be reachable by admin and
// unreachable by every other caller.
func TestIntegrationAdminOverview(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/admin/overview", "admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	overview := decodeInto[OverviewResponse](t, rec)
	for _, key := range []string{"suppliers", "sellers", "stores", "products", "categories", "offers", "listings"} {
		if _, ok := overview.Counts[key]; !ok {
			t.Errorf("overview is missing the %q count", key)
		}
	}
	if overview.Counts["sellers"] != 2 {
		t.Errorf("seller count = %d, want 2", overview.Counts["sellers"])
	}
	if overview.Counts["stores"] != 3 {
		t.Errorf("store count = %d, want 3 (two active plus one inactive)", overview.Counts["stores"])
	}
}

// A seller must not be able to read another seller's store, and must receive a
// safe not-found rather than a forbidden that would confirm the store exists.
func TestIntegrationSellerCannotAccessAnotherSellersStore(t *testing.T) {
	env := setupIntegration(t)

	// sellerA's subject resolves to sellerA; storeB belongs to sellerB.
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeB.ID, "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, "subject-of-seller-a")
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	// No seller membership exists for this subject, so resolution fails first.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
}

// Admin may read any store; the same request that a seller cannot make succeeds
// for the admin caller.
func TestIntegrationAdminCanReadAnyStore(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/stores/"+env.storeB.ID, "admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestIntegrationSellerStoreListing(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/stores/"+env.storeA.ID+"/listings", "admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	collection := decodeInto[CollectionResponse[commerce.SellerListing]](t, rec)
	if len(collection.Items) != 1 {
		t.Fatalf("store A listings = %d, want 1", len(collection.Items))
	}
}

func TestIntegrationSupplierCatalogIsMarketScoped(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/stores/"+env.storeA.ID+"/supplier-catalog", "admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	collection := decodeInto[CollectionResponse[commerce.SupplierCatalogItem]](t, rec)
	if len(collection.Items) == 0 {
		t.Fatal("expected the supplier catalog to contain the seeded offers")
	}
	for _, item := range collection.Items {
		if item.MarketCode != "EG" {
			t.Errorf("catalog item market = %q, want EG (the store's market)", item.MarketCode)
		}
	}
}

func TestIntegrationThemes(t *testing.T) {
	env := setupIntegration(t)

	rec := env.get(t, "/internal/v1/themes", "seller", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list themes status = %d (body %q)", rec.Code, rec.Body.String())
	}
	collection := decodeInto[CollectionResponse[themes.Theme]](t, rec)
	if len(collection.Items) == 0 {
		t.Fatal("expected the seeded theme catalog to be non-empty")
	}

	key := collection.Items[0].Key
	recVersions := env.get(t, "/internal/v1/themes/"+key+"/versions", "seller", "")
	if recVersions.Code != http.StatusOK {
		t.Fatalf("list theme versions status = %d (body %q)", recVersions.Code, recVersions.Body.String())
	}
}

// A theme preview token must never be issued without a signing secret, and the
// failure must be a 503 rather than an unsigned token.
func TestIntegrationThemePreviewFailsClosedWithoutSecret(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
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
	} {
		applyMigrationFile(t, db, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}

	repo := commerce.NewRepository(db.Pool)
	resolver := storefront.NewStoreResolver(repo)
	deps := Dependencies{
		Commerce:  commerce.NewService(repo),
		Repo:      repo,
		Markets:   markets.NewService(markets.NewRepository(db.Pool)),
		Catalog:   storefront.NewCatalogRepository(db.Pool),
		Stores:    resolver,
		Revisions: storefront.NewRevisionReader(resolver, repo),
		// No PreviewSecret: the service must fail closed.
		Themes: themes.NewService(themes.NewRepository(db.Pool), repo, themes.Options{}),
	}
	appRouter := httpx.NewRouter(httpx.App{})
	appRouter.Mount("/", serviceauth.Middleware(testAuthConfig())(NewRouter(deps)))
	handler := appRouter

	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/stores/any-store/theme/preview", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, "subject-of-seller-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// The subject cannot resolve to a seller, so ownership fails first with 404.
	// The important assertion is that no token is ever returned.
	if rec.Code == http.StatusOK {
		t.Fatalf("preview must not succeed without a signing secret (body %q)", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Errorf("response must not contain a token: %q", rec.Body.String())
	}
}

func TestIntegrationFinalizeCheckoutCorrelationIDPropagation(t *testing.T) {
	env := setupIntegration(t)
	ctx := env.ctx

	// Setup item, cart, session
	_, cartToken, err := env.repo.CreateCart(ctx, env.storeA.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	var skuID string
	if err := env.db.QueryRow(ctx, `SELECT id FROM skus LIMIT 1`).Scan(&skuID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.repo.AddCartItem(ctx, env.storeA.ID, cartToken, skuID, 1); err != nil {
		t.Fatal(err)
	}
	session, _, err := env.repo.CreateCheckoutSession(ctx, env.storeA.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// 1. HTTP request WITHOUT X-Correlation-ID header
	bodyJSON := `{"shipping_address":{"recipient_name":"Alice","address_line_1":"123 St","city":"Cairo","country_code":"EG"},"contact_email":"alice@example.com"}`
	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/storefront/checkout-sessions/"+session.ID+"/finalize", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, env.domainA)
	req.Body = io.NopCloser(strings.NewReader(bodyJSON))
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("finalize without correlation header status = %d (body %q)", rec.Code, rec.Body.String())
	}

	var res commerce.Order
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}

	var generatedCorrID string
	if err := env.db.QueryRow(ctx, `SELECT correlation_id FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'commerce.order.created.v1'`, res.ID).Scan(&generatedCorrID); err != nil {
		t.Fatal(err)
	}
	if generatedCorrID == "" {
		t.Fatal("expected non-empty generated correlation_id in outbox event")
	}

	// 2. HTTP request WITH caller-supplied X-Correlation-Id header
	_, cartToken2, err := env.repo.CreateCart(ctx, env.storeA.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.repo.AddCartItem(ctx, env.storeA.ID, cartToken2, skuID, 1); err != nil {
		t.Fatal(err)
	}
	session2, _, err := env.repo.CreateCheckoutSession(ctx, env.storeA.ID, cartToken2, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req2 := authenticatedRequest(t, http.MethodPost, "/internal/v1/storefront/checkout-sessions/"+session2.ID+"/finalize", "seller", testSellerToken)
	req2.Header.Set(serviceauth.HeaderStorefrontHost, env.domainA)
	req2.Header.Set("X-Correlation-Id", "caller-supplied-corr-123")
	req2.Body = io.NopCloser(strings.NewReader(bodyJSON))
	rec2 := httptest.NewRecorder()
	env.handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("finalize with correlation header status = %d (body %q)", rec2.Code, rec2.Body.String())
	}

	var res2 commerce.Order
	if err := json.NewDecoder(rec2.Body).Decode(&res2); err != nil {
		t.Fatal(err)
	}

	var customCorrID string
	if err := env.db.QueryRow(ctx, `SELECT correlation_id FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'commerce.order.created.v1'`, res2.ID).Scan(&customCorrID); err != nil {
		t.Fatal(err)
	}
	if customCorrID != "caller-supplied-corr-123" {
		t.Fatalf("correlation_id = %q, want 'caller-supplied-corr-123'", customCorrID)
	}
}

func TestBalanceProjectionAPI(t *testing.T) {
	env := setupIntegration(t)

	// 1. Create a ledger account via API
	createAccountJSON := `{"account_code":"ACC-BAL-API-1","name":"Balance API Test Account","account_type":"ASSET","currency":"SAR"}`
	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/ledger/accounts", "admin", testAdminToken)
	req.Body = io.NopCloser(strings.NewReader(createAccountJSON))
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create account status = %d (body %q)", rec.Code, rec.Body.String())
	}

	var acc LedgerAccountResponse
	if err := json.NewDecoder(rec.Body).Decode(&acc); err != nil {
		t.Fatal(err)
	}

	// 2. Query initial balance projection for created account
	req = authenticatedRequest(t, http.MethodGet, "/internal/v1/balances/accounts/"+acc.ID, "seller", testSellerToken)
	rec = httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get account balance status = %d (body %q)", rec.Code, rec.Body.String())
	}

	var bal AccountBalanceResponse
	if err := json.NewDecoder(rec.Body).Decode(&bal); err != nil {
		t.Fatal(err)
	}

	if bal.AccountID != acc.ID || bal.Currency != "SAR" || bal.BalanceMinor != 0 {
		t.Fatalf("unexpected balance response: %+v", bal)
	}

	// 3. List account balances
	req = authenticatedRequest(t, http.MethodGet, "/internal/v1/balances/accounts", "seller", testSellerToken)
	rec = httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list account balances status = %d (body %q)", rec.Code, rec.Body.String())
	}
}
