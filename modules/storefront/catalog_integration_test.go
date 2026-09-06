package storefront

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/i18n"
	"github.com/matjeroapps/core/packages/money"
)

// Fixture prices. Each store's public price is its own seller listing price; the
// supplier wholesale cost below must never reach a public payload.
const (
	supplierWholesaleMinor = 10000 // 100.00
	storeAPriceMinor       = 15000 // 150.00
	storeBPriceMinor       = 19900 // 199.00
	storeACheapPriceMinor  = 5000  // 50.00
)

// catalogEnv is a two-store fixture with intentionally distinct domains,
// products, categories, prices, and translations so cross-store leakage in either
// direction is detectable.
type catalogEnv struct {
	ctx      context.Context
	db       *database.Pool
	commerce commerce.Repository
	catalog  CatalogRepository
	resolver StoreResolver

	storeA commerce.Store
	storeB commerce.Store

	domainA string
	domainB string

	supplierID string
}

func setupCatalogTest(t *testing.T) catalogEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)

	for _, m := range []string{
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
		applySQLFileStorefront(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}

	repo := commerce.NewRepository(db.Pool)
	env := catalogEnv{
		ctx:      ctx,
		db:       db,
		commerce: repo,
		catalog:  NewCatalogRepository(db.Pool),
		resolver: NewStoreResolver(repo),
		domainA:  "store-a.matjero.test",
		domainB:  "store-b.matjero.test",
	}

	seller, err := repo.CreateSeller(ctx, "seller-a", "Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	sellerB, err := repo.CreateSeller(ctx, "seller-b", "Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}

	settingsA := map[string]any{
		"public":   map[string]any{"tagline": "Store A tagline"},
		"internal": map[string]any{"supplier_margin_target": 0.35},
	}
	storeA, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-a", "Store A", "active", settingsA, env.domainA, "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	storeB, _, err := repo.CreateStoreWithDomain(ctx, sellerB.ID, "EG", "store-b", "Store B", "active", nil, env.domainB, "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	env.storeA, env.storeB = storeA, storeB

	supplier, err := repo.CreateSupplier(ctx, "supplier-1", "Supplier One", "active", map[string]any{"contact_email": "ops@supplier.test"})
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	env.supplierID = supplier.ID

	marketEG, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market EG: %v", err)
	}
	marketSA, err := repo.CreateSupplierMarket(ctx, supplier.ID, "SA", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market SA: %v", err)
	}
	locationEG, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, marketEG.ID, "EG", "cairo-hub", "Cairo Hub", "warehouse", "active")
	if err != nil {
		t.Fatalf("create fulfillment location: %v", err)
	}

	catA := env.category(t, "store-a-lighting", nil, "active", "Lighting", "الإضاءة")
	catAChild := env.category(t, "store-a-lamps", &catA, "active", "Lamps", "المصابيح")
	catB := env.category(t, "store-b-kitchen", nil, "active", "Kitchen", "المطبخ")

	// Store A: in-stock product in a child category, priced 150 while the supplier
	// wholesale cost is 100.
	lamp := env.product(t, "store-a-desk-lamp", "active", "Desk Lamp", "مصباح مكتبي", "A bright desk lamp", "مصباح مكتبي ساطع")
	env.assignCategories(t, lamp, catAChild)
	env.media(t, lamp, "https://cdn.matjero.test/lamp.jpg", "Desk Lamp")
	lampSKU := env.variantWithStock(t, lamp, "default", locationEG.ID, 5)
	lampOffer := env.supplierOffer(t, supplier.ID, lamp, marketEG.ID, "EG", supplierWholesaleMinor, true)
	env.listing(t, storeA.ID, lamp, &lampOffer, "EG", "active", storeAPriceMinor)
	_ = lampSKU

	// Store A: cheaper out-of-stock product for price/availability/sort coverage.
	shade := env.product(t, "store-a-lamp-shade", "active", "Lamp Shade", "غطاء مصباح", "", "")
	env.assignCategories(t, shade, catA)
	env.variantWithStock(t, shade, "default", locationEG.ID, 0)
	shadeOffer := env.supplierOffer(t, supplier.ID, shade, marketEG.ID, "EG", supplierWholesaleMinor, true)
	env.listing(t, storeA.ID, shade, &shadeOffer, "EG", "active", storeACheapPriceMinor)

	// Store A: draft listing on an otherwise active product must stay hidden.
	hidden := env.product(t, "store-a-hidden", "active", "Hidden Item", "عنصر مخفي", "", "")
	env.assignCategories(t, hidden, catA)
	env.variantWithStock(t, hidden, "default", locationEG.ID, 3)
	hiddenOffer := env.supplierOffer(t, supplier.ID, hidden, marketEG.ID, "EG", supplierWholesaleMinor, true)
	env.listing(t, storeA.ID, hidden, &hiddenOffer, "EG", "draft", storeAPriceMinor)

	// Store A: active listing whose product is draft must stay hidden.
	draftProduct := env.product(t, "store-a-draft-product", "draft", "Draft Product", "منتج مسودة", "", "")
	env.variantWithStock(t, draftProduct, "default", locationEG.ID, 3)
	draftOffer := env.supplierOffer(t, supplier.ID, draftProduct, marketEG.ID, "EG", supplierWholesaleMinor, true)
	env.listing(t, storeA.ID, draftProduct, &draftOffer, "EG", "active", storeAPriceMinor)

	// Store A: listing without a current price must stay hidden.
	unpriced := env.product(t, "store-a-unpriced", "active", "Unpriced", "بدون سعر", "", "")
	env.variantWithStock(t, unpriced, "default", locationEG.ID, 3)
	if _, err := repo.CreateSellerListing(ctx, storeA.ID, unpriced.ID, nil, "EG", "active"); err != nil {
		t.Fatalf("create unpriced listing: %v", err)
	}

	// Store B: distinct product, category, translations, and price.
	pan := env.product(t, "store-b-frying-pan", "active", "Frying Pan", "مقلاة", "A non-stick pan", "مقلاة غير لاصقة")
	env.assignCategories(t, pan, catB)
	env.variantWithStock(t, pan, "default", locationEG.ID, 7)
	panOffer := env.supplierOffer(t, supplier.ID, pan, marketEG.ID, "EG", supplierWholesaleMinor, true)
	env.listing(t, storeB.ID, pan, &panOffer, "EG", "active", storeBPriceMinor)

	// Cross-market product: a store in EG must never see an SA-market listing.
	// The seller_listings composite FK requires an SA store, so the listing is
	// created on a dedicated SA store owned by seller A.
	storeASA, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "SA", "store-a-sa", "Store A SA", "active", nil, "store-a-sa.matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create SA store: %v", err)
	}
	kettle := env.product(t, "store-a-kettle-sa", "active", "SA Kettle", "غلاية", "", "")
	env.assignCategories(t, kettle, catA)
	locationSA, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, marketSA.ID, "SA", "riyadh-hub", "Riyadh Hub", "warehouse", "active")
	if err != nil {
		t.Fatalf("create SA location: %v", err)
	}
	env.variantWithStock(t, kettle, "default", locationSA.ID, 4)
	kettleOffer := env.supplierOffer(t, supplier.ID, kettle, marketSA.ID, "SA", supplierWholesaleMinor, true)
	env.listing(t, storeASA.ID, kettle, &kettleOffer, "SA", "active", storeAPriceMinor)

	return env
}

