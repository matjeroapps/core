package commerce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/money"
)

// Storefront revision integration tests.
//
// These prove the two properties the whole downstream cache depends on. A write
// that changes public output advances the generation of exactly the stores whose
// output changed, and a write that does not commit does not advance anything.
//
// The fixture is deliberately shared: one supplier product is listed by both
// stores and one product is listed by store B alone, so a bump that is too wide
// (touching an unrelated store) and a bump that is too narrow (missing a store
// that publishes the same underlying record) are both detectable.

type revisionEnv struct {
	ctx  context.Context
	db   *database.Pool
	repo Repository

	storeA string
	storeB string

	// sharedProduct is listed by both stores through the same supplier offer.
	sharedProduct string
	sharedOffer   string
	sharedSKU     string
	sharedStock   string
	listingA      string
	listingB      string

	// soloProduct is listed by store B only.
	soloProduct string
	soloListing string

	category      string
	childCategory string
	locationID    string
}

func setupRevisionTest(t *testing.T) *revisionEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	for _, m := range []string{
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
		"000025_seller_catalog_phase_b",
	} {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}

	ctx := context.Background()
	repo := NewRepository(db.Pool)
	env := &revisionEnv{ctx: ctx, db: db, repo: repo}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	sellerA, err := repo.CreateSeller(ctx, "seller-a-"+suffix, "Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	sellerB, err := repo.CreateSeller(ctx, "seller-b-"+suffix, "Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	storeA, err := repo.CreateStore(ctx, sellerA.ID, "EG", "store-a-"+suffix, "Store A", "active", nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	storeB, err := repo.CreateStore(ctx, sellerB.ID, "EG", "store-b-"+suffix, "Store B", "active", nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	env.storeA, env.storeB = storeA.ID, storeB.ID

	supplier, err := repo.CreateSupplier(ctx, "supplier-"+suffix, "Supplier", "active", nil)
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	market, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market: %v", err)
	}
	location, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, market.ID, "EG", "hub-"+suffix, "Hub", "warehouse", "active")
	if err != nil {
		t.Fatalf("create fulfillment location: %v", err)
	}
	env.locationID = location.ID

	parent, err := repo.CreateCategory(ctx, "parent-"+suffix, nil, "active")
	if err != nil {
		t.Fatalf("create parent category: %v", err)
	}
	child, err := repo.CreateCategory(ctx, "child-"+suffix, &parent.ID, "active")
	if err != nil {
		t.Fatalf("create child category: %v", err)
	}
	env.category, env.childCategory = parent.ID, child.ID

	shared, err := repo.CreateProduct(ctx, "shared-"+suffix, "active")
	if err != nil {
		t.Fatalf("create shared product: %v", err)
	}
	env.sharedProduct = shared.ID
	if err := repo.SetProductCategories(ctx, shared.ID, []string{child.ID}); err != nil {
		t.Fatalf("assign shared categories: %v", err)
	}
	variant, err := repo.CreateVariant(ctx, shared.ID, "default", "active")
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	sku, err := repo.CreateSKU(ctx, variant.ID, "sku-shared-"+suffix, "", "active")
	if err != nil {
		t.Fatalf("create sku: %v", err)
	}
	env.sharedSKU = sku.ID
	snapshot, err := repo.CreateInventorySnapshot(ctx, location.ID, sku.ID, 10)
	if err != nil {
		t.Fatalf("create inventory snapshot: %v", err)
	}
	env.sharedStock = snapshot.ID

	supplierProduct, err := repo.CreateSupplierProduct(ctx, supplier.ID, shared.ID, "SUP-shared-"+suffix, "active")
	if err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	offer, err := repo.CreateSupplierOffer(ctx, supplier.ID, supplierProduct.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("create supplier offer: %v", err)
	}
	env.sharedOffer = offer.ID
	if _, err := repo.SetSupplierOfferPrice(ctx, offer.ID, money.MustNew(10000, "EGP")); err != nil {
		t.Fatalf("set supplier offer price: %v", err)
	}

	listingA, err := repo.CreateSellerListing(ctx, storeA.ID, shared.ID, &offer.ID, "EG", "active")
	if err != nil {
		t.Fatalf("create listing A: %v", err)
	}
	listingB, err := repo.CreateSellerListing(ctx, storeB.ID, shared.ID, &offer.ID, "EG", "active")
	if err != nil {
		t.Fatalf("create listing B: %v", err)
	}
	env.listingA, env.listingB = listingA.ID, listingB.ID
	if _, err := repo.SetSellerListingPrice(ctx, listingA.ID, money.MustNew(15000, "EGP")); err != nil {
		t.Fatalf("price listing A: %v", err)
	}
	if _, err := repo.SetSellerListingPrice(ctx, listingB.ID, money.MustNew(19900, "EGP")); err != nil {
		t.Fatalf("price listing B: %v", err)
	}

	solo, err := repo.CreateProduct(ctx, "solo-"+suffix, "active")
	if err != nil {
		t.Fatalf("create solo product: %v", err)
	}
	env.soloProduct = solo.ID
	soloListing, err := repo.CreateSellerListing(ctx, storeB.ID, solo.ID, nil, "EG", "active")
	if err != nil {
		t.Fatalf("create solo listing: %v", err)
	}
	env.soloListing = soloListing.ID

	return env
}

