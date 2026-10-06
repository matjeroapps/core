package listings_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/listings"
	"core/internal/testdb"
	"core/packages/database"
)

func TestMarginGuardrail_UnitInvariants(t *testing.T) {
	t.Run("Retail price >= wholesale cost succeeds with positive margin", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(12000, 10000, false, false, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		margin := listings.CalculateMarginPercentage(12000, 10000)
		if margin != 20.0 {
			t.Errorf("expected 20.0 margin, got %f", margin)
		}
	})

	t.Run("Retail price < wholesale cost without override returns ErrUnsafeMargin", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(8000, 10000, false, false, "")
		if !errors.Is(err, listings.ErrUnsafeMargin) {
			t.Fatalf("expected ErrUnsafeMargin, got %v", err)
		}
	})

	t.Run("Retail price < wholesale cost with override but non-owner returns ErrOwnerOverrideRequired", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(8000, 10000, true, false, "clearance")
		if !errors.Is(err, listings.ErrOwnerOverrideRequired) {
			t.Fatalf("expected ErrOwnerOverrideRequired, got %v", err)
		}
	})

	t.Run("Retail price < wholesale cost with override and owner but missing reason returns ErrInvalidInput", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(8000, 10000, true, true, "   ")
		if !errors.Is(err, listings.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("Retail price < wholesale cost with override, owner, and valid reason succeeds", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(8000, 10000, true, true, "Seasonal clearance liquidation")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		margin := listings.CalculateMarginPercentage(8000, 10000)
		if margin != -20.0 {
			t.Errorf("expected -20.0 margin, got %f", margin)
		}
	})

	t.Run("Seller-owned product (wholesale=0) allows any non-negative retail price", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(5000, 0, false, false, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		margin := listings.CalculateMarginPercentage(5000, 0)
		if margin != 100.0 {
			t.Errorf("expected 100.0 margin, got %f", margin)
		}
	})

	t.Run("Negative retail price rejected", func(t *testing.T) {
		err := listings.ValidateMarginGuardrail(-100, 10000, false, false, "")
		if !errors.Is(err, listings.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})
}

func setupListingsDB(t *testing.T) (*database.Pool, listings.Service, context.Context) {
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
	}

	for _, name := range migrations {
		path := filepath.Join("..", "..", "migrations", name+".up.sql")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	repo := listings.NewPostgresRepository(db.Pool)
	svc := listings.NewService(repo)
	return db, svc, ctx
}

func TestMarginGuardrail_IntegrationWithPostgres(t *testing.T) {
	db, svc, ctx := setupListingsDB(t)

	sellerID := uuid.NewString()
	storeID := uuid.NewString()
	supplierID := uuid.NewString()
	suppProdID := uuid.NewString()
	suppMarketID := uuid.NewString()
	offerID := uuid.NewString()
	productID := uuid.NewString()
	dropshipListingID := uuid.NewString()
	ownedListingID := uuid.NewString()

	// 1. Seed Seller & Store
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status)
		VALUES ($1, 'SLR-MARGIN', 'Margin Test Seller', 'active')
	`, sellerID)
	if err != nil {
		t.Fatalf("seed seller: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO stores (id, seller_id, code, name, market_code, status)
		VALUES ($1, $2, 'STR-MARGIN', 'Margin Store', 'SA', 'active')
	`, storeID, sellerID)
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	// 2. Seed Supplier, Supplier Market, Supplier Product, and Offer
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO suppliers (id, code, name, status)
		VALUES ($1, 'SUP-MARGIN', 'Margin Supplier', 'active')
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

	// Seed product before supplier_products (FK requirement)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status)
		VALUES ($1, 'dropship-product', 'active')
	`, productID)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status)
		VALUES ($1, $2, $3, 'SP-MARGIN-1', 'active')
	`, suppProdID, supplierID, productID)
	if err != nil {
		t.Fatalf("seed supplier product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status)
		VALUES ($1, $2, $3, $4, 'SA', 'active')
	`, offerID, supplierID, suppProdID, suppMarketID)
	if err != nil {
		t.Fatalf("seed supplier offer: %v", err)
	}

	// Wholesale price = 100.00 SAR (10000 minor units)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
		VALUES ($1, $2, 10000, 'SAR', true)
	`, uuid.NewString(), offerID)
	if err != nil {
		t.Fatalf("seed wholesale price: %v", err)
	}

	// 3. Seed Dropship Listing backed by offer
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status)
		VALUES ($1, $2, $3, $4, 'SA', 'draft')
	`, dropshipListingID, storeID, productID, offerID)
	if err != nil {
		t.Fatalf("seed dropship listing: %v", err)
	}

	// Seller-owned listing without offer
	ownedProductID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status)
		VALUES ($1, 'owned-product', 'active')
	`, ownedProductID)
	if err != nil {
		t.Fatalf("seed owned product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status)
		VALUES ($1, $2, $3, NULL, 'SA', 'draft')
	`, ownedListingID, storeID, ownedProductID)
	if err != nil {
		t.Fatalf("seed owned listing: %v", err)
	}

	// Scenario A: Set retail price 120.00 SAR (>= wholesale cost) -> 200 OK
	t.Run("Safe margin update succeeds", func(t *testing.T) {
		res, err := svc.SetListingPrice(ctx, storeID, dropshipListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 12000,
			AllowSubWholesale:     false,
		}, false)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if res.RetailPriceMinorUnits != 12000 {
			t.Errorf("expected retail 12000, got %d", res.RetailPriceMinorUnits)
		}
		if res.WholesalePriceMinorUnits != 10000 {
			t.Errorf("expected wholesale 10000, got %d", res.WholesalePriceMinorUnits)
		}
		if res.MarginPercentage != 20.0 {
			t.Errorf("expected margin 20.0, got %f", res.MarginPercentage)
		}
		if res.SubWholesaleOverrideActive {
			t.Errorf("expected override to be false")
		}
	})

	// Scenario B: Set retail price 80.00 SAR (< wholesale cost) without override -> ErrUnsafeMargin
	t.Run("Sub-wholesale price without override returns ErrUnsafeMargin", func(t *testing.T) {
		_, err := svc.SetListingPrice(ctx, storeID, dropshipListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 8000,
			AllowSubWholesale:     false,
		}, false)
		if !errors.Is(err, listings.ErrUnsafeMargin) {
			t.Fatalf("expected ErrUnsafeMargin, got %v", err)
		}
	})

	// Scenario C: Set retail price 80.00 SAR with override but non-owner -> ErrOwnerOverrideRequired
	t.Run("Sub-wholesale price with non-owner returns ErrOwnerOverrideRequired", func(t *testing.T) {
		_, err := svc.SetListingPrice(ctx, storeID, dropshipListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 8000,
			AllowSubWholesale:     true,
			AuditReason:           "clearance",
		}, false) // isOwner = false
		if !errors.Is(err, listings.ErrOwnerOverrideRequired) {
			t.Fatalf("expected ErrOwnerOverrideRequired, got %v", err)
		}
	})

	// Scenario D: Set retail price 80.00 SAR with override, owner, but no reason -> ErrInvalidInput
	t.Run("Sub-wholesale price with owner but empty reason returns ErrInvalidInput", func(t *testing.T) {
		_, err := svc.SetListingPrice(ctx, storeID, dropshipListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 8000,
			AllowSubWholesale:     true,
			AuditReason:           "   ",
		}, true) // isOwner = true
		if !errors.Is(err, listings.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})

	// Scenario E: Set retail price 80.00 SAR with override, owner, and valid reason -> 200 OK
	t.Run("Sub-wholesale price with owner and reason succeeds", func(t *testing.T) {
		res, err := svc.SetListingPrice(ctx, storeID, dropshipListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 8000,
			AllowSubWholesale:     true,
			AuditReason:           "Clearance sale liquidation",
		}, true) // isOwner = true
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if res.RetailPriceMinorUnits != 8000 {
			t.Errorf("expected retail 8000, got %d", res.RetailPriceMinorUnits)
		}
		if res.MarginPercentage != -20.0 {
			t.Errorf("expected margin -20.0, got %f", res.MarginPercentage)
		}
		if !res.SubWholesaleOverrideActive {
			t.Errorf("expected SubWholesaleOverrideActive to be true")
		}
	})

	// Scenario F: Seller-owned product without wholesale cost allows setting price without guardrail
	t.Run("Seller-owned product allows price update without wholesale constraint", func(t *testing.T) {
		res, err := svc.SetListingPrice(ctx, storeID, ownedListingID, listings.ListingPricingRequest{
			RetailPriceMinorUnits: 5000,
			AllowSubWholesale:     false,
		}, false)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if res.RetailPriceMinorUnits != 5000 {
			t.Errorf("expected retail 5000, got %d", res.RetailPriceMinorUnits)
		}
		if res.WholesalePriceMinorUnits != 0 {
			t.Errorf("expected wholesale 0, got %d", res.WholesalePriceMinorUnits)
		}
		if res.MarginPercentage != 100.0 {
			t.Errorf("expected margin 100.0, got %f", res.MarginPercentage)
		}
	})
}