func (e catalogEnv) category(t *testing.T, slug string, parent *commerce.Category, status, nameEN, nameAR string) commerce.Category {
	t.Helper()
	var parentID *string
	if parent != nil {
		parentID = &parent.ID
	}
	category, err := e.commerce.CreateCategory(e.ctx, slug, parentID, status)
	if err != nil {
		t.Fatalf("create category %s: %v", slug, err)
	}
	for locale, name := range map[string]string{"en": nameEN, "ar": nameAR} {
		if name == "" {
			continue
		}
		if err := e.commerce.UpsertCategoryTranslation(e.ctx, commerce.CategoryTranslation{
			CategoryID: category.ID, Locale: locale, Name: name,
		}); err != nil {
			t.Fatalf("translate category %s/%s: %v", slug, locale, err)
		}
	}
	return category
}

func (e catalogEnv) product(t *testing.T, slug, status, nameEN, nameAR, descEN, descAR string) commerce.Product {
	t.Helper()
	product, err := e.commerce.CreateProduct(e.ctx, slug, status)
	if err != nil {
		t.Fatalf("create product %s: %v", slug, err)
	}
	translations := []commerce.ProductTranslation{
		{ProductID: product.ID, Locale: "en", Name: nameEN, Description: descEN},
		{ProductID: product.ID, Locale: "ar", Name: nameAR, Description: descAR},
	}
	for _, translation := range translations {
		if translation.Name == "" {
			continue
		}
		if err := e.commerce.UpsertProductTranslation(e.ctx, translation); err != nil {
			t.Fatalf("translate product %s/%s: %v", slug, translation.Locale, err)
		}
	}
	return product
}

