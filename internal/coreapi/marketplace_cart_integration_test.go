package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"core/internal/marketplace"
	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

type marketplaceCartFixture struct {
	db             *database.Pool
	repo           commerce.Repository
	marketplaceSvc marketplace.Service
	handler        http.Handler
	sellerID       string
	storeAEG       string
	storeBEG       string
	storeCSA       string
	listingA1EG    string
	skuA1EG        string
	listingA2EG    string
	skuA2EG        string
	listingB1EG    string
	skuB1EG        string
	listingC1SA    string
	skuC1SA        string
	unpubListingEG string
	noStockListing string
}

func setupMarketplaceCartFixture(t *testing.T) marketplaceCartFixture {
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

	ctx := context.Background()
	sellerID := uuid.NewString()
	storeAEG := uuid.NewString()
	storeBEG := uuid.NewString()
	storeCSA := uuid.NewString()

	if _, err := db.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status) VALUES
			($1, $2, 'Marketplace Seller', 'active')
	`, sellerID, "mp-seller-"+sellerID[:8]); err != nil {
		t.Fatalf("insert seller: %v", err)
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status) VALUES
			($1, $2, 'EG', $3, 'Store A Egypt', 'active'),
			($4, $2, 'EG', $5, 'Store B Egypt', 'active'),
			($6, $2, 'SA', $7, 'Store C Saudi', 'active')
	`, storeAEG, sellerID, "store-a-"+storeAEG[:8],
		storeBEG, "store-b-"+storeBEG[:8],
		storeCSA, "store-c-"+storeCSA[:8]); err != nil {
		t.Fatalf("insert stores: %v", err)
	}

	insertListingWithInventory := func(name, slug, market, storeID, listingStatus, productStatus string, priceMinor int64, currency string, stockQty int64) (string, string) {
		t.Helper()
		productID := uuid.NewString()
		variantID := uuid.NewString()
		skuID := uuid.NewString()
		listingID := uuid.NewString()
		locationID := uuid.NewString()

		if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, $3)`, productID, slug, productStatus); err != nil {
			t.Fatalf("insert product %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', $2)`, productID, name); err != nil {
			t.Fatalf("insert product_translations %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "var-"+slug); err != nil {
			t.Fatalf("insert variant %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "sku-"+slug); err != nil {
			t.Fatalf("insert sku %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO seller_listings (id, store_id, product_id, market_code, status, created_at)
			VALUES ($1, $2, $3, $4, $5, now())
		`, listingID, storeID, productID, market, listingStatus); err != nil {
			t.Fatalf("insert listing %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current)
			VALUES ($1, $2, $3, $4, true)
		`, uuid.NewString(), listingID, priceMinor, currency); err != nil {
			t.Fatalf("insert price %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
			VALUES ($1, $2, $3, $4, $5, 'warehouse', 'active')
		`, locationID, storeID, market, "loc-"+uuid.NewString()[:8], "Location "+name); err != nil {
			t.Fatalf("insert location %s: %v", slug, err)
		}
		if _, err := db.Exec(ctx, `
			INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty)
			VALUES ($1, $2, $3, $4, 0)
		`, uuid.NewString(), locationID, skuID, stockQty); err != nil {
			t.Fatalf("insert snapshot %s: %v", slug, err)
		}
		return listingID, skuID
	}

	listA1, skuA1 := insertListingWithInventory("Store A Product 1", "store-a-p1-"+uuid.NewString()[:8], "EG", storeAEG, "published", "active", 150000, "EGP", 50)
	listA2, skuA2 := insertListingWithInventory("Store A Product 2", "store-a-p2-"+uuid.NewString()[:8], "EG", storeAEG, "published", "active", 80000, "EGP", 30)
	listB1, skuB1 := insertListingWithInventory("Store B Product 1", "store-b-p1-"+uuid.NewString()[:8], "EG", storeBEG, "published", "active", 200000, "EGP", 20)
	listC1, skuC1 := insertListingWithInventory("Store C Product 1", "store-c-p1-"+uuid.NewString()[:8], "SA", storeCSA, "published", "active", 35000, "SAR", 40)
	unpubList, _ := insertListingWithInventory("Unpublished Product", "unpub-"+uuid.NewString()[:8], "EG", storeAEG, "draft", "active", 50000, "EGP", 10)
	noStockList, _ := insertListingWithInventory("No Stock Product", "no-stock-"+uuid.NewString()[:8], "EG", storeAEG, "published", "active", 60000, "EGP", 0)

	repo := commerce.NewRepository(db.Pool)
	commerceSvc := commerce.NewService(repo)
	marketRepo := marketplace.NewRepository(db.Pool)
	marketplaceSvc := marketplace.NewService(marketRepo)

	deps := Dependencies{
		Commerce:    commerceSvc,
		Repo:        repo,
		Marketplace: marketplaceSvc,
	}
	handler := newTestRouter(deps)

	return marketplaceCartFixture{
		db:             db,
		repo:           repo,
		marketplaceSvc: marketplaceSvc,
		handler:        handler,
		sellerID:       sellerID,
		storeAEG:       storeAEG,
		storeBEG:       storeBEG,
		storeCSA:       storeCSA,
		listingA1EG:    listA1,
		skuA1EG:        skuA1,
		listingA2EG:    listA2,
		skuA2EG:        skuA2,
		listingB1EG:    listB1,
		skuB1EG:        skuB1,
		listingC1SA:    listC1,
		skuC1SA:        skuC1,
		unpubListingEG: unpubList,
		noStockListing: noStockList,
	}
}

func TestMarketplaceCartHandoffIntegration_FullFlow(t *testing.T) {
	fixture := setupMarketplaceCartFixture(t)

	// Step 1: Create a new marketplace cart item (no cart token provided)
	sourceCol := "fast_delivery"
	reqBody1, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID:  fixture.listingA1EG,
		Quantity:         2,
		SourceCollection: &sourceCol,
	})
	httpReq1 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(reqBody1))
	httpReq1.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq1.Header.Set("X-Matjero-Service", "platform")
	httpReq1.Header.Set("Content-Type", "application/json")

	rec1 := doRequest(t, fixture.handler, httpReq1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("step 1 status = %d, want %d (body: %s)", rec1.Code, http.StatusOK, rec1.Body.String())
	}

	var resp1 MarketplaceCartHandoffResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("step 1 unmarshal response: %v", err)
	}

	if resp1.CartID == "" || resp1.CartToken == "" {
		t.Fatalf("step 1 expected non-empty cart_id and cart_token, got id=%q token=%q", resp1.CartID, resp1.CartToken)
	}
	if resp1.StoreID != fixture.storeAEG {
		t.Fatalf("step 1 store_id = %q, want %q", resp1.StoreID, fixture.storeAEG)
	}
	if resp1.MarketCode != "EG" {
		t.Fatalf("step 1 market_code = %q, want EG", resp1.MarketCode)
	}
	if len(resp1.Items) != 1 {
		t.Fatalf("step 1 items length = %d, want 1", len(resp1.Items))
	}
	if resp1.Items[0].SellerListingID != fixture.listingA1EG || resp1.Items[0].Quantity != 2 || resp1.Items[0].UnitPriceMinor != 150000 || resp1.Items[0].CurrencyCode != "EGP" {
		t.Fatalf("step 1 unexpected item content: %+v", resp1.Items[0])
	}
	if resp1.Attribution == nil || resp1.Attribution.SellerListingID != fixture.listingA1EG || resp1.Attribution.StoreID != fixture.storeAEG || resp1.Attribution.SourceCollection == nil || *resp1.Attribution.SourceCollection != "fast_delivery" {
		t.Fatalf("step 1 unexpected attribution: %+v", resp1.Attribution)
	}

	cartToken := resp1.CartToken

	// Step 2: Add a second item to the same store's cart using cart_token
	reqBody2, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID:  fixture.listingA2EG,
		Quantity:         1,
		CartToken:        cartToken,
		SourceCollection: &sourceCol,
	})
	httpReq2 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(reqBody2))
	httpReq2.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq2.Header.Set("X-Matjero-Service", "platform")
	httpReq2.Header.Set("Content-Type", "application/json")

	rec2 := doRequest(t, fixture.handler, httpReq2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("step 2 status = %d, want %d (body: %s)", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	var resp2 MarketplaceCartHandoffResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("step 2 unmarshal response: %v", err)
	}

	if resp2.CartID != resp1.CartID || resp2.CartToken != cartToken {
		t.Fatalf("step 2 cart identity changed: got id=%q token=%q", resp2.CartID, resp2.CartToken)
	}
	if len(resp2.Items) != 2 {
		t.Fatalf("step 2 items length = %d, want 2", len(resp2.Items))
	}

	// Step 3: Attempt to add item from Store B into Store A's cart -> Reject with 409 Conflict (store mismatch)
	reqBody3, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID: fixture.listingB1EG,
		Quantity:        1,
		CartToken:       cartToken,
	})
	httpReq3 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(reqBody3))
	httpReq3.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq3.Header.Set("X-Matjero-Service", "platform")
	httpReq3.Header.Set("Content-Type", "application/json")

	rec3 := doRequest(t, fixture.handler, httpReq3)
	if rec3.Code != http.StatusConflict {
		t.Fatalf("step 3 status = %d, want %d", rec3.Code, http.StatusConflict)
	}
	if errCode := decodeError(t, rec3).Error.Code; errCode != CodeConflict {
		t.Fatalf("step 3 error code = %q, want %q", errCode, CodeConflict)
	}

	// Step 4: Attempt to add item from Store C (Saudi SA) into Store A's cart (Egypt EG) -> Reject with 409 Conflict (market mismatch)
	reqBody4, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID: fixture.listingC1SA,
		Quantity:        1,
		CartToken:       cartToken,
	})
	httpReq4 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/SA/marketplace/carts/items", bytes.NewReader(reqBody4))
	httpReq4.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq4.Header.Set("X-Matjero-Service", "platform")
	httpReq4.Header.Set("Content-Type", "application/json")

	rec4 := doRequest(t, fixture.handler, httpReq4)
	if rec4.Code != http.StatusConflict {
		t.Fatalf("step 4 status = %d, want %d", rec4.Code, http.StatusConflict)
	}
	if errCode := decodeError(t, rec4).Error.Code; errCode != CodeMarketMismatch {
		t.Fatalf("step 4 error code = %q, want %q", errCode, CodeMarketMismatch)
	}

	// Step 5: Unpublished listing rejected and does not mutate cart
	reqBody5, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID: fixture.unpubListingEG,
		Quantity:        1,
		CartToken:       cartToken,
	})
	httpReq5 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(reqBody5))
	httpReq5.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq5.Header.Set("X-Matjero-Service", "platform")
	httpReq5.Header.Set("Content-Type", "application/json")

	rec5 := doRequest(t, fixture.handler, httpReq5)
	if rec5.Code != http.StatusConflict {
		t.Fatalf("step 5 status = %d, want %d", rec5.Code, http.StatusConflict)
	}
	if errCode := decodeError(t, rec5).Error.Code; errCode != CodeListingUnavailable {
		t.Fatalf("step 5 error code = %q, want %q", errCode, CodeListingUnavailable)
	}

	// Step 6: Out of stock listing rejected and does not create a cart
	var countBefore int64
	_ = fixture.db.QueryRow(context.Background(), `SELECT count(*) FROM carts`).Scan(&countBefore)

	reqBody6, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID: fixture.noStockListing,
		Quantity:        1,
	})
	httpReq6 := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(reqBody6))
	httpReq6.Header.Set("Authorization", "Bearer "+testPlatformToken)
	httpReq6.Header.Set("X-Matjero-Service", "platform")
	httpReq6.Header.Set("Content-Type", "application/json")

	rec6 := doRequest(t, fixture.handler, httpReq6)
	if rec6.Code != http.StatusConflict {
		t.Fatalf("step 6 status = %d, want %d", rec6.Code, http.StatusConflict)
	}
	if errCode := decodeError(t, rec6).Error.Code; errCode != CodeInsufficientInventory {
		t.Fatalf("step 6 error code = %q, want %q", errCode, CodeInsufficientInventory)
	}

	var countAfter int64
	_ = fixture.db.QueryRow(context.Background(), `SELECT count(*) FROM carts`).Scan(&countAfter)
	if countAfter != countBefore {
		t.Fatalf("step 6 cart count changed from %d to %d on failed resolution", countBefore, countAfter)
	}

	// Step 7: Create checkout session for the marketplace cart and finalize checkout with attribution
	session, _, err := fixture.repo.CreateCheckoutSession(context.Background(), fixture.storeAEG, cartToken, nil, 30*time.Minute)
	if err != nil {
		t.Fatalf("create checkout session: %v", err)
	}

	finalReq := commerce.FinalizeRequest{
		SessionID: session.ID,
		ShippingAddress: commerce.ShippingAddress{
			RecipientName: "Ahmed Ali",
			AddressLine1:  "123 Nile Street",
			City:          "Cairo",
			CountryCode:   "EG",
		},
		ContactEmail: "ahmed@example.com",
		Attribution: &commerce.MarketplaceAttributionInput{
			SellerListingID:  fixture.listingA1EG,
			StoreID:          fixture.storeAEG,
			MarketCode:       "EG",
			SourceCollection: &sourceCol,
		},
	}

	order, err := fixture.repo.FinalizeCheckout(context.Background(), fixture.storeAEG, finalReq, "test-correlation-1")
	if err != nil {
		t.Fatalf("finalize checkout: %v", err)
	}

	if order.Attribution == nil {
		t.Fatalf("expected order attribution to be persisted, got nil")
	}
	if order.Attribution.SellerListingID != fixture.listingA1EG || order.Attribution.StoreID != fixture.storeAEG || order.Attribution.MarketCode != "EG" || order.Attribution.SourceCollection == nil || *order.Attribution.SourceCollection != "fast_delivery" {
		t.Fatalf("unexpected order attribution: %+v", order.Attribution)
	}

	// Step 8: Verify idempotency replay returns identical order with immutable attribution
	replayOrder, err := fixture.repo.FinalizeCheckout(context.Background(), fixture.storeAEG, finalReq, "test-correlation-2")
	if err != nil {
		t.Fatalf("replay checkout: %v", err)
	}
	if replayOrder.ID != order.ID {
		t.Fatalf("replay order id = %q, want %q", replayOrder.ID, order.ID)
	}
	if replayOrder.Attribution == nil || replayOrder.Attribution.ID != order.Attribution.ID {
		t.Fatalf("replay attribution mismatch: got %+v, want %+v", replayOrder.Attribution, order.Attribution)
	}
}