func (e *revisionEnv) revision(t *testing.T, storeID string) int64 {
	t.Helper()
	revision, err := e.repo.StorefrontRevision(e.ctx, storeID)
	if err != nil {
		t.Fatalf("read storefront revision: %v", err)
	}
	return revision
}

// expectBump asserts which stores a write invalidated. bumpA/bumpB describe the
// expected direction, so a bump that is too wide fails just as loudly as one that
// is too narrow.
func (e *revisionEnv) expectBump(t *testing.T, name string, bumpA, bumpB bool, write func() error) {
	t.Helper()
	beforeA, beforeB := e.revision(t, e.storeA), e.revision(t, e.storeB)
	if err := write(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	afterA, afterB := e.revision(t, e.storeA), e.revision(t, e.storeB)

	assertRevision(t, name+": store A", beforeA, afterA, bumpA)
	assertRevision(t, name+": store B", beforeB, afterB, bumpB)
}

func assertRevision(t *testing.T, label string, before, after int64, expectBump bool) {
	t.Helper()
	switch {
	case expectBump && after <= before:
		t.Errorf("%s: revision did not advance (%d -> %d)", label, before, after)
	case !expectBump && after != before:
		t.Errorf("%s: revision advanced unexpectedly (%d -> %d)", label, before, after)
	}
}

// Every store must have a usable generation the moment it exists, so a
// storefront never starts without one.
func TestStorefrontRevisionStartsAtOne(t *testing.T) {
	env := setupRevisionTest(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	seller, err := env.repo.CreateSeller(env.ctx, "seller-fresh-"+suffix, "Fresh", "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	store, err := env.repo.CreateStore(env.ctx, seller.ID, "EG", "store-fresh-"+suffix, "Fresh", "active", nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if got := env.revision(t, store.ID); got != 1 {
		t.Fatalf("initial revision = %d, want 1", got)
	}
}

// A store with no row must still read as a usable generation, so a store created
// before this table existed is never left without one.
func TestStorefrontRevisionDefaultsWhenRowMissing(t *testing.T) {
	env := setupRevisionTest(t)

	if _, err := env.db.Exec(env.ctx, `DELETE FROM storefront_revisions WHERE store_id = $1`, env.storeA); err != nil {
		t.Fatalf("delete revision row: %v", err)
	}
	if got := env.revision(t, env.storeA); got != 1 {
		t.Fatalf("revision without a row = %d, want 1", got)
	}

	// The first bump must land on the generation that follows the implicit one.
	if _, err := env.repo.SetSellerListingPrice(env.ctx, env.listingA, money.MustNew(12345, "EGP")); err != nil {
		t.Fatalf("set listing price: %v", err)
	}
	if got := env.revision(t, env.storeA); got != 2 {
		t.Fatalf("revision after first bump = %d, want 2", got)
	}
}

func TestStorefrontRevisionRequiresStore(t *testing.T) {
	env := setupRevisionTest(t)

	if _, err := env.repo.StorefrontRevision(env.ctx, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty store id error = %v, want ErrInvalidInput", err)
	}
}

// The public price is the seller listing price, so repricing must invalidate that
// store and only that store.
func TestStorefrontRevisionSellerListingPriceBumpsOwningStoreOnly(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "reprice listing A", true, false, func() error {
		_, err := env.repo.SetSellerListingPrice(env.ctx, env.listingA, money.MustNew(16000, "EGP"))
		return err
	})
	env.expectBump(t, "reprice listing B", false, true, func() error {
		_, err := env.repo.SetSellerListingPrice(env.ctx, env.listingB, money.MustNew(20000, "EGP"))
		return err
	})
}

func TestStorefrontRevisionListingStatusBumpsOwningStoreOnly(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "disable listing A", true, false, func() error {
		return env.repo.UpdateSellerListingStatus(env.ctx, env.listingA, "draft")
	})
}