func (e catalogEnv) assignCategories(t *testing.T, product commerce.Product, categories ...commerce.Category) {
	t.Helper()
	ids := make([]string, 0, len(categories))
	for _, category := range categories {
		ids = append(ids, category.ID)
	}
	if err := e.commerce.SetProductCategories(e.ctx, product.ID, ids); err != nil {
		t.Fatalf("set product categories: %v", err)
	}
}

func (e catalogEnv) media(t *testing.T, product commerce.Product, uri, alt string) {
	t.Helper()
	if _, err := e.db.Exec(e.ctx, `
		INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order)
		VALUES (gen_random_uuid(), $1, 'image', $2, $3, 0)
	`, product.ID, uri, alt); err != nil {
		t.Fatalf("insert media: %v", err)
	}
}

func (e catalogEnv) variantWithStock(t *testing.T, product commerce.Product, code, locationID string, onHand int64) commerce.SKU {
	t.Helper()
	variant, err := e.commerce.CreateVariant(e.ctx, product.ID, code, "active")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	sku, err := e.commerce.CreateSKU(e.ctx, variant.ID, "SKU-"+product.Slug+"-"+code, "", "active")
	if err != nil {
		t.Fatalf("create sku: %v", err)
	}
	if _, err := e.commerce.CreateInventorySnapshot(e.ctx, locationID, sku.ID, onHand); err != nil {
		t.Fatalf("create inventory snapshot: %v", err)
	}
	return sku
}

func (e catalogEnv) supplierOffer(t *testing.T, supplierID string, product commerce.Product, supplierMarketID, marketCode string, wholesaleMinor int64, available bool) string {
	t.Helper()
	supplierProduct, err := e.commerce.CreateSupplierProduct(e.ctx, supplierID, product.ID, "SUP-"+product.Slug, "active")
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	offer, err := e.commerce.CreateSupplierOffer(e.ctx, supplierID, supplierProduct.ID, supplierMarketID, marketCode, "active")
	if err != nil {
		t.Fatalf("create supplier offer: %v", err)
	}
	currency := "EGP"
	if marketCode == "SA" {
		currency = "SAR"
	}
	if _, err := e.commerce.SetSupplierOfferPrice(e.ctx, offer.ID, money.MustNew(wholesaleMinor, currency)); err != nil {
		t.Fatalf("set supplier offer price: %v", err)
	}
	if _, err := e.commerce.SetSupplierOfferAvailability(e.ctx, offer.ID, available, nil); err != nil {
		t.Fatalf("set supplier offer availability: %v", err)
	}
	return offer.ID
}

