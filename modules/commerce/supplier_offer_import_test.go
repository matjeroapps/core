package commerce_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

func setupOfferImportDB(t *testing.T) (*database.Pool, commerce.Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	ctx := context.Background()

	migrations := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000008_storefront_revisions",
		"000025_seller_catalog_phase_b",
	}

	for _, name := range migrations {
		path := "../../migrations/" + name + ".up.sql"
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	repo := commerce.NewRepository(db.Pool)
	svc := commerce.NewService(repo)
	return db, svc, ctx
}

func TestAtomicSupplierOfferImportWithMarkup(t *testing.T) {
	db, svc, ctx := setupOfferImportDB(t)

	sellerID := uuid.NewString()
	storeID := uuid.NewString()
	supplierID := uuid.NewString()
	suppMarketID := uuid.NewString()
	suppProdID := uuid.NewString()
	productID := uuid.NewString()
	offerID := uuid.NewString()
	offer2ID := uuid.NewString()
	offerSubWholesaleID := uuid.NewString()
	subject := "seller-member-" + uuid.NewString()

	// 1. Seed Seller, Store, and Member
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status)
		VALUES ($1, 'SLR-IMPORT', 'Offer Import Seller', 'active')
	`, sellerID)
	if err != nil {
		t.Fatalf("seed seller: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO stores (id, seller_id, code, name, market_code, status)
		VALUES ($1, $2, 'STR-IMPORT', 'Import Store', 'SA', 'active')
	`, storeID, sellerID)
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO seller_members (id, seller_id, principal_subject, role, status)
		VALUES ($1, $2, $3, 'seller_manager', 'active')
	`, uuid.NewString(), sellerID, subject)
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}

	// 2. Seed Global Product
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status)
		VALUES ($1, 'importable-product', 'active')
	`, productID)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	// 3. Seed Supplier, Market, Supplier Product
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO suppliers (id, code, name, status)
		VALUES ($1, 'SUP-IMPORT', 'Import Supplier', 'active')
	`, supplierID)
	if err != nil {
		t.Fatalf("seed supplier: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO supplier_markets (id, supplier_id, market_code, status)
		VALUES ($1, $2, 'SA', 'active')
	`, suppMarketID, supplierID)
	if err != nil {
		t.Fatalf("seed supplier market: %v", err)
	}

	// 4. Seed 3 Global Products, Supplier Products & Offers with wholesale price 100.00 SAR (10000 minor units)
	offers := []struct {
		id   string
		spID string
		pID  string
		code string
	}{
		{id: offerID, spID: suppProdID, pID: productID, code: "SP-IMPORT-1"},
		{id: offer2ID, spID: uuid.NewString(), pID: uuid.NewString(), code: "SP-IMPORT-2"},
		{id: offerSubWholesaleID, spID: uuid.NewString(), pID: uuid.NewString(), code: "SP-IMPORT-3"},
	}

	for _, o := range offers {
		if o.pID != productID {
			_, err = db.Pool.Exec(ctx, `
				INSERT INTO products (id, slug, status)
				VALUES ($1, $2, 'active')
			`, o.pID, "product-"+o.code)
			if err != nil {
				t.Fatalf("seed product %s: %v", o.pID, err)
			}
		}

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status)
			VALUES ($1, $2, $3, $4, 'active')
		`, o.spID, supplierID, o.pID, o.code)
		if err != nil {
			t.Fatalf("seed supplier product %s: %v", o.spID, err)
		}

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status)
			VALUES ($1, $2, $3, $4, 'SA', 'active')
		`, o.id, supplierID, o.spID, suppMarketID)
		if err != nil {
			t.Fatalf("seed offer %s: %v", o.id, err)
		}

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
			VALUES ($1, $2, 10000, 'SAR', true)
		`, uuid.NewString(), o.id)
		if err != nil {
			t.Fatalf("seed offer price: %v", err)
		}
	}

	// Test 1: Atomic import with markup_percentage = 25.0
	t.Run("Import offer with markup percentage initializes price atomically", func(t *testing.T) {
		markup := 25.0
		res, err := svc.ImportSupplierOfferWithPricingForSubject(ctx, subject, storeID, offerID, commerce.SupplierOfferImportParams{
			MarkupPercentage: &markup,
		})
		if err != nil {
			t.Fatalf("expected successful import, got %v", err)
		}

		if res.RetailPriceMinorUnits != 12500 {
			t.Errorf("expected retail price 12500 (10000 + 25%%), got %d", res.RetailPriceMinorUnits)
		}
		if res.WholesalePriceMinorUnits != 10000 {
			t.Errorf("expected wholesale price 10000, got %d", res.WholesalePriceMinorUnits)
		}
		if res.MarginPercentage != 25.0 {
			t.Errorf("expected margin 25.0, got %f", res.MarginPercentage)
		}
		if res.Status != "draft" {
			t.Errorf("expected draft status, got %s", res.Status)
		}

		// Verify price table row actually exists in Postgres
		var priceMinor int64
		err = db.Pool.QueryRow(ctx, `
			SELECT amount_minor FROM seller_listing_prices WHERE seller_listing_id = $1 AND is_current = true
		`, res.ListingID).Scan(&priceMinor)
		if err != nil {
			t.Fatalf("query saved listing price: %v", err)
		}
		if priceMinor != 12500 {
			t.Errorf("expected persisted price 12500, got %d", priceMinor)
		}
	})

	// Test 2: Re-importing same offer returns ErrOfferAlreadyImported (409 conflict)
	t.Run("Duplicate offer import returns ErrOfferAlreadyImported", func(t *testing.T) {
		markup := 30.0
		_, err := svc.ImportSupplierOfferWithPricingForSubject(ctx, subject, storeID, offerID, commerce.SupplierOfferImportParams{
			MarkupPercentage: &markup,
		})
		if !errors.Is(err, commerce.ErrOfferAlreadyImported) {
			t.Fatalf("expected ErrOfferAlreadyImported, got %v", err)
		}
	})

	// Test 3: Atomic import with explicit retail price >= wholesale
	t.Run("Import offer with explicit retail price succeeds", func(t *testing.T) {
		retailPrice := int64(15000)
		res, err := svc.ImportSupplierOfferWithPricingForSubject(ctx, subject, storeID, offer2ID, commerce.SupplierOfferImportParams{
			RetailPriceMinorUnits: &retailPrice,
		})
		if err != nil {
			t.Fatalf("expected successful import, got %v", err)
		}
		if res.RetailPriceMinorUnits != 15000 {
			t.Errorf("expected retail price 15000, got %d", res.RetailPriceMinorUnits)
		}
		if res.MarginPercentage != 50.0 {
			t.Errorf("expected margin 50.0, got %f", res.MarginPercentage)
		}
	})

	// Test 4: Atomic import with sub-wholesale price is rejected (ErrUnsafeMargin) and rolls back
	t.Run("Import offer with sub-wholesale price is rejected with ErrUnsafeMargin", func(t *testing.T) {
		subWholesalePrice := int64(8000)
		_, err := svc.ImportSupplierOfferWithPricingForSubject(ctx, subject, storeID, offerSubWholesaleID, commerce.SupplierOfferImportParams{
			RetailPriceMinorUnits: &subWholesalePrice,
		})
		if !errors.Is(err, commerce.ErrUnsafeMargin) {
			t.Fatalf("expected ErrUnsafeMargin, got %v", err)
		}

		// Verify neither listing nor price were inserted (transactional atomicity)
		var count int
		err = db.Pool.QueryRow(ctx, `
			SELECT count(*) FROM seller_listings WHERE store_id = $1 AND supplier_offer_id = $2
		`, storeID, offerSubWholesaleID).Scan(&count)
		if err != nil {
			t.Fatalf("check listing count: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0 listings after rollback, got %d", count)
		}
	})
}
