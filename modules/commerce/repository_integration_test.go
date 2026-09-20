package commerce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"core/internal/testdb"
	"core/packages/database"
	"core/packages/money"
)

func TestRepositoryCommerceFoundations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}

	ctx := context.Background()
	db := testdb.Open(t, dsn)

	applyMigrationBatch(t, db,
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
	)

	repo := NewRepository(db.Pool)
	service := NewService(repo)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	supplier, err := repo.CreateSupplier(ctx, "supplier-"+suffix, "Supplier One", "active", map[string]any{"tier": "gold"})
	if err != nil {
		t.Fatalf("CreateSupplier returned error: %v", err)
	}
	if _, err := repo.CreateSupplierMember(ctx, supplier.ID, "user-1-"+suffix, "owner", "active"); err != nil {
		t.Fatalf("CreateSupplierMember returned error: %v", err)
	}
	supplierMarketEG, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", map[string]any{"currency_locked": true})
	if err != nil {
		t.Fatalf("CreateSupplierMarket EG returned error: %v", err)
	}
	supplierMarketSA, err := repo.CreateSupplierMarket(ctx, supplier.ID, "SA", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket SA returned error: %v", err)
	}

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller One", "active", map[string]any{"channel": "retail"})
	if err != nil {
		t.Fatalf("CreateSeller returned error: %v", err)
	}
	if _, err := repo.CreateSellerMember(ctx, seller.ID, "user-2-"+suffix, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember returned error: %v", err)
	}

	store, err := repo.CreateStore(ctx, seller.ID, "EG", "store-eg-"+suffix, "Cairo Store", "active", map[string]any{"theme": "starter"})
	if err != nil {
		t.Fatalf("CreateStore returned error: %v", err)
	}
	if _, err := repo.CreateStoreDomain(ctx, store.ID, "example-"+suffix+".eg", "platform", "active", true, nil, nil); err != nil {
		t.Fatalf("CreateStoreDomain returned error: %v", err)
	}

	product, err := repo.CreateProduct(ctx, "fresh-detergent-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct returned error: %v", err)
	}
	category, err := repo.CreateCategory(ctx, "home-care-"+suffix, nil, "active")
	if err != nil {
		t.Fatalf("CreateCategory returned error: %v", err)
	}
	if err := repo.UpsertProductTranslation(ctx, ProductTranslation{ProductID: product.ID, Locale: "ar", Name: "منظف", Description: "منظف متعدد الاستخدامات"}); err != nil {
		t.Fatalf("UpsertProductTranslation ar returned error: %v", err)
	}
	if err := repo.UpsertProductTranslation(ctx, ProductTranslation{ProductID: product.ID, Locale: "en", Name: "Detergent", Description: "Multi-purpose detergent"}); err != nil {
		t.Fatalf("UpsertProductTranslation en returned error: %v", err)
	}
	if err := repo.SetProductCategories(ctx, product.ID, []string{category.ID}); err != nil {
		t.Fatalf("SetProductCategories returned error: %v", err)
	}

	var translationCount int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM product_translations WHERE product_id = $1`, product.ID).Scan(&translationCount); err != nil {
		t.Fatalf("count translations: %v", err)
	}
	if translationCount != 2 {
		t.Fatalf("expected 2 translations, got %d", translationCount)
	}
	var categoryCount int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM product_categories WHERE product_id = $1`, product.ID).Scan(&categoryCount); err != nil {
		t.Fatalf("count product categories: %v", err)
	}
	if categoryCount != 1 {
		t.Fatalf("expected 1 product category, got %d", categoryCount)
	}

	resolvedSupplier, err := service.ResolveSupplierIDForSubject(ctx, "user-1-"+suffix)
	if err != nil {
		t.Fatalf("ResolveSupplierIDForSubject returned error: %v", err)
	}
	if resolvedSupplier != supplier.ID {
		t.Fatalf("resolved supplier id = %q", resolvedSupplier)
	}
	resolvedSeller, err := service.ResolveSellerIDForSubject(ctx, "user-2-"+suffix)
	if err != nil {
		t.Fatalf("ResolveSellerIDForSubject returned error: %v", err)
	}
	if resolvedSeller != seller.ID {
		t.Fatalf("resolved seller id = %q", resolvedSeller)
	}

	supplierProduct, err := repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "SUP-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct returned error: %v", err)
	}
	supplierOfferEG, err := repo.CreateSupplierOffer(ctx, supplier.ID, supplierProduct.ID, supplierMarketEG.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer EG returned error: %v", err)
	}
	supplierOfferSA, err := repo.CreateSupplierOffer(ctx, supplier.ID, supplierProduct.ID, supplierMarketSA.ID, "SA", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer SA returned error: %v", err)
	}
	if _, err := repo.SetSupplierOfferPrice(ctx, supplierOfferEG.ID, money.MustNew(1250, "EGP")); err != nil {
		t.Fatalf("SetSupplierOfferPrice returned error: %v", err)
	}
	availableQty := int64(7)
	if _, err := repo.SetSupplierOfferAvailability(ctx, supplierOfferEG.ID, true, &availableQty); err != nil {
		t.Fatalf("SetSupplierOfferAvailability returned error: %v", err)
	}

	catalogItems, err := repo.ListSupplierCatalog(ctx, SupplierCatalogFilter{MarketCode: "EG", SupplierID: supplier.ID, Locale: "en", Page: Page{Limit: 10}})
	if err != nil {
		t.Fatalf("ListSupplierCatalog returned error: %v", err)
	}
	if len(catalogItems) != 1 {
		t.Fatalf("expected 1 catalog item, got %d", len(catalogItems))
	}
	if catalogItems[0].CategoryID != category.ID {
		t.Fatalf("catalog category id = %q", catalogItems[0].CategoryID)
	}

	listing, err := service.CreateSellerListing(ctx, store.ID, product.ID, &supplierOfferEG.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSellerListing returned error: %v", err)
	}
	if _, err := repo.SetSellerListingPrice(ctx, listing.ID, money.MustNew(1500, "EGP")); err != nil {
		t.Fatalf("SetSellerListingPrice returned error: %v", err)
	}

	if _, err := service.CreateSellerListing(ctx, store.ID, product.ID, &supplierOfferSA.ID, "EG", "active"); !errors.Is(err, ErrMarketMismatch) {
		t.Fatalf("expected ErrMarketMismatch, got %v", err)
	}

	if _, err := repo.CreateSellerListing(ctx, store.ID, product.ID, &supplierOfferSA.ID, "EG", "active"); err == nil {
		t.Fatalf("expected repository insertion to fail for mismatched market")
	}

	var verifiedAt = time.Now().UTC()
	_, err = repo.CreateStoreDomain(ctx, store.ID, "store-"+suffix+".example.eg", "platform", "active", false, &verifiedAt, nil)
	if err != nil {
		t.Fatalf("CreateStoreDomain verified returned error: %v", err)
	}

	var primary bool
	if err := db.Pool.QueryRow(ctx, `SELECT is_primary FROM store_domains WHERE domain = $1`, "example-"+suffix+".eg").Scan(&primary); err != nil {
		t.Fatalf("read store domain: %v", err)
	}
	if !primary {
		t.Fatalf("expected primary store domain")
	}

	var variant *Variant
	v, err := repo.CreateVariant(ctx, product.ID, "default", "active")
	if err != nil {
		t.Fatalf("CreateVariant returned error: %v", err)
	}
	variant = &v

	sku, err := repo.CreateSKU(ctx, variant.ID, "SKU-"+suffix, "", "active")
	if err != nil {
		t.Fatalf("CreateSKU returned error: %v", err)
	}
	location, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, supplierMarketSA.ID, "SA", "riyadh-hub", "Riyadh Hub", "warehouse", "active")
	if err != nil {
		t.Fatalf("CreateFulfillmentLocation returned error: %v", err)
	}
	snapshot, err := repo.CreateInventorySnapshot(ctx, location.ID, sku.ID, 3)
	if err != nil {
		t.Fatalf("CreateInventorySnapshot returned error: %v", err)
	}

	expiresAt := time.Now().Add(15 * time.Minute).UTC()
	if _, err := service.ReserveInventory(ctx, snapshot.ID, 2, "reservation-"+suffix+"-1", &expiresAt); err != nil {
		t.Fatalf("ReserveInventory returned error: %v", err)
	}
	updatedSnapshot, err := repo.GetInventorySnapshot(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("GetInventorySnapshot returned error: %v", err)
	}
	if updatedSnapshot.ReservedQty != 2 {
		t.Fatalf("reserved qty = %d", updatedSnapshot.ReservedQty)
	}
	if _, err := service.ReserveInventory(ctx, snapshot.ID, 2, "reservation-"+suffix+"-2", &expiresAt); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("expected ErrInsufficientInventory, got %v", err)
	}
	updatedSnapshot, err = repo.GetInventorySnapshot(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("GetInventorySnapshot returned error: %v", err)
	}
	if updatedSnapshot.ReservedQty != 2 {
		t.Fatalf("reserved qty changed after failed reserve: %d", updatedSnapshot.ReservedQty)
	}

	updatedSnapshot, movement, err := repo.AdjustInventory(ctx, snapshot.ID, 1, "adjust", "receive stock", "user-1-"+suffix, "corr-"+suffix, "cause-"+suffix, "", "")
	if err != nil {
		t.Fatalf("AdjustInventory returned error: %v", err)
	}
	if updatedSnapshot.OnHandQty != 4 {
		t.Fatalf("on hand qty after adjustment = %d", updatedSnapshot.OnHandQty)
	}
	var movementCount int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapshot.ID).Scan(&movementCount); err != nil {
		t.Fatalf("count inventory movements: %v", err)
	}
	if movementCount != 1 {
		t.Fatalf("expected 1 inventory movement, got %d", movementCount)
	}
	if movement.MovementType != "adjust" {
		t.Fatalf("movement type = %q", movement.MovementType)
	}
}

func applySQLFile(t *testing.T, db *database.Pool, path string) {
	t.Helper()

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	if _, err := db.Exec(context.Background(), string(sqlBytes)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}