func (e catalogEnv) listing(t *testing.T, storeID string, product commerce.Product, offerID *string, marketCode, status string, priceMinor int64) commerce.SellerListing {
	t.Helper()
	listing, err := e.commerce.CreateSellerListing(e.ctx, storeID, product.ID, offerID, marketCode, status)
	if err != nil {
		t.Fatalf("create seller listing: %v", err)
	}
	currency := "EGP"
	if marketCode == "SA" {
		currency = "SAR"
	}
	if _, err := e.commerce.SetSellerListingPrice(e.ctx, listing.ID, money.MustNew(priceMinor, currency)); err != nil {
		t.Fatalf("set seller listing price: %v", err)
	}
	return listing
}

// scope resolves a tenant the way a public request does: from the trusted host.
func (e catalogEnv) scope(t *testing.T, domain string, locale i18n.Locale) CatalogScope {
	t.Helper()
	resolved, err := e.resolver.Resolve(e.ctx, domain)
	if err != nil {
		t.Fatalf("resolve %s: %v", domain, err)
	}
	scope, err := NewCatalogScope(resolved, locale)
	if err != nil {
		t.Fatalf("scope %s: %v", domain, err)
	}
	return scope
}

func productSlugs(page ProductPage) []string {
	slugs := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		slugs = append(slugs, item.Slug)
	}
	return slugs
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCatalogBootstrapExposesPublicStoreContext(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleArabic)

	bootstrap, err := env.catalog.Bootstrap(env.ctx, scope)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if bootstrap.StoreCode != "store-a" || bootstrap.StoreName != "Store A" {
		t.Fatalf("unexpected store identity: %+v", bootstrap)
	}
	if bootstrap.Domain != env.domainA {
		t.Fatalf("expected domain %q, got %q", env.domainA, bootstrap.Domain)
	}
	if bootstrap.Market != "EG" || bootstrap.Currency.Code != "EGP" || bootstrap.Currency.MinorUnit != 2 {
		t.Fatalf("unexpected market context: %+v", bootstrap)
	}
	if bootstrap.DefaultLocale != "ar" {
		t.Fatalf("expected default locale ar, got %q", bootstrap.DefaultLocale)
	}
	if len(bootstrap.SupportedLocales) != 2 {
		t.Fatalf("expected 2 supported locales, got %v", bootstrap.SupportedLocales)
	}
	if bootstrap.Settings["tagline"] != "Store A tagline" {
		t.Fatalf("expected public settings to be exposed, got %+v", bootstrap.Settings)
	}
	// Only the "public" settings object may surface; internal seller settings must not.
	if _, leaked := bootstrap.Settings["supplier_margin_target"]; leaked {
		t.Fatalf("internal store settings leaked: %+v", bootstrap.Settings)
	}
	if bootstrap.Theme != nil {
		t.Fatalf("expected no theme before installation, got %+v", bootstrap.Theme)
	}
}

