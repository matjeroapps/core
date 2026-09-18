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
	"github.com/google/uuid"
)

func setupP51Database(t *testing.T) (*database.Pool, Repository, context.Context) {
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
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000007_theme_engine_schema",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
	} {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	return db, NewRepository(db.Pool), context.Background()
}

func TestP51CustomerCartConstraintsAndCanonicalAdd(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "p51-seller-"+suffix, "P51 Seller", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p51-store-"+suffix, "P51 Store", "active", nil, "p51-"+suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	storeB, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p51-store-b-"+suffix, "P51 Store B", "active", nil, "p51-b-"+suffix+".test", "platform", "active", true, nil, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	product, err := repo.CreateProduct(ctx, "p51-product-"+suffix, "active")
	if err != nil {
		t.Fatal(err)
	}
	variant, err := repo.CreateVariant(ctx, product.ID, "default", "active")
	if err != nil {
		t.Fatal(err)
	}
	sku, err := repo.CreateSKU(ctx, variant.ID, "p51-sku-"+suffix, "", "active")
	if err != nil {
		t.Fatal(err)
	}
	location, err := repo.CreateStoreFulfillmentLocation(ctx, store.ID, "EG", "store-stock", "Store stock", "warehouse", "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateInventorySnapshot(ctx, location.ID, sku.ID, 5); err != nil {
		t.Fatal(err)
	}

	listingA, err := repo.CreateSellerListing(ctx, store.ID, product.ID, nil, "EG", "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSellerListingPrice(ctx, listingA.ID, money.MustNew(1000, "EGP")); err != nil {
		t.Fatal(err)
	}
	listingB := listingA
	if _, err := repo.SetSellerListingPrice(ctx, listingA.ID, money.MustNew(1200, "EGP")); err != nil {
		t.Fatal(err)
	}

	customer, err := repo.CreateCustomer(ctx, store.ID, "EG", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateCart(ctx, store.ID, "EG", &customer.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateCart(ctx, store.ID, "EG", &customer.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active customer cart error = %v", err)
	}

	cart, token, err := repo.CreateCart(ctx, storeB.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected raw Cart capability only at creation")
	}
	var storedDigest string
	if err := db.QueryRow(ctx, `SELECT cart_token_digest FROM carts WHERE id = $1`, cart.ID).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	if storedDigest == token || storedDigest == "" {
		t.Fatal("Cart stores raw token or no digest")
	}

	// Add to Store A's Cart: Core resolves the public SKU to newest Listing B;
	// there is no API parameter through which the caller can select Listing A.
	cartA, tokenA, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	cartA, err = repo.AddCartItem(ctx, store.ID, tokenA, sku.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cartA.Items) != 1 || cartA.Items[0].SellerListingID != listingB.ID || cartA.Items[0].ExpectedUnitPriceMinor != 1200 {
		t.Fatalf("canonical Cart resolution = %+v, want listing %s at 1200", cartA.Items, listingB.ID)
	}

	if _, err := repo.UpdateCartItemQuantity(ctx, store.ID, tokenA, cartA.Items[0].ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RemoveCartItem(ctx, store.ID, tokenA, cartA.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE carts SET status = 'checked_out' WHERE id = $1`, cartA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddCartItem(ctx, store.ID, tokenA, sku.ID, 1); !errors.Is(err, ErrCartExpired) {
		t.Fatalf("checked-out add error = %v", err)
	}
}

func TestP51FulfillmentOwnershipAndCustomerIsolationConstraints(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	suffix := uuid.NewString()
	seller, err := repo.CreateSeller(ctx, "p51-constraint-seller-"+suffix, "P51 Constraint Seller", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p51-c-"+suffix, "P51 C", "active", nil, "p51-c-"+suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	storeB, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p51-d-"+suffix, "P51 D", "active", nil, "p51-d-"+suffix+".test", "platform", "active", true, nil, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	supplier, err := repo.CreateSupplier(ctx, "p51-supplier-"+suffix, "P51 Supplier", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	supplierMarket, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, supplierMarket.ID, "EG", "supplier-code", "Supplier", "warehouse", "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateStoreFulfillmentLocation(ctx, store.ID, "EG", "store-code", "Store", "warehouse", "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateStoreFulfillmentLocation(ctx, storeB.ID, "EG", "store-code", "Store B", "warehouse", "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateStoreFulfillmentLocation(ctx, store.ID, "EG", "store-code", "Duplicate", "warehouse", "active"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Store location error = %v", err)
	}

	var constraintCases = []struct {
		name string
		args []any
	}{
		{"both owners", []any{uuid.NewString(), supplier.ID, supplierMarket.ID, store.ID, "EG", "both", "Both", "warehouse", "active"}},
		{"no owner", []any{uuid.NewString(), nil, nil, nil, "EG", "none", "None", "warehouse", "active"}},
	}
	for _, tc := range constraintCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, supplier_id, supplier_market_id, store_id, market_code, code, name, location_type, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tc.args...)
			if err == nil {
				t.Fatal("expected ownership constraint rejection")
			}
		})
	}

	customer, err := repo.CreateCustomer(ctx, store.ID, "EG", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO customers (id, store_id, market_code, status) VALUES ($1, $2, 'SA', 'active')`, uuid.NewString(), store.ID); err == nil {
		t.Fatal("expected Customer Store/Market mismatch rejection")
	}
	if _, err := repo.CreateCustomerAddress(ctx, CustomerAddress{CustomerID: customer.ID, StoreID: storeB.ID, RecipientName: "Wrong Store", AddressLine1: "1 Main", City: "Cairo", CountryCode: "EG"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-Store Customer address error = %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO carts (id, store_id, market_code, customer_id, cart_token_digest, status) VALUES ($1,$2,$3,$4,$5,'active')`, uuid.NewString(), storeB.ID, "EG", customer.ID, "digest-"+suffix); err == nil {
		t.Fatal("expected cross-Store Customer to Cart rejection")
	}
	if _, err := db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1,$2,$3,$4,1,NULL,'EGP')`, uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()); err == nil {
		t.Fatal("expected NOT NULL Cart price snapshot rejection")
	}
}