func TestStorefrontRevisionListingImportBumpsImportingStoreOnly(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "import into store A", true, false, func() error {
		_, err := env.repo.CreateSellerListing(env.ctx, env.storeA, env.soloProduct, nil, "EG", "active")
		return err
	})
}

// A store's own status and public identity appear in the bootstrap payload, so
// suspending a store must invalidate it immediately.
func TestStorefrontRevisionStoreWritesBumpThatStoreOnly(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "suspend store A", true, false, func() error {
		return env.repo.UpdateStoreStatus(env.ctx, env.storeA, "inactive")
	})
	env.expectBump(t, "rename store B", false, true, func() error {
		return env.repo.UpdateStoreProfile(env.ctx, env.storeB, "Store B Renamed", "active", map[string]any{
			"public": map[string]any{"tagline": "new"},
		})
	})
}

// A product is a global record. Every store that lists it publishes it, so a
// public product change must invalidate all of them.
func TestStorefrontRevisionSharedProductBumpsEveryListingStore(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "translate shared product", true, true, func() error {
		return env.repo.UpsertProductTranslation(env.ctx, ProductTranslation{
			ProductID: env.sharedProduct, Locale: "en", Name: "Renamed", Description: "d",
		})
	})
	env.expectBump(t, "moderate shared product", true, true, func() error {
		return env.repo.UpdateProductStatus(env.ctx, env.sharedProduct, "draft")
	})
	env.expectBump(t, "reslug shared product", true, true, func() error {
		return env.repo.UpdateProduct(env.ctx, env.sharedProduct, "shared-renamed", "active")
	})
	env.expectBump(t, "recategorize shared product", true, true, func() error {
		return env.repo.SetProductCategories(env.ctx, env.sharedProduct, []string{env.category})
	})
	env.expectBump(t, "add a variant", true, true, func() error {
		_, err := env.repo.CreateVariant(env.ctx, env.sharedProduct, "large", "active")
		return err
	})
}

// A product listed by one store only must never invalidate the other.
func TestStorefrontRevisionUnsharedProductBumpsOneStore(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "translate solo product", false, true, func() error {
		return env.repo.UpsertProductTranslation(env.ctx, ProductTranslation{
			ProductID: env.soloProduct, Locale: "en", Name: "Solo", Description: "",
		})
	})
}

// The public category tree is assembled from a category and its descendants, so a
// change anywhere in the subtree invalidates the stores that publish it.
func TestStorefrontRevisionCategoryWritesBumpPublishingStores(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "translate child category", true, true, func() error {
		return env.repo.UpsertCategoryTranslation(env.ctx, CategoryTranslation{
			CategoryID: env.childCategory, Locale: "en", Name: "Renamed", Description: "",
		})
	})
	env.expectBump(t, "moderate ancestor category", true, true, func() error {
		return env.repo.UpdateCategoryStatus(env.ctx, env.category, "disabled")
	})
	env.expectBump(t, "reslug ancestor category", true, true, func() error {
		return env.repo.UpdateCategory(env.ctx, env.category, "parent-renamed", "active", nil)
	})
}