func TestCatalogBootstrapExposesOnlyPublishedTheme(t *testing.T) {
	env := setupCatalogTest(t)

	themeRepo := themes.NewRepository(env.db.Pool)
	svc := themes.NewService(themeRepo, env.commerce, themes.Options{PreviewSecret: []byte("test-secret")})
	if err := svc.SeedBuiltInThemes(env.ctx); err != nil {
		t.Fatalf("seed themes: %v", err)
	}
	if _, err := svc.Install(env.ctx, env.storeA.SellerID, env.storeA.ID, themes.DefaultThemeKey, themes.DefaultThemeVersion); err != nil {
		t.Fatalf("install theme: %v", err)
	}
	if _, err := svc.UpdateDraftConfiguration(env.ctx, env.storeA.SellerID, env.storeA.ID, map[string]any{
		"logo": "https://cdn.matjero.test/draft-only.png",
	}); err != nil {
		t.Fatalf("update draft: %v", err)
	}

	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)
	bootstrap, err := env.catalog.Bootstrap(env.ctx, scope)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if bootstrap.Theme == nil {
		t.Fatal("expected an installed theme")
	}
	if bootstrap.Theme.Key != themes.DefaultThemeKey || bootstrap.Theme.Version != themes.DefaultThemeVersion {
		t.Fatalf("unexpected theme identity: %+v", bootstrap.Theme)
	}
	// The draft edit must not appear until it is published.
	encoded, err := json.Marshal(bootstrap)
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	if strings.Contains(string(encoded), "draft-only") {
		t.Fatalf("draft theme configuration leaked into public bootstrap: %s", encoded)
	}
	beforeRevision := bootstrap.Theme.ConfigurationRevision

	if _, err := svc.PublishConfiguration(env.ctx, env.storeA.SellerID, env.storeA.ID); err != nil {
		t.Fatalf("publish configuration: %v", err)
	}
	bootstrap, err = env.catalog.Bootstrap(env.ctx, scope)
	if err != nil {
		t.Fatalf("Bootstrap after publish: %v", err)
	}
	if bootstrap.Theme.ConfigurationRevision <= beforeRevision {
		t.Fatalf("expected published revision to advance past %d, got %d", beforeRevision, bootstrap.Theme.ConfigurationRevision)
	}
	if bootstrap.Theme.Configuration["logo"] != "https://cdn.matjero.test/draft-only.png" {
		t.Fatalf("expected published configuration to carry the published logo, got %+v", bootstrap.Theme.Configuration)
	}
}

func TestCatalogProductsAreStoreScoped(t *testing.T) {
	env := setupCatalogTest(t)

	pageA, err := env.catalog.Products(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), ProductQuery{})
	if err != nil {
		t.Fatalf("Products A: %v", err)
	}
	slugsA := productSlugs(pageA)
	if !contains(slugsA, "store-a-desk-lamp") || !contains(slugsA, "store-a-lamp-shade") {
		t.Fatalf("store A catalog missing its own products: %v", slugsA)
	}
	if contains(slugsA, "store-b-frying-pan") {
		t.Fatalf("store B product leaked into store A: %v", slugsA)
	}

	pageB, err := env.catalog.Products(env.ctx, env.scope(t, env.domainB, i18n.LocaleEnglish), ProductQuery{})
	if err != nil {
		t.Fatalf("Products B: %v", err)
	}
	slugsB := productSlugs(pageB)
	if !contains(slugsB, "store-b-frying-pan") {
		t.Fatalf("store B catalog missing its own product: %v", slugsB)
	}
	for _, slug := range slugsB {
		if strings.HasPrefix(slug, "store-a-") {
			t.Fatalf("store A product leaked into store B: %v", slugsB)
		}
	}
}

func TestCatalogExcludesNonPublicRecords(t *testing.T) {
	env := setupCatalogTest(t)
	page, err := env.catalog.Products(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), ProductQuery{})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	slugs := productSlugs(page)
	for _, hidden := range []string{
		"store-a-hidden",        // draft listing
		"store-a-draft-product", // draft product
		"store-a-unpriced",      // no current seller price
	} {
		if contains(slugs, hidden) {
			t.Fatalf("non-public record %q appeared in the public catalog: %v", hidden, slugs)
		}
	}
}

func TestCatalogEnforcesMarketIsolation(t *testing.T) {
	env := setupCatalogTest(t)
	page, err := env.catalog.Products(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), ProductQuery{})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if contains(productSlugs(page), "store-a-kettle-sa") {
		t.Fatalf("SA-market listing leaked into an EG store: %v", productSlugs(page))
	}
	if _, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "store-a-kettle-sa"); err != ErrCatalogNotFound {
		t.Fatalf("expected ErrCatalogNotFound for a cross-market slug, got %v", err)
	}
}

