package storefront

import (
	"encoding/json"
	"strings"
	"testing"

	"core/packages/i18n"
)

// forbiddenPublicTerms are field names and vocabulary that must never appear in a
// serialized public catalog payload. They cover supplier identity and contact
// data, supplier economics, internal fees, fulfillment internals, and inventory
// quantities.
var forbiddenPublicTerms = []string{
	"supplier",
	"supplier_id",
	"supplier_code",
	"supplier_email",
	"supplier_phone",
	"contact_email",
	"wholesale",
	"cost",
	"margin",
	"fee",
	"payout",
	"fulfillment",
	"fulfillment_location",
	"fulfillment_location_id",
	"on_hand_qty",
	"reserved_qty",
	"available_qty",
	"inventory",
	"internal",
	"seller_id",
	"store_id",
	"listing_id",
	"seller_listing_id",
	"supplier_offer_id",
	"offer_id",
	"minimum_order_quantity",
	"available_qty",
	"reservation",
	"reservation_id",
	"actor",
	"actor_id",
	"integration",
	"moderation",
	"draft",
	"verification_token",
}

// assertNoPrivateData serializes a public payload and fails on any forbidden term
// or on the supplier wholesale amount. Serializing (rather than reviewing struct
// definitions) catches leakage through embedded structs and free-form JSON such
// as theme or settings documents.
func assertNoPrivateData(t *testing.T, label string, payload any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("%s: marshal: %v", label, err)
	}
	lower := strings.ToLower(string(encoded))
	for _, term := range forbiddenPublicTerms {
		if strings.Contains(lower, term) {
			t.Fatalf("%s: forbidden term %q found in public payload: %s", label, term, encoded)
		}
	}
	// The supplier wholesale cost is 100.00 while the public price is 150.00.
	if strings.Contains(lower, "10000") {
		t.Fatalf("%s: supplier wholesale amount leaked into public payload: %s", label, encoded)
	}
}

func TestPublicCatalogPayloadsCarryNoPrivateData(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	bootstrap, err := env.catalog.Bootstrap(env.ctx, scope)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	assertNoPrivateData(t, "bootstrap", bootstrap)

	categories, err := env.catalog.Categories(env.ctx, scope)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	assertNoPrivateData(t, "categories", categories)

	page, err := env.catalog.Products(env.ctx, scope, ProductQuery{})
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	assertNoPrivateData(t, "products", page)

	results, err := env.catalog.Search(env.ctx, scope, "lamp", ProductQuery{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertNoPrivateData(t, "search", results)

	detail, err := env.catalog.ProductBySlug(env.ctx, scope, "store-a-desk-lamp")
	if err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}
	assertNoPrivateData(t, "product detail", detail)

	// The public price must be present even though the supplier cost is absent.
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if !strings.Contains(string(encoded), "15000") {
		t.Fatalf("expected the seller listing price in the public payload: %s", encoded)
	}
}

// TestPublicCatalogReadsDoNotMutateState proves browse is read-only: inventory,
// reservations, listing state, product state, and supplier offer state are
// unchanged after every public operation.
func TestPublicCatalogReadsDoNotMutateState(t *testing.T) {
	env := setupCatalogTest(t)
	scope := env.scope(t, env.domainA, i18n.LocaleEnglish)

	type stateSnapshot struct {
		inventory    string
		reservations int64
		listings     string
		products     string
		offers       string
	}

	snapshot := func() stateSnapshot {
		t.Helper()
		var s stateSnapshot
		if err := env.db.QueryRow(env.ctx, `
			SELECT COALESCE(string_agg(id::text || ':' || on_hand_qty || ':' || reserved_qty || ':' || version, ',' ORDER BY id), '')
			FROM inventory_snapshots
		`).Scan(&s.inventory); err != nil {
			t.Fatalf("snapshot inventory: %v", err)
		}
		if err := env.db.QueryRow(env.ctx, `SELECT COUNT(*) FROM inventory_reservations`).Scan(&s.reservations); err != nil {
			t.Fatalf("snapshot reservations: %v", err)
		}
		if err := env.db.QueryRow(env.ctx, `
			SELECT COALESCE(string_agg(id::text || ':' || status || ':' || updated_at, ',' ORDER BY id), '')
			FROM seller_listings
		`).Scan(&s.listings); err != nil {
			t.Fatalf("snapshot listings: %v", err)
		}
		if err := env.db.QueryRow(env.ctx, `
			SELECT COALESCE(string_agg(id::text || ':' || status || ':' || updated_at, ',' ORDER BY id), '')
			FROM products
		`).Scan(&s.products); err != nil {
			t.Fatalf("snapshot products: %v", err)
		}
		if err := env.db.QueryRow(env.ctx, `
			SELECT COALESCE(string_agg(id::text || ':' || status || ':' || updated_at, ',' ORDER BY id), '')
			FROM supplier_offers
		`).Scan(&s.offers); err != nil {
			t.Fatalf("snapshot offers: %v", err)
		}
		return s
	}

	before := snapshot()

	if _, err := env.catalog.Bootstrap(env.ctx, scope); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := env.catalog.Categories(env.ctx, scope); err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if _, err := env.catalog.CategoryBySlug(env.ctx, scope, "store-a-lamps"); err != nil {
		t.Fatalf("CategoryBySlug: %v", err)
	}
	if _, err := env.catalog.Products(env.ctx, scope, ProductQuery{Availability: AvailabilityInStock}); err != nil {
		t.Fatalf("Products: %v", err)
	}
	if _, err := env.catalog.Search(env.ctx, scope, "lamp", ProductQuery{}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := env.catalog.ProductBySlug(env.ctx, scope, "store-a-desk-lamp"); err != nil {
		t.Fatalf("ProductBySlug: %v", err)
	}

	if after := snapshot(); after != before {
		t.Fatalf("public catalog reads mutated state:\nbefore: %+v\nafter:  %+v", before, after)
	}
}