// Availability is derived from inventory, so a stock movement must invalidate
// every store selling the affected SKU's product.
func TestStorefrontRevisionInventoryWritesBumpEverySellingStore(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "adjust stock upward", true, true, func() error {
		_, _, err := env.repo.AdjustInventory(env.ctx, env.sharedStock, 5, "restock", "", "subject", "", "", "", "")
		return err
	})
	env.expectBump(t, "adjust stock downward", true, true, func() error {
		_, _, err := env.repo.AdjustInventory(env.ctx, env.sharedStock, -5, "shrinkage", "", "subject", "", "", "", "")
		return err
	})
	env.expectBump(t, "reserve stock", true, true, func() error {
		_, err := env.repo.ReserveInventory(env.ctx, env.sharedStock, 1, "token-"+fmt.Sprint(time.Now().UnixNano()), nil)
		return err
	})
	env.expectBump(t, "open a snapshot for a new location sku", true, true, func() error {
		variant, err := env.repo.CreateVariant(env.ctx, env.sharedProduct, "extra", "active")
		if err != nil {
			return err
		}
		sku, err := env.repo.CreateSKU(env.ctx, variant.ID, "sku-extra-"+fmt.Sprint(time.Now().UnixNano()), "", "active")
		if err != nil {
			return err
		}
		_, err = env.repo.CreateInventorySnapshot(env.ctx, env.locationID, sku.ID, 3)
		return err
	})
	env.expectBump(t, "disable the fulfillment location", true, true, func() error {
		return env.repo.UpdateFulfillmentLocationStatus(env.ctx, env.locationID, "disabled")
	})
}

// Offer eligibility and offer availability are both public inputs; the supplier's
// wholesale cost is not and must not throw away a store's cache.
func TestStorefrontRevisionSupplierOfferWrites(t *testing.T) {
	env := setupRevisionTest(t)

	env.expectBump(t, "offer availability", true, true, func() error {
		_, err := env.repo.SetSupplierOfferAvailability(env.ctx, env.sharedOffer, false, nil)
		return err
	})
	env.expectBump(t, "offer status", true, true, func() error {
		return env.repo.UpdateSupplierOfferStatus(env.ctx, env.sharedOffer, "disabled")
	})
	env.expectBump(t, "wholesale price", false, false, func() error {
		_, err := env.repo.SetSupplierOfferPrice(env.ctx, env.sharedOffer, money.MustNew(11000, "EGP"))
		return err
	})
}