func TestCatalogPublicPriceIsSellerListingPrice(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	detail, err := env.catalog.ProductBySlug(env.ctx, scope, "store-a-desk-lamp")
	if err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}
	if detail.Price.AmountMinor != storeAPriceMinor || detail.Price.Currency != "EGP" {
		t.Fatalf("expected seller listing price %d EGP, got %+v", storeAPriceMinor, detail.Price)
	}

	page, err := env.catalog.Products(env.ctx, scope, ProductQuery{CategorySlug: "store-a-lamps"})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Price.AmountMinor != storeAPriceMinor {
		t.Fatalf("expected the listing price in browse rows, got %+v", page.Items)
	}

	// Store B sells the same supplier cost at a different price: the public price
	// follows the store's own listing, never the shared supplier cost.
	detailB, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainB, i18n.LocaleEnglish), "store-b-frying-pan")
	if err != nil {
		t.Fatalf("ProductBySlug B: %v", err)
	}
	if detailB.Price.AmountMinor != storeBPriceMinor {
		t.Fatalf("expected store B price %d, got %+v", storeBPriceMinor, detailB.Price)
	}
}

func TestCatalogLocalizedProjections(t *testing.T) {
	env := setupCatalogTest(t)

	en, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "store-a-desk-lamp")
	if err != nil {
		t.Fatalf("ProductBySlug en: %v", err)
	}
	if en.Name != "Desk Lamp" || en.Description != "A bright desk lamp" {
		t.Fatalf("unexpected English projection: %+v", en)
	}
	if len(en.Categories) != 1 || en.Categories[0].Name != "Lamps" {
		t.Fatalf("unexpected English category projection: %+v", en.Categories)
	}

	ar, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleArabic), "store-a-desk-lamp")
	if err != nil {
		t.Fatalf("ProductBySlug ar: %v", err)
	}
	if ar.Name != "مصباح مكتبي" || ar.Description != "مصباح مكتبي ساطع" {
		t.Fatalf("unexpected Arabic projection: %+v", ar)
	}
	if len(ar.Categories) != 1 || ar.Categories[0].Name != "المصابيح" {
		t.Fatalf("unexpected Arabic category projection: %+v", ar.Categories)
	}
	if ar.Slug != en.Slug {
		t.Fatalf("slug must be locale-stable: %q vs %q", ar.Slug, en.Slug)
	}
}

func TestCatalogCategoriesAreStoreScoped(t *testing.T) {
	env := setupCatalogTest(t)

	nodes, err := env.catalog.Categories(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish))
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	slugs := make([]string, 0, len(nodes))
	byslug := map[string]CategoryNode{}
	for _, node := range nodes {
		slugs = append(slugs, node.Slug)
		byslug[node.Slug] = node
	}
	if !contains(slugs, "store-a-lighting") || !contains(slugs, "store-a-lamps") {
		t.Fatalf("store A categories missing: %v", slugs)
	}
	if contains(slugs, "store-b-kitchen") {
		t.Fatalf("store B category leaked into store A: %v", slugs)
	}
	// The child category is returned with its parent so a client can rebuild the tree.
	if byslug["store-a-lamps"].ParentSlug != "store-a-lighting" {
		t.Fatalf("expected a parent slug on the child category, got %+v", byslug["store-a-lamps"])
	}
	if byslug["store-a-lamps"].ProductCount != 1 {
		t.Fatalf("expected one product in the child category, got %d", byslug["store-a-lamps"].ProductCount)
	}
}

func TestCatalogCategorySlugCannotCrossStores(t *testing.T) {
	env := setupCatalogTest(t)

	if _, err := env.catalog.CategoryBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "store-b-kitchen"); err != ErrCatalogNotFound {
		t.Fatalf("expected store B category to be not found for store A, got %v", err)
	}
	if _, err := env.catalog.CategoryBySlug(env.ctx, env.scope(t, env.domainB, i18n.LocaleEnglish), "store-a-lighting"); err != ErrCatalogNotFound {
		t.Fatalf("expected store A category to be not found for store B, got %v", err)
	}

	node, err := env.catalog.CategoryBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleArabic), "store-a-lamps")
	if err != nil {
		t.Fatalf("CategoryBySlug: %v", err)
	}
	if node.Name != "المصابيح" {
		t.Fatalf("expected the Arabic category name, got %q", node.Name)
	}
}

