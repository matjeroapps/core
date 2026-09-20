package marketplace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"core/internal/testdb"
	"core/packages/database"
	"core/packages/i18n"
)

type discoveryFixture struct {
	db                 *database.Pool
	repository         Repository
	egListingOne       string
	egListingTwo       string
	egListingEmpty     string
	noPriceListing     string
	inactiveListing    string
	unpublishedListing string
	saListing          string
}

func setupDiscoveryFixture(t *testing.T) discoveryFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	migrationNames := []string{
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
		"000017_create_shipping_schema",
		"000018_supplier_retail_affiliation",
		"000019_create_payments_schema",
		"000020_create_ledger_schema",
		"000021_create_balance_projection_schema",
		"000026_seller_catalog_phase_c",
		"000032_marketplace_attribution",
	}

	migrationPaths := make([]string, 0, len(migrationNames))
	for _, migrationName := range migrationNames {
		migrationPaths = append(migrationPaths, filepath.Join("..", "..", "migrations", migrationName+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrationPaths...)

	fixture := discoveryFixture{db: db, repository: NewRepository(db.Pool)}
	ctx := context.Background()
	sellerID := uuid.NewString()
	storeEG := uuid.NewString()
	storeSA := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status) VALUES
			($1, $2, 'Discovery Seller', 'active')
	`, sellerID, "discovery-"+sellerID[:8]); err != nil {
		t.Fatalf("insert seller: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status) VALUES
			($1, $2, 'EG', $3, 'Egypt Store', 'active'),
			($4, $2, 'SA', $5, 'Saudi Store', 'active')
	`, storeEG, sellerID, "eg-"+storeEG[:8], storeSA, "sa-"+storeSA[:8]); err != nil {
		t.Fatalf("insert stores: %v", err)
	}

	type productFixture struct {
		listingID string
		productID string
		skuID     string
		createdAt time.Time
	}
	insertProduct := func(name, slug, market, storeID string, listingStatus string, productStatus string, createdAt time.Time, price bool, arabic bool) productFixture {
		t.Helper()
		productID := uuid.NewString()
		variantID := uuid.NewString()
		skuID := uuid.NewString()
		listingID := uuid.NewString()
		if _, err := db.Exec(ctx, `
			INSERT INTO products (id, slug, status) VALUES ($1, $2, $3)
		`, productID, slug, productStatus); err != nil {
			t.Fatalf("insert product %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', $2)
		`, productID, name); err != nil {
			t.Fatalf("insert English translation %s: %v", slug, err)
		}
		if arabic {
			if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'ar', $2)`, productID, "عربي "+name); err != nil {
				t.Fatalf("insert Arabic translation %s: %v", slug, err)
			}
		}
		if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "variant-"+slug); err != nil {
			t.Fatalf("insert variant %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "sku-"+slug); err != nil {
			t.Fatalf("insert SKU %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO seller_listings (id, store_id, product_id, market_code, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, listingID, storeID, productID, market, listingStatus, createdAt); err != nil {
			t.Fatalf("insert listing %s: %v", slug, err)
		}
		if price {
			if _, err := db.Exec(ctx, `
				INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current)
				VALUES ($1, $2, 1000, $3, true)
			`, uuid.NewString(), listingID, map[string]string{"EG": "EGP", "SA": "SAR"}[market]); err != nil {
				t.Fatalf("insert price %s: %v", slug, err)
			}
		}
		return productFixture{listingID: listingID, productID: productID, skuID: skuID, createdAt: createdAt}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	one := insertProduct("Arabic Product", "arabic-product-"+uuid.NewString()[:8], "EG", storeEG, "published", "active", now.Add(-4*time.Hour), true, true)
	two := insertProduct("English Fallback", "english-fallback-"+uuid.NewString()[:8], "EG", storeEG, "published", "active", now.Add(-3*time.Hour), true, false)
	empty := insertProduct("Unavailable Product", "unavailable-"+uuid.NewString()[:8], "EG", storeEG, "published", "active", now.Add(-2*time.Hour), true, false)
	noPrice := insertProduct("No Current Price", "no-price-"+uuid.NewString()[:8], "EG", storeEG, "published", "active", now.Add(-90*time.Minute), false, false)
	inactive := insertProduct("Inactive Product", "inactive-"+uuid.NewString()[:8], "EG", storeEG, "published", "draft", now.Add(-60*time.Minute), true, false)
	unpublished := insertProduct("Unpublished Product", "unpublished-"+uuid.NewString()[:8], "EG", storeEG, "unpublished", "active", now.Add(-30*time.Minute), true, false)
	sa := insertProduct("Saudi Product", "saudi-"+uuid.NewString()[:8], "SA", storeSA, "published", "active", now.Add(-time.Minute), true, false)
	fixture.egListingOne, fixture.egListingTwo, fixture.egListingEmpty, fixture.saListing = one.listingID, two.listingID, empty.listingID, sa.listingID
	fixture.noPriceListing, fixture.inactiveListing, fixture.unpublishedListing = noPrice.listingID, inactive.listingID, unpublished.listingID

	locationEG := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Local EG', 'warehouse', 'active')
	`, locationEG, storeEG, "eg-location-"+locationEG[:8]); err != nil {
		t.Fatalf("insert EG location: %v", err)
	}
	insertInventory := func(skuID string, onHand, reserved int64) string {
		snapshotID := uuid.NewString()
		if _, err := db.Exec(ctx, `
			INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version)
			VALUES ($1, $2, $3, $4, $5, 1)
		`, snapshotID, locationEG, skuID, onHand, reserved); err != nil {
			t.Fatalf("insert inventory: %v", err)
		}
		return snapshotID
	}
	snapshotOne := insertInventory(one.skuID, 10, 0)
	insertInventory(two.skuID, 3, 0)
	snapshotEmpty := insertInventory(empty.skuID, 1, 1)

	insertDeliveredOrder := func(listingID, productID, skuID, snapshotID, status string, quantity int64) {
		t.Helper()
		cartID := uuid.NewString()
		sessionID := uuid.NewString()
		orderID := uuid.NewString()
		reservationID := uuid.NewString()
		if _, err := db.Exec(ctx, `
			INSERT INTO carts (id, store_id, market_code, cart_token_digest, status)
			VALUES ($1, $2, 'EG', $3, 'checked_out')
		`, cartID, storeEG, "cart-"+cartID); err != nil {
			t.Fatalf("insert %s cart: %v", status, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest)
			VALUES ($1, $2, $3, 'finalized', now(), decode(repeat('bb', 32), 'hex'))
		`, sessionID, storeEG, cartID); err != nil {
			t.Fatalf("insert %s checkout session: %v", status, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO orders (
				id, order_number, store_id, market_code, checkout_session_id, status,
				currency_code, guest_order_access_token_digest, subtotal_minor, total_minor,
				confirmation_deadline_at
			) VALUES ($1, $2, $3, 'EG', $4, $5, 'EGP', decode(repeat('cc', 32), 'hex'), $6, $6, now())
		`, orderID, "order-"+orderID, storeEG, sessionID, status, quantity*1000); err != nil {
			t.Fatalf("insert %s order: %v", status, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO inventory_reservations (
				id, inventory_snapshot_id, quantity, status, reservation_token
			) VALUES ($1, $2, $3, 'committed', $4)
		`, reservationID, snapshotID, quantity, "reservation-"+reservationID); err != nil {
			t.Fatalf("insert %s reservation: %v", status, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO order_items (
				id, order_id, seller_listing_id, product_id, sku_id, fulfillment_location_id,
				inventory_reservation_id, product_title_snapshot, sku_code_snapshot,
				unit_price_minor, currency_code, quantity, line_total_minor
			)
			SELECT $1, $2, $3, $4, $5, fulfillment_location_id, $6, 'Snapshot', 'SKU',
				1000, 'EGP', $7::bigint, $7::bigint * 1000
			FROM inventory_snapshots
			WHERE id = $8
		`, uuid.NewString(), orderID, listingID, productID, skuID, reservationID, quantity, snapshotID); err != nil {
			t.Fatalf("insert %s order item: %v", status, err)
		}
	}
	insertDeliveredOrder(one.listingID, one.productID, one.skuID, snapshotOne, "delivered", 5)
	insertDeliveredOrder(two.listingID, two.productID, two.skuID, snapshotEmpty, "pending", 99)

	return fixture
}

func TestRepositoryCollections(t *testing.T) {
	fixture := setupDiscoveryFixture(t)
	ctx := context.Background()
	svc := NewService(fixture.repository)

	t.Run("empty collection", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "AE", Locale: i18n.LocaleEnglish}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		if len(result.Items) != 0 {
			t.Fatalf("items = %d, want 0", len(result.Items))
		}
	})

	t.Run("market isolation", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		for _, item := range result.Items {
			if item.ListingID == fixture.saListing {
				t.Fatalf("Saudi listing leaked into EG collection")
			}
		}
	})

	t.Run("Arabic falls back to English", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleArabic, Limit: 10}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		found := false
		for _, item := range result.Items {
			if item.ListingID == fixture.egListingTwo {
				found = true
				if item.Title != "English Fallback" {
					t.Fatalf("fallback title = %q, want English Fallback", item.Title)
				}
			}
		}
		if !found {
			t.Fatalf("listing %s not found", fixture.egListingTwo)
		}
	})

	t.Run("new products exclude unpublished inactive and missing current price", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish, Limit: 100}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		if len(result.Items) != 3 {
			t.Fatalf("eligible items = %d, want 3", len(result.Items))
		}
		excluded := map[string]string{
			fixture.noPriceListing:     "missing current price",
			fixture.inactiveListing:    "inactive product",
			fixture.unpublishedListing: "unpublished listing",
		}
		for _, item := range result.Items {
			if reason, ok := excluded[item.ListingID]; ok {
				t.Fatalf("%s listing %s was returned", reason, item.ListingID)
			}
		}
	})

	t.Run("best sellers aggregate delivered quantity only", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish}, CollectionBestSellers)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		if len(result.Items) != 1 || result.Items[0].ListingID != fixture.egListingOne {
			t.Fatalf("best sellers = %+v, want only %s", result.Items, fixture.egListingOne)
		}
		if result.Items[0].DeliveredQty != 5 {
			t.Fatalf("delivered quantity = %d, want 5", result.Items[0].DeliveredQty)
		}
	})

	t.Run("fast delivery excludes unavailable local inventory", func(t *testing.T) {
		result, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish, Limit: 100}, CollectionFastDelivery)
		if err != nil {
			t.Fatalf("GetCollection: %v", err)
		}
		for _, item := range result.Items {
			if item.ListingID == fixture.egListingEmpty {
				t.Fatalf("listing with no available local inventory was returned")
			}
		}
	})

	t.Run("cursor stability", func(t *testing.T) {
		first, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish, Limit: 1}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("first page: %v", err)
		}
		if first.NextCursor == "" || len(first.Items) != 1 {
			t.Fatalf("first page = %+v, want one item and cursor", first)
		}
		second, err := svc.GetCollection(ctx, PageRequest{MarketCode: "EG", Locale: i18n.LocaleEnglish, Limit: 1, Cursor: first.NextCursor}, CollectionNewProducts)
		if err != nil {
			t.Fatalf("second page: %v", err)
		}
		if len(second.Items) != 1 || second.Items[0].ListingID == first.Items[0].ListingID {
			t.Fatalf("second page = %+v, cursor repeated or missing", second)
		}
	})
}

func TestIntegrationResolveListing(t *testing.T) {
	fixture := setupDiscoveryFixture(t)
	svc := NewService(fixture.repository)
	ctx := context.Background()

	t.Run("resolves published listing with stock successfully", func(t *testing.T) {
		resolved, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: fixture.egListingOne,
			Quantity:        2,
			Locale:          i18n.LocaleArabic,
		})
		if err != nil {
			t.Fatalf("ResolveListing failed: %v", err)
		}
		if resolved.SellerListingID != fixture.egListingOne {
			t.Fatalf("listing id = %s, want %s", resolved.SellerListingID, fixture.egListingOne)
		}
		if resolved.MarketCode != "EG" {
			t.Fatalf("market code = %s, want EG", resolved.MarketCode)
		}
		if resolved.Quantity != 2 {
			t.Fatalf("quantity = %d, want 2", resolved.Quantity)
		}
		if resolved.UnitPriceMinor != 1000 {
			t.Fatalf("unit price = %d, want 1000", resolved.UnitPriceMinor)
		}
		if resolved.CurrencyCode != "EGP" {
			t.Fatalf("currency = %s, want EGP", resolved.CurrencyCode)
		}
		if resolved.StoreID == "" || resolved.ProductID == "" || resolved.SKUID == "" || resolved.FulfillmentLocationID == "" {
			t.Fatalf("resolved fields must not be empty: %+v", resolved)
		}
		if resolved.ProductTitle != "عربي Arabic Product" {
			t.Fatalf("product title = %q, want Arabic title", resolved.ProductTitle)
		}
		if resolved.Attribution.SellerListingID != fixture.egListingOne || resolved.Attribution.MarketCode != "EG" || resolved.Attribution.StoreID != resolved.StoreID {
			t.Fatalf("invalid attribution: %+v", resolved.Attribution)
		}
	})

	t.Run("resolves fast delivery listing with attribution", func(t *testing.T) {
		resolved, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:       "EG",
			SellerListingID:  fixture.egListingOne,
			Quantity:         1,
			SourceCollection: CollectionFastDelivery,
		})
		if err != nil {
			t.Fatalf("ResolveListing fast delivery failed: %v", err)
		}
		if resolved.Attribution.SourceCollection == nil || *resolved.Attribution.SourceCollection != string(CollectionFastDelivery) {
			t.Fatalf("source collection = %v, want %s", resolved.Attribution.SourceCollection, CollectionFastDelivery)
		}
	})

	t.Run("rejects cross market resolution", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "SA",
			SellerListingID: fixture.egListingOne,
			Quantity:        1,
		})
		if err == nil || !containsError(err, ErrCrossMarketAccess) {
			t.Fatalf("error = %v, want ErrCrossMarketAccess", err)
		}
	})

	t.Run("rejects not found listing", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: uuid.NewString(),
			Quantity:        1,
		})
		if err == nil || !containsError(err, ErrListingNotFound) {
			t.Fatalf("error = %v, want ErrListingNotFound", err)
		}
	})

	t.Run("rejects unpublished listing", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: fixture.unpublishedListing,
			Quantity:        1,
		})
		if err == nil || !containsError(err, ErrListingNotPublished) {
			t.Fatalf("error = %v, want ErrListingNotPublished", err)
		}
	})

	t.Run("rejects inactive product", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: fixture.inactiveListing,
			Quantity:        1,
		})
		if err == nil || !containsError(err, ErrProductUnavailable) {
			t.Fatalf("error = %v, want ErrProductUnavailable", err)
		}
	})

	t.Run("rejects missing current price", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: fixture.noPriceListing,
			Quantity:        1,
		})
		if err == nil || !containsError(err, ErrPriceUnavailable) {
			t.Fatalf("error = %v, want ErrPriceUnavailable", err)
		}
	})

	t.Run("rejects insufficient inventory", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: fixture.egListingOne,
			Quantity:        9999,
		})
		if err == nil || !containsError(err, ErrInventoryUnavailable) {
			t.Fatalf("error = %v, want ErrInventoryUnavailable", err)
		}
	})

	t.Run("fast delivery rejects when no eligible store location has inventory", func(t *testing.T) {
		_, err := svc.ResolveListing(ctx, ResolveListingParams{
			MarketCode:       "EG",
			SellerListingID:  fixture.egListingEmpty,
			Quantity:         1,
			SourceCollection: CollectionFastDelivery,
		})
		if err == nil || (!containsError(err, ErrInventoryUnavailable) && !containsError(err, ErrNoEligibleLocation)) {
			t.Fatalf("error = %v, want ErrInventoryUnavailable or ErrNoEligibleLocation", err)
		}
	})
}