// Records that never reach a public payload must not invalidate anything.
func TestStorefrontRevisionIgnoresNonPublicWrites(t *testing.T) {
	env := setupRevisionTest(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	supplier, err := env.repo.CreateSupplier(env.ctx, "supplier-extra-"+suffix, "Extra", "active", nil)
	if err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	market, err := env.repo.CreateSupplierMarket(env.ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("create supplier market: %v", err)
	}

	env.expectBump(t, "supplier status", false, false, func() error {
		return env.repo.UpdateSupplierStatus(env.ctx, supplier.ID, "suspended")
	})
	env.expectBump(t, "supplier market status", false, false, func() error {
		return env.repo.UpdateSupplierMarketStatus(env.ctx, market.ID, "disabled")
	})
	env.expectBump(t, "create an unlisted product", false, false, func() error {
		_, err := env.repo.CreateProduct(env.ctx, "unlisted-"+suffix, "active")
		return err
	})
	env.expectBump(t, "create a supplier product and offer atomically", false, false, func() error {
		_, supplierProduct, err := env.repo.CreateSupplierProductAtomically(env.ctx, supplier.ID, ProductDraft{
			Slug:         "fresh-" + suffix,
			Status:       "active",
			SupplierCode: "SUP-fresh-" + suffix,
			Translations: []ProductTranslation{{Locale: "en", Name: "Fresh"}},
		})
		if err != nil {
			return err
		}
		available := true
		price := money.MustNew(9000, "EGP")
		_, err = env.repo.CreateSupplierOfferAtomically(env.ctx, supplier.ID, OfferDraft{
			SupplierProductID: supplierProduct.ID,
			SupplierMarketID:  market.ID,
			MarketCode:        "EG",
			Status:            "active",
			Price:             &price,
			IsAvailable:       &available,
		})
		return err
	})
}

// A rolled-back business mutation must not advance the generation, otherwise a
// cache would be discarded for a change that never happened.
func TestStorefrontRevisionDoesNotAdvanceOnRollback(t *testing.T) {
	env := setupRevisionTest(t)

	t.Run("failed inventory adjustment", func(t *testing.T) {
		before := env.revision(t, env.storeA)
		if _, _, err := env.repo.AdjustInventory(env.ctx, env.sharedStock, -1000, "shrinkage", "", "subject", "", "", "", ""); !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("adjustment error = %v, want ErrInsufficientInventory", err)
		}
		if after := env.revision(t, env.storeA); after != before {
			t.Fatalf("revision advanced on a rolled-back adjustment (%d -> %d)", before, after)
		}
	})

	// The category path bumps before its write so a reparent also invalidates the
	// old position. That ordering must still roll back when the write fails.
	t.Run("failed category rename", func(t *testing.T) {
		before := env.revision(t, env.storeA)
		if err := env.repo.UpdateCategory(env.ctx, env.category, categorySlug(t, env, env.childCategory), "active", nil); !errors.Is(err, ErrConflict) {
			t.Fatalf("rename error = %v, want ErrConflict", err)
		}
		if after := env.revision(t, env.storeA); after != before {
			t.Fatalf("revision advanced on a rolled-back category rename (%d -> %d)", before, after)
		}
	})
}

func categorySlug(t *testing.T, env *revisionEnv, categoryID string) string {
	t.Helper()
	var slug string
	if err := env.db.Pool.QueryRow(env.ctx, `SELECT slug FROM categories WHERE id = $1`, categoryID).Scan(&slug); err != nil {
		t.Fatalf("read category slug: %v", err)
	}
	return slug
}

// Concurrent writes must each advance the generation exactly once. A
// read-modify-write would lose bumps here and leave a stale cache readable.
func TestStorefrontRevisionConcurrentBumpsAreNotLost(t *testing.T) {
	env := setupRevisionTest(t)

	const writers = 8
	before := env.revision(t, env.storeA)

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := env.repo.SetSellerListingPrice(env.ctx, env.listingA, money.MustNew(int64(10000+i), "EGP")); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent reprice: %v", err)
	}

	if after := env.revision(t, env.storeA); after != before+writers {
		t.Fatalf("revision = %d after %d concurrent writes, want %d", after, writers, before+writers)
	}
}

// A store deletion must not leave an orphaned generation behind.
func TestStorefrontRevisionIsRemovedWithItsStore(t *testing.T) {
	env := setupRevisionTest(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	seller, err := env.repo.CreateSeller(env.ctx, "seller-temp-"+suffix, "Temp", "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	store, err := env.repo.CreateStore(env.ctx, seller.ID, "EG", "store-temp-"+suffix, "Temp", "active", nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := env.db.Exec(env.ctx, `DELETE FROM stores WHERE id = $1`, store.ID); err != nil {
		t.Fatalf("delete store: %v", err)
	}

	var rows int
	if err := env.db.Pool.QueryRow(env.ctx, `SELECT count(*) FROM storefront_revisions WHERE store_id = $1`, store.ID).Scan(&rows); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if rows != 0 {
		t.Fatalf("orphaned revision rows = %d, want 0", rows)
	}
}