func TestCatalogProductSlugCannotCrossStores(t *testing.T) {
	env := setupCatalogTest(t)

	if _, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "store-b-frying-pan"); err != ErrCatalogNotFound {
		t.Fatalf("expected store B product to be not found for store A, got %v", err)
	}
	if _, err := env.catalog.ProductBySlug(env.ctx, env.scope(t, env.domainB, i18n.LocaleEnglish), "store-a-desk-lamp"); err != ErrCatalogNotFound {
		t.Fatalf("expected store A product to be not found for store B, got %v", err)
	}
}

func TestCatalogFiltersSortAndPagination(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	min := int64(storeACheapPriceMinor + 1)
	filtered, err := env.catalog.Products(env.ctx, scope, ProductQuery{MinPriceMinor: &min})
	if err != nil {
		t.Fatalf("Products (min price): %v", err)
	}
	if productSlugsEqual(filtered, "store-a-desk-lamp") == false {
		t.Fatalf("min price filter returned %v", productSlugs(filtered))
	}

	max := int64(storeACheapPriceMinor)
	filtered, err = env.catalog.Products(env.ctx, scope, ProductQuery{MaxPriceMinor: &max})
	if err != nil {
		t.Fatalf("Products (max price): %v", err)
	}
	if productSlugsEqual(filtered, "store-a-lamp-shade") == false {
		t.Fatalf("max price filter returned %v", productSlugs(filtered))
	}

	inStock, err := env.catalog.Products(env.ctx, scope, ProductQuery{Availability: AvailabilityInStock})
	if err != nil {
		t.Fatalf("Products (in stock): %v", err)
	}
	if productSlugsEqual(inStock, "store-a-desk-lamp") == false {
		t.Fatalf("availability filter returned %v", productSlugs(inStock))
	}
	outOfStock, err := env.catalog.Products(env.ctx, scope, ProductQuery{Availability: AvailabilityOutOfStock})
	if err != nil {
		t.Fatalf("Products (out of stock): %v", err)
	}
	if productSlugsEqual(outOfStock, "store-a-lamp-shade") == false {
		t.Fatalf("out-of-stock filter returned %v", productSlugs(outOfStock))
	}

	ascending, err := env.catalog.Products(env.ctx, scope, ProductQuery{Sort: SortPriceAsc})
	if err != nil {
		t.Fatalf("Products (price asc): %v", err)
	}
	if productSlugsEqual(ascending, "store-a-lamp-shade", "store-a-desk-lamp") == false {
		t.Fatalf("price ascending sort returned %v", productSlugs(ascending))
	}
	descending, err := env.catalog.Products(env.ctx, scope, ProductQuery{Sort: SortPriceDesc})
	if err != nil {
		t.Fatalf("Products (price desc): %v", err)
	}
	if productSlugsEqual(descending, "store-a-desk-lamp", "store-a-lamp-shade") == false {
		t.Fatalf("price descending sort returned %v", productSlugs(descending))
	}

	// Paging over a stable sort must partition the result set with no duplicates
	// and no gaps.
	firstPage, err := env.catalog.Products(env.ctx, scope, ProductQuery{Sort: SortPriceAsc, Page: Page{Limit: 1}})
	if err != nil {
		t.Fatalf("Products (page 1): %v", err)
	}
	secondPage, err := env.catalog.Products(env.ctx, scope, ProductQuery{Sort: SortPriceAsc, Page: Page{Limit: 1, Offset: 1}})
	if err != nil {
		t.Fatalf("Products (page 2): %v", err)
	}
	if firstPage.Total != 2 || secondPage.Total != 2 {
		t.Fatalf("expected a total of 2 on both pages, got %d and %d", firstPage.Total, secondPage.Total)
	}
	if productSlugsEqual(firstPage, "store-a-lamp-shade") == false || productSlugsEqual(secondPage, "store-a-desk-lamp") == false {
		t.Fatalf("unstable pagination: %v then %v", productSlugs(firstPage), productSlugs(secondPage))
	}

	if _, err := env.catalog.Products(env.ctx, scope, ProductQuery{Page: Page{Limit: 1000000}}); err == nil {
		t.Fatal("expected an unbounded page limit to be rejected")
	}
	if _, err := env.catalog.Products(env.ctx, scope, ProductQuery{Sort: "random"}); err == nil {
		t.Fatal("expected an unknown sort to be rejected")
	}
}

