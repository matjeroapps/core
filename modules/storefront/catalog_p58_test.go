package storefront

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"github.com/matjeroapps/core/internal/testdb"
	"strings"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/i18n"
	"github.com/matjeroapps/core/packages/money"
)

// p58StorefrontEnv seeds a single EG store with one seller-owned active
// product, ready for canonical-listing and media-ordering assertions.
type p58StorefrontEnv struct {
	ctx      context.Context
	db       *database.Pool
	commerce commerce.Repository
	catalog  CatalogRepository
	resolver StoreResolver

	store   commerce.Store
	product commerce.Product
}

func setupP58Storefront(t *testing.T) p58StorefrontEnv {
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
		content, err := os.ReadFile(filepath.Join("..", "..", "migrations", m+".up.sql"))
		if err != nil {
			t.Fatalf("read migration %s: %v", m, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", m, err)
		}
	}

	repo := commerce.NewRepository(db.Pool)
	env := p58StorefrontEnv{
		ctx:      ctx,
		db:       db,
		commerce: repo,
		catalog:  NewCatalogRepository(db.Pool),
		resolver: NewStoreResolver(repo),
	}

	seller, err := repo.CreateSeller(ctx, "p58-seller", "P5.8 Seller", "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p58-store", "P5.8 Store", "active", nil, "p58-store.matjero.test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	env.store = store

	product, err := repo.CreateProduct(ctx, "p58-product", "active")
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	for _, tr := range []commerce.ProductTranslation{
		{ProductID: product.ID, Locale: "en", Name: "P5.8 Product", Description: "English"},
		{ProductID: product.ID, Locale: "ar", Name: "منتج", Description: "عربي"},
	} {
		if err := repo.UpsertProductTranslation(ctx, tr); err != nil {
			t.Fatalf("translate product: %v", err)
		}
	}
	env.product = product

	// Store-owned, active, EG-market location with stock so the canonical
	// listing is sellable.
	var locID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES (gen_random_uuid(), $1, 'EG', 'p58-main', 'Main', 'warehouse', 'active')
		RETURNING id
	`, store.ID).Scan(&locID); err != nil {
		t.Fatalf("insert location: %v", err)
	}
	variant, err := repo.CreateVariant(ctx, product.ID, "default", "active")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	sku, err := repo.CreateSKU(ctx, variant.ID, "SKU-P58-DEFAULT", "", "active")
	if err != nil {
		t.Fatalf("create sku: %v", err)
	}
	if _, err := repo.CreateInventorySnapshot(ctx, locID, sku.ID, 25); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	return env
}

// listing creates an active seller-owned listing with a current price and an
// optional presentation, returning it.
func (e p58StorefrontEnv) listing(t *testing.T, priceMinor int64, behavior string, sections []commerce.ProductPageSection) commerce.SellerListing {
	t.Helper()
	listing, err := e.commerce.CreateSellerListing(e.ctx, e.store.ID, e.product.ID, nil, "EG", "active")
	if err != nil {
		t.Fatalf("create listing: %v", err)
	}
	if _, err := e.commerce.SetSellerListingPrice(e.ctx, listing.ID, money.MustNew(priceMinor, "EGP")); err != nil {
		t.Fatalf("set price: %v", err)
	}
	if sections != nil {
		if _, err := e.commerce.UpsertSellerListingPresentation(e.ctx, commerce.SellerListingPresentation{
			SellerListingID:  listing.ID,
			SchemaVersion:    1,
			PurchaseBehavior: behavior,
			Sections:         sections,
		}); err != nil {
			t.Fatalf("upsert presentation: %v", err)
		}
	}
	return listing
}

func (e p58StorefrontEnv) media(t *testing.T, uri string, sortOrder int, isPrimary bool) {
	t.Helper()
	if _, err := e.db.Exec(e.ctx, `
		INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, is_primary)
		VALUES (gen_random_uuid(), $1, 'image/webp', $2, $2, $3, $4)
	`, e.product.ID, uri, sortOrder, isPrimary); err != nil {
		t.Fatalf("insert media: %v", err)
	}
}

func (e p58StorefrontEnv) scope(t *testing.T) CatalogScope {
	return e.scopeForLocale(t, "en")
}

func (e p58StorefrontEnv) scopeForLocale(t *testing.T, locale string) CatalogScope {
	t.Helper()
	resolved, err := e.resolver.Resolve(e.ctx, "p58-store.matjero.test")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	scope, err := NewCatalogScope(resolved, i18n.Locale(locale))
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	return scope
}

// TestPrimaryImageControlsPublicOrdering verifies the deterministic
// primary-first media ordering on every public product media projection.
func TestPrimaryImageControlsPublicOrdering(t *testing.T) {
	e := setupP58Storefront(t)
	e.listing(t, 10000, "add_to_cart", nil)
	scope := e.scope(t)

	// Image A: sort order 1, not primary. Image B: sort order 5, primary.
	e.media(t, "https://cdn.test/a.webp", 1, false)
	e.media(t, "https://cdn.test/b.webp", 5, true)

	detail, err := e.catalog.ProductBySlug(e.ctx, scope, "p58-product")
	if err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}
	if len(detail.Images) != 2 || detail.Images[0].URI != "https://cdn.test/b.webp" {
		t.Fatalf("detail first image must be the primary B, got %+v", detail.Images)
	}
	page, err := e.catalog.Products(e.ctx, scope, ProductQuery{Page: Page{Limit: 10}})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Image == nil || page.Items[0].Image.URI != "https://cdn.test/b.webp" {
		t.Fatalf("list thumbnail must be the primary B, got %+v", page.Items)
	}

	// Flip the primary to A: ordering must follow.
	if _, err := e.db.Exec(e.ctx, `
		UPDATE media_metadata SET is_primary = false WHERE product_id = $1
	`, e.product.ID); err != nil {
		t.Fatalf("clear primary: %v", err)
	}
	if _, err := e.db.Exec(e.ctx, `
		UPDATE media_metadata SET is_primary = true WHERE product_id = $1 AND uri = 'https://cdn.test/a.webp'
	`, e.product.ID); err != nil {
		t.Fatalf("flip primary: %v", err)
	}

	detail, err = e.catalog.ProductBySlug(e.ctx, scope, "p58-product")
	if err != nil {
		t.Fatalf("ProductBySlug after flip: %v", err)
	}
	if len(detail.Images) != 2 || detail.Images[0].URI != "https://cdn.test/a.webp" {
		t.Fatalf("detail first image must now be primary A, got %+v", detail.Images)
	}
	page, err = e.catalog.Products(e.ctx, scope, ProductQuery{Page: Page{Limit: 10}})
	if err != nil {
		t.Fatalf("Products after flip: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Image == nil || page.Items[0].Image.URI != "https://cdn.test/a.webp" {
		t.Fatalf("list thumbnail must now be primary A, got %+v", page.Items)
	}
}

// TestCanonicalListingContinuity proves price, purchase behavior and
// presentation all come from the one canonical listing — zero fields may leak
// from a superseded listing of the same product.
func TestCanonicalListingContinuity(t *testing.T) {
	e := setupP58Storefront(t)
	scope := e.scope(t)

	// Listing A: older, price 100, add_to_cart, presentation A.
	listingA := e.listing(t, 10000, "add_to_cart", []commerce.ProductPageSection{{
		ID: "sec-a", Type: "description", Enabled: true, SortOrder: 1,
		Content: map[string]any{"en": map[string]any{"heading": "From listing A", "body": "A body"}},
	}})

	// Listing B: newest, canonical, price 120, buy_now, presentation B.
	listingB := e.listing(t, 12000, "buy_now", []commerce.ProductPageSection{{
		ID: "sec-b", Type: "description", Enabled: true, SortOrder: 1,
		Content: map[string]any{"en": map[string]any{"heading": "From listing B", "body": "B body"}},
	}})
	_ = listingA

	detail, err := e.catalog.ProductBySlug(e.ctx, scope, "p58-product")
	if err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}

	if detail.Price.AmountMinor != 12000 {
		t.Fatalf("price must come from canonical listing B (12000), got %d", detail.Price.AmountMinor)
	}
	if detail.PurchaseBehavior != "buy_now" {
		t.Fatalf("purchase behavior must come from canonical listing B (buy_now), got %s", detail.PurchaseBehavior)
	}
	if len(detail.Sections) != 1 {
		t.Fatalf("expected exactly the canonical listing's sections, got %d", len(detail.Sections))
	}
	sec, _ := detail.Sections[0].(map[string]any)
	content, _ := sec["content"].(map[string]any)
	heading, _ := content["heading"].(string)
	if heading != "From listing B" {
		t.Fatalf("sections must come from canonical listing B, got %q", heading)
	}
	_ = listingB
}

// TestPublicSectionProjectionAllSixTypes seeds all six structured section
// types and asserts the exact locale-projected public contract for EN and AR,
// including image_text resolution to public image data and the absence of any
// internal handles.
func TestPublicSectionProjectionAllSixTypes(t *testing.T) {
	e := setupP58Storefront(t)
	listing := e.listing(t, 15000, "add_to_cart", nil)

	// Deterministic media row the image_text section can reference.
	const mediaID = "11111111-1111-1111-1111-111111111111"
	if _, err := e.db.Exec(e.ctx, "INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, is_primary) VALUES ($1, $2, 'image/png', 'https://cdn.test/quality.png', 'Quality shot', 1, true)", mediaID, e.product.ID); err != nil {
		t.Fatalf("insert media: %v", err)
	}

	presentation := commerce.SellerListingPresentation{
		SellerListingID:  listing.ID,
		SchemaVersion:    1,
		PurchaseBehavior: "buy_now",
		Sections: []commerce.ProductPageSection{
			{
				ID: "s1", Type: "description", Enabled: true, SortOrder: 10,
				Content: map[string]any{
					"en": map[string]any{"heading": "About", "body": "English body"},
					"ar": map[string]any{"heading": "\u0646\u0628\u0630\u0629", "body": "\u0646\u0635 \u0639\u0631\u0628\u064a"},
				},
			},
			{
				ID: "s2", Type: "highlights", Enabled: true, SortOrder: 20,
				Content: map[string]any{
					"en": map[string]any{"title": "Highlights", "items": []any{"One", "Two"}},
					"ar": map[string]any{"title": "\u0627\u0644\u0645\u0645\u064a\u0632\u0627\u062a", "items": []any{"\u0648\u0627\u062d\u062f"}},
				},
			},
			{
				ID: "s3", Type: "image_text", Enabled: true, SortOrder: 30,
				Content: map[string]any{
					"media_id": mediaID,
					"layout":   "right",
					"en":       map[string]any{"heading": "Quality", "body": "Crafted"},
					"ar":       map[string]any{"heading": "\u0627\u0644\u062c\u0648\u062f\u0629", "body": "\u0628\u062c\u0648\u062f\u0629 \u0639\u0627\u0644\u064a\u0629"},
				},
			},
			{
				ID: "s4", Type: "specifications", Enabled: true, SortOrder: 40,
				Content: map[string]any{
					"en": map[string]any{"items": []any{map[string]any{"key": "Material", "value": "Cotton"}}},
					"ar": map[string]any{"items": []any{map[string]any{"key": "\u0627\u0644\u062e\u0627\u0645\u0629", "value": "\u0642\u0637\u0646"}}},
				},
			},
			{
				ID: "s5", Type: "faq", Enabled: true, SortOrder: 50,
				Content: map[string]any{
					"en": map[string]any{"items": []any{map[string]any{"question": "Ships fast?", "answer": "Yes."}}},
					"ar": map[string]any{"items": []any{map[string]any{"question": "\u0634\u062d\u0646 \u0633\u0631\u064a\u0639\u061f", "answer": "\u0646\u0639\u0645."}}},
				},
			},
			{
				ID: "s6", Type: "final_cta", Enabled: true, SortOrder: 60,
				Content: map[string]any{
					"action": "buy_now",
					"en":     map[string]any{"title": "Ready to order?", "body": "Order now"},
					"ar":     map[string]any{"title": "\u062c\u0627\u0647\u0632 \u0644\u0644\u0637\u0644\u0628\u061f", "body": "\u0627\u0637\u0644\u0628 \u0627\u0644\u0622\u0646"},
				},
			},
			{
				// Disabled sections must not reach the public payload.
				ID: "s7", Type: "description", Enabled: false, SortOrder: 70,
				Content: map[string]any{"en": map[string]any{"heading": "Hidden"}},
			},
		},
	}
	if _, err := e.commerce.UpsertSellerListingPresentation(e.ctx, presentation); err != nil {
		t.Fatalf("upsert presentation: %v", err)
	}

	assertProjection := func(t *testing.T, locale string, heading, highlightsTitle, imageHeading, materialKey, question, ctaTitle string) {
		t.Helper()
		scope := e.scopeForLocale(t, locale)
		detail, err := e.catalog.ProductBySlug(e.ctx, scope, "p58-product")
		if err != nil {
			t.Fatalf("ProductBySlug (%s): %v", locale, err)
		}
		raw, err := json.Marshal(detail.Sections)
		if err != nil {
			t.Fatalf("marshal sections: %v", err)
		}
		public := string(raw)

		if len(detail.Sections) != 6 {
			t.Fatalf("(%s) expected 6 public sections (disabled excluded), got %d: %s", locale, len(detail.Sections), public)
		}
		// Deterministic ordering by sort_order.
		for i, want := range []string{"s1", "s2", "s3", "s4", "s5", "s6"} {
			sec, _ := detail.Sections[i].(map[string]any)
			if sec["id"] != want {
				t.Fatalf("(%s) section %d = %v, want %s", locale, i, sec["id"], want)
			}
		}
		for _, needle := range []string{heading, highlightsTitle, imageHeading, materialKey, question, ctaTitle} {
			if !strings.Contains(public, needle) {
				t.Fatalf("(%s) public sections missing %q: %s", locale, needle, public)
			}
		}
		// image_text resolved to public image data.
		for _, needle := range []string{"https://cdn.test/quality.png", "Quality shot", "\"layout\":\"right\""} {
			if !strings.Contains(public, needle) {
				t.Fatalf("(%s) image_text projection missing %q: %s", locale, needle, public)
			}
		}
		// Internal handles and raw locale maps must never leak.
		for _, leak := range []string{mediaID, "storage_key", "\"en\":{", "\"ar\":{"} {
			if strings.Contains(public, leak) {
				t.Fatalf("(%s) public sections leak internal data %q: %s", locale, leak, public)
			}
		}
		// final_cta projects the approved global action.
		if !strings.Contains(public, "\"action\":\"buy_now\"") {
			t.Fatalf("(%s) final_cta action not projected: %s", locale, public)
		}
	}

	assertProjection(t, "en", "About", "Highlights", "Quality", "Material", "Ships fast?", "Ready to order?")
	assertProjection(t, "ar", "\u0646\u0628\u0630\u0629", "\u0627\u0644\u0645\u0645\u064a\u0632\u0627\u062a", "\u0627\u0644\u062c\u0648\u062f\u0629", "\u0627\u0644\u062e\u0627\u0645\u0629", "\u0634\u062d\u0646 \u0633\u0631\u064a\u0639\u061f", "\u062c\u0627\u0647\u0632 \u0644\u0644\u0637\u0644\u0628\u061f")
}
