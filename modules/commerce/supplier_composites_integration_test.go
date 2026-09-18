package commerce

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testdb"
	"core/packages/database"
	"core/packages/money"
)

// Supplier onboarding writes several rows per logical creation. When those rows
// are written by separate calls, a failure partway through commits the earlier
// ones and leaves an invalid record behind: a product with no supplier binding,
// or an offer with no price. These tests assert the atomic variants leave no
// trace when any step fails.

func setupCompositeTest(t *testing.T) (context.Context, Repository, *database.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	for _, name := range []string{
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
	} {
		applyCompositeMigration(t, db, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	return context.Background(), NewRepository(db.Pool), db
}

func applyCompositeMigration(t *testing.T, db *database.Pool, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if _, err := db.Exec(context.Background(), string(body)); err != nil {
		t.Fatalf("apply migration %s: %v", path, err)
	}
}

func countRows(t *testing.T, db *database.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func seedSupplier(t *testing.T, ctx context.Context, repo Repository) (Supplier, SupplierMarket) {
	t.Helper()
	supplier, err := repo.CreateSupplier(ctx, "supplier-composite", "Composite Supplier", "active", nil)
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	market, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market: %v", err)
	}
	return supplier, market
}

// --- product composite ---

func TestCreateSupplierProductAtomicallyWritesEveryRow(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, _ := seedSupplier(t, ctx, repo)

	category, err := repo.CreateCategory(ctx, "composite-category", nil, "active")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	product, supplierProduct, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, ProductDraft{
		Slug:         "composite-product",
		Status:       "active",
		SupplierCode: "COMP-1",
		Translations: []ProductTranslation{
			{Locale: "en", Name: "Composite", Description: "English"},
			{Locale: "ar", Name: "مركب", Description: "عربي"},
		},
		CategoryIDs: []string{category.ID},
	})
	if err != nil {
		t.Fatalf("create supplier product atomically: %v", err)
	}
	if product.ID == "" || supplierProduct.ID == "" {
		t.Fatal("expected both the product and the supplier binding to be returned")
	}
	if supplierProduct.ProductID != product.ID {
		t.Errorf("binding product id = %q, want %q", supplierProduct.ProductID, product.ID)
	}

	if got := countRows(t, db, `SELECT count(*) FROM products WHERE slug = $1`, "composite-product"); got != 1 {
		t.Errorf("products = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM product_translations WHERE product_id = $1`, product.ID); got != 2 {
		t.Errorf("translations = %d, want 2", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_products WHERE product_id = $1`, product.ID); got != 1 {
		t.Errorf("supplier bindings = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM product_categories WHERE product_id = $1`, product.ID); got != 1 {
		t.Errorf("category assignments = %d, want 1", got)
	}
}

// An unknown category is the realistic mid-operation failure: the product,
// translations and supplier binding all succeed first. Nothing may survive.
func TestCreateSupplierProductAtomicallyRollsBackOnUnknownCategory(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, _ := seedSupplier(t, ctx, repo)

	before := countRows(t, db, `SELECT count(*) FROM products`)

	_, _, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, ProductDraft{
		Slug:         "orphan-probe",
		Status:       "active",
		SupplierCode: "ORPHAN-1",
		Translations: []ProductTranslation{{Locale: "en", Name: "Orphan Probe"}},
		CategoryIDs:  []string{"00000000-0000-0000-0000-000000000000"},
	})
	if err == nil {
		t.Fatal("expected an unknown category to fail the operation")
	}

	if got := countRows(t, db, `SELECT count(*) FROM products WHERE slug = $1`, "orphan-probe"); got != 0 {
		t.Errorf("orphaned products = %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_products WHERE supplier_code = $1`, "ORPHAN-1"); got != 0 {
		t.Errorf("orphaned supplier bindings = %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM products`); got != before {
		t.Errorf("product count = %d, want the pre-operation count %d", got, before)
	}
}

func TestCreateSupplierProductAtomicallyRollsBackOnDuplicateSlug(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, _ := seedSupplier(t, ctx, repo)

	draft := ProductDraft{
		Slug:         "duplicate-probe",
		Status:       "active",
		SupplierCode: "DUP-1",
		Translations: []ProductTranslation{{Locale: "en", Name: "Duplicate Probe"}},
	}
	if _, _, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, draft); err != nil {
		t.Fatalf("first create: %v", err)
	}

	draft.SupplierCode = "DUP-2"
	if _, _, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, draft); err == nil {
		t.Fatal("expected a duplicate slug to fail")
	}

	if got := countRows(t, db, `SELECT count(*) FROM products WHERE slug = $1`, "duplicate-probe"); got != 1 {
		t.Errorf("products = %d, want 1 (the failed retry must leave nothing)", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_products WHERE supplier_code = $1`, "DUP-2"); got != 0 {
		t.Errorf("orphaned binding from the failed retry = %d, want 0", got)
	}
}

func TestCreateSupplierProductAtomicallyRejectsInvalidDraft(t *testing.T) {
	ctx, repo, _ := setupCompositeTest(t)
	supplier, _ := seedSupplier(t, ctx, repo)

	cases := map[string]ProductDraft{
		"missing slug":          {Status: "active", SupplierCode: "X"},
		"missing status":        {Slug: "s", SupplierCode: "X"},
		"missing supplier code": {Slug: "s", Status: "active"},
		"translation without locale": {
			Slug: "s", Status: "active", SupplierCode: "X",
			Translations: []ProductTranslation{{Name: "No locale"}},
		},
		"translation without name": {
			Slug: "s", Status: "active", SupplierCode: "X",
			Translations: []ProductTranslation{{Locale: "en"}},
		},
	}

	for name, draft := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, draft); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// --- offer composite ---

func TestCreateSupplierOfferAtomicallyWritesPriceAndAvailability(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, market := seedSupplier(t, ctx, repo)

	_, supplierProduct, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, ProductDraft{
		Slug: "offer-product", Status: "active", SupplierCode: "OFF-1",
		Translations: []ProductTranslation{{Locale: "en", Name: "Offer Product"}},
	})
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}

	price := money.MustNew(12345, "EGP")
	available := true
	qty := int64(42)

	offer, err := repo.CreateSupplierOfferAtomically(ctx, supplier.ID, OfferDraft{
		SupplierProductID: supplierProduct.ID,
		SupplierMarketID:  market.ID,
		MarketCode:        "EG",
		Status:            "active",
		Price:             &price,
		IsAvailable:       &available,
		AvailableQty:      &qty,
	})
	if err != nil {
		t.Fatalf("create supplier offer atomically: %v", err)
	}

	if got := countRows(t, db, `SELECT count(*) FROM supplier_offers WHERE id = $1`, offer.ID); got != 1 {
		t.Errorf("offers = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_offer_prices WHERE supplier_offer_id = $1 AND amount_minor = 12345`, offer.ID); got != 1 {
		t.Errorf("priced offers = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_offer_availability WHERE supplier_offer_id = $1 AND available_qty = 42`, offer.ID); got != 1 {
		t.Errorf("availability rows = %d, want 1", got)
	}
}

// An invalid price must not leave an unpriced offer behind.
func TestCreateSupplierOfferAtomicallyLeavesNoUnpricedOffer(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, market := seedSupplier(t, ctx, repo)

	_, supplierProduct, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, ProductDraft{
		Slug: "unpriced-probe", Status: "active", SupplierCode: "UNPRICED-1",
		Translations: []ProductTranslation{{Locale: "en", Name: "Unpriced Probe"}},
	})
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}

	// A negative amount is rejected by money validation, which is exactly the
	// step that used to run after the offer row was already committed.
	invalid := money.Money{AmountMinor: -500, Currency: "EGP"}
	available := true

	_, err = repo.CreateSupplierOfferAtomically(ctx, supplier.ID, OfferDraft{
		SupplierProductID: supplierProduct.ID,
		SupplierMarketID:  market.ID,
		MarketCode:        "EG",
		Status:            "active",
		Price:             &invalid,
		IsAvailable:       &available,
	})
	if err == nil {
		t.Fatal("expected an invalid price to fail the operation")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}

	if got := countRows(t, db, `SELECT count(*) FROM supplier_offers WHERE supplier_product_id = $1`, supplierProduct.ID); got != 0 {
		t.Errorf("orphaned offers = %d, want 0", got)
	}
}

// An offer with no price at all is still a valid request: price and availability
// are optional on the draft.
func TestCreateSupplierOfferAtomicallyAllowsOmittedPriceAndAvailability(t *testing.T) {
	ctx, repo, db := setupCompositeTest(t)
	supplier, market := seedSupplier(t, ctx, repo)

	_, supplierProduct, err := repo.CreateSupplierProductAtomically(ctx, supplier.ID, ProductDraft{
		Slug: "bare-offer-product", Status: "active", SupplierCode: "BARE-1",
		Translations: []ProductTranslation{{Locale: "en", Name: "Bare Offer Product"}},
	})
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}

	offer, err := repo.CreateSupplierOfferAtomically(ctx, supplier.ID, OfferDraft{
		SupplierProductID: supplierProduct.ID,
		SupplierMarketID:  market.ID,
		MarketCode:        "EG",
		Status:            "active",
	})
	if err != nil {
		t.Fatalf("create bare offer: %v", err)
	}

	if got := countRows(t, db, `SELECT count(*) FROM supplier_offers WHERE id = $1`, offer.ID); got != 1 {
		t.Errorf("offers = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT count(*) FROM supplier_offer_prices WHERE supplier_offer_id = $1`, offer.ID); got != 0 {
		t.Errorf("price rows = %d, want 0 when no price was supplied", got)
	}
}

func TestCreateSupplierOfferAtomicallyRejectsInvalidDraft(t *testing.T) {
	ctx, repo, _ := setupCompositeTest(t)
	supplier, market := seedSupplier(t, ctx, repo)

	negative := int64(-1)
	cases := map[string]OfferDraft{
		"missing supplier product": {SupplierMarketID: market.ID, MarketCode: "EG", Status: "active"},
		"missing market":           {SupplierProductID: "x", MarketCode: "EG", Status: "active"},
		"missing market code":      {SupplierProductID: "x", SupplierMarketID: market.ID, Status: "active"},
		"missing status":           {SupplierProductID: "x", SupplierMarketID: market.ID, MarketCode: "EG"},
		"negative quantity": {
			SupplierProductID: "x", SupplierMarketID: market.ID, MarketCode: "EG", Status: "active",
			AvailableQty: &negative,
		},
	}

	for name, draft := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := repo.CreateSupplierOfferAtomically(ctx, supplier.ID, draft); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}