func TestCatalogSearchIsStoreScoped(t *testing.T) {
	env := setupCatalogTest(t)

	results, err := env.catalog.Search(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "desk", ProductQuery{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if productSlugsEqual(results, "store-a-desk-lamp") == false {
		t.Fatalf("English search returned %v", productSlugs(results))
	}

	arabic, err := env.catalog.Search(env.ctx, env.scope(t, env.domainA, i18n.LocaleArabic), "مصباح مكتبي", ProductQuery{})
	if err != nil {
		t.Fatalf("Search (ar): %v", err)
	}
	if productSlugsEqual(arabic, "store-a-desk-lamp") == false {
		t.Fatalf("Arabic search returned %v", productSlugs(arabic))
	}

	// A term that only matches store B content must not resolve for store A.
	crossStore, err := env.catalog.Search(env.ctx, env.scope(t, env.domainA, i18n.LocaleEnglish), "frying", ProductQuery{})
	if err != nil {
		t.Fatalf("Search (cross store): %v", err)
	}
	if len(crossStore.Items) != 0 {
		t.Fatalf("store B content matched a store A search: %v", productSlugs(crossStore))
	}
}

func TestCatalogProductDetailAvailabilityAndVariants(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	inStock, err := env.catalog.ProductBySlug(env.ctx, scope, "store-a-desk-lamp")
	if err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}
	if inStock.Availability != AvailabilityInStock {
		t.Fatalf("expected in_stock, got %q", inStock.Availability)
	}
	if len(inStock.Images) != 1 || inStock.Images[0].AltText != "Desk Lamp" {
		t.Fatalf("unexpected images: %+v", inStock.Images)
	}
	if len(inStock.Variants) != 1 || len(inStock.Variants[0].SKUs) != 1 {
		t.Fatalf("unexpected variants: %+v", inStock.Variants)
	}
	if inStock.Variants[0].Availability != AvailabilityInStock || inStock.Variants[0].SKUs[0].Availability != AvailabilityInStock {
		t.Fatalf("unexpected variant availability: %+v", inStock.Variants)
	}

	outOfStock, err := env.catalog.ProductBySlug(env.ctx, scope, "store-a-lamp-shade")
	if err != nil {
		t.Fatalf("ProductBySlug (out of stock): %v", err)
	}
	if outOfStock.Availability != AvailabilityOutOfStock {
		t.Fatalf("expected out_of_stock, got %q", outOfStock.Availability)
	}
}

func TestCatalogRejectsUnknownSlugs(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	if _, err := env.catalog.ProductBySlug(env.ctx, scope, "does-not-exist"); err != ErrCatalogNotFound {
		t.Fatalf("expected ErrCatalogNotFound, got %v", err)
	}
	if _, err := env.catalog.CategoryBySlug(env.ctx, scope, "does-not-exist"); err != ErrCatalogNotFound {
		t.Fatalf("expected ErrCatalogNotFound, got %v", err)
	}
	if _, err := env.catalog.ProductBySlug(env.ctx, scope, "  "); err == nil {
		t.Fatal("expected a blank slug to be rejected")
	}
}

func productSlugsEqual(page ProductPage, want ...string) bool {
	got := productSlugs(page)
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
