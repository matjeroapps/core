package commerce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/money"
)

func setupSellerCatalogTestDB(t *testing.T) (*database.Pool, Service, Repository, string) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}

	db := testdb.Open(t, dsn)
	migrations := []string{
		"000001_event_delivery_foundation.up.sql",
		"000002_market_reference_data.up.sql",
		"000003_commerce_domain_schema.up.sql",
		"000004_admin_supplier_seller_platforms.up.sql",
		"000005_store_domain_lifecycle.up.sql",
		"000006_store_domain_integrity.up.sql",
		"000007_theme_engine_schema.up.sql",
		"000008_storefront_revisions.up.sql",
		"000009_supplier_retail_capability.up.sql",
		"000010_customer_cart_domain.up.sql",
		"000011_checkout_sessions.up.sql",
		"000012_order_aggregate_schema.up.sql",
		"000013_outbox_publish_claims.up.sql",
		"000014_seller_catalog_authoring.up.sql",
	}

	for _, m := range migrations {
		path := filepath.Join("..", "..", "migrations", m)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", m, err)
		}
		if _, err := db.Pool.Exec(context.Background(), string(content)); err != nil {
			t.Fatalf("failed to execute migration %s: %v", m, err)
		}
	}

	repo := NewRepository(db.Pool)
	service := NewService(repo)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	return db, service, repo, suffix
}

func TestFirstLiveProductAndOrderCoreIntegration(t *testing.T) {
	_, service, repo, suffix := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	// 1. Create Seller & Store
	seller, err := repo.CreateSeller(ctx, "seller-live-"+suffix, "MatjerHub First Store Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "user-seller-live-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := repo.CreateStore(ctx, seller.ID, "EG", "store-live-"+suffix, "First Live Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	// 2. Category
	cat, err := repo.CreateCategory(ctx, "beverages-"+suffix, nil, "active")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	// 3. Create Product Atomically
	draft := SellerProductDraft{
		Slug: "arabic-coffee-" + suffix,
		Translations: []ProductTranslation{
			{Locale: "en", Name: "Premium Arabic Coffee", Description: "Authentic roasted coffee beans"},
			{Locale: "ar", Name: "قهوة عربية فاخرة", Description: "بن محمص فاخر"},
		},
		CategoryIDs: []string{cat.ID},
	}
	detail, err := service.CreateSellerProductForSubject(ctx, subject, store.ID, draft)
	if err != nil {
		t.Fatalf("CreateSellerProductForSubject: %v", err)
	}

	productID := detail.Product.ID

	// 4. Variant & SKU
	variant, err := service.CreateVariantForSubject(ctx, subject, store.ID, productID, "500g", "active")
	if err != nil {
		t.Fatalf("CreateVariantForSubject: %v", err)
	}
	sku, err := service.CreateSKUForSubject(ctx, subject, store.ID, productID, variant.ID, "SKU-COF-500G", "1234567890", "active")
	if err != nil {
		t.Fatalf("CreateSKUForSubject: %v", err)
	}

	// 5. Media
	mediaUpload, err := service.CompleteMediaUploadForSubject(ctx, subject, store.ID, productID, CompleteMediaUploadRequest{
		StorageKey: fmt.Sprintf("products/%s/%s/img1.webp", seller.ID, productID),
		AltText:    "Coffee Bag Front",
		SortOrder:  1,
		IsPrimary:  true,
	})
	if err != nil {
		t.Fatalf("CompleteMediaUploadForSubject: %v", err)
	}
	if !mediaUpload.IsPrimary {
		t.Fatalf("Expected media to be primary")
	}

	// 6. Retail Price
	priceObj, _ := money.New(25000, "EGP") // 250.00 EGP
	if _, err := repo.SetSellerListingPrice(ctx, detail.Listing.ID, priceObj); err != nil {
		t.Fatalf("SetSellerListingPrice: %v", err)
	}

	// 7. Seller Location & Inventory Snapshot
	loc, err := service.CreateStoreFulfillmentLocationForSubject(ctx, subject, store.ID, "loc-main", "Main Store Warehouse", "warehouse", "active")
	if err != nil {
		t.Fatalf("CreateStoreFulfillmentLocationForSubject: %v", err)
	}
	snap, err := service.CreateInventorySnapshotForSubject(ctx, subject, store.ID, loc.ID, sku.ID, 50)
	if err != nil {
		t.Fatalf("CreateInventorySnapshotForSubject: %v", err)
	}

	// 8. Presentation
	pres := SellerListingPresentation{
		SellerListingID:  detail.Listing.ID,
		SchemaVersion:    1,
		PurchaseBehavior: "buy_now",
		Sections: []ProductPageSection{
			{
				ID:        "sec-1",
				Type:      "highlights",
				Enabled:   true,
				SortOrder: 1,
				Content: map[string]any{
					"en": map[string]any{"title": "Highlights", "items": []any{"100% Arabica", "Freshly Roasted"}},
					"ar": map[string]any{"title": "المميزات", "items": []any{"100% أرابيكا", "محمص طازج"}},
				},
			},
		},
	}
	if _, err := service.UpdateListingPresentationForSubject(ctx, subject, store.ID, detail.Listing.ID, pres); err != nil {
		t.Fatalf("UpdateListingPresentationForSubject: %v", err)
	}

	// 9. Publish Product
	if err := service.PublishSellerProductForSubject(ctx, subject, store.ID, productID); err != nil {
		t.Fatalf("PublishSellerProductForSubject: %v", err)
	}

	// 10. Verify Readiness & Published Detail
	pubDetail, err := service.GetSellerProductDetailForSubject(ctx, subject, store.ID, productID)
	if err != nil {
		t.Fatalf("GetSellerProductDetailForSubject after publish: %v", err)
	}
	if !pubDetail.PublishReadiness.IsReady {
		t.Fatalf("Expected product to be ready for publish, reasons: %v", pubDetail.PublishReadiness.Reasons)
	}
	if pubDetail.Listing.Status != "active" {
		t.Fatalf("Expected listing status active, got %s", pubDetail.Listing.Status)
	}
	if pubDetail.PurchaseBehavior != "buy_now" {
		t.Fatalf("Expected effective purchase behavior buy_now, got %s", pubDetail.PurchaseBehavior)
	}

	// 11. Core Customer Order Flow
	cart, token, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	cart, err = repo.AddCartItem(ctx, store.ID, token, sku.ID, 2)
	if err != nil {
		t.Fatalf("AddCartItem: %v", err)
	}
	if len(cart.Items) == 0 {
		t.Fatalf("Expected cart items")
	}

	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, token, nil, 30*time.Minute)
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}

	order, err := repo.FinalizeCheckout(ctx, store.ID, FinalizeRequest{
		SessionID: session.ID,
		ShippingAddress: ShippingAddress{
			RecipientName: "Ahmed Customer",
			AddressLine1:  "123 Main St",
			City:          "Cairo",
			CountryCode:   "EG",
		},
		ContactEmail: "customer@example.test",
	}, "corr-test")
	if err != nil {
		t.Fatalf("FinalizeCheckout: %v", err)
	}
	if order.Status != "pending" {
		t.Fatalf("Expected order status pending, got %s", order.Status)
	}

	// 12. Seller Order Lifecycle Transitions
	// Seller lists orders
	orders, total, err := service.ListStoreOrdersForSubject(ctx, subject, store.ID, "", 10, 0)
	if err != nil || total == 0 || len(orders) == 0 {
		t.Fatalf("ListStoreOrdersForSubject failed, total: %d, err: %v", total, err)
	}
	orderID := orders[0].ID

	// Confirm Order: pending -> confirmed
	confirmedOrder, err := service.TransitionStoreOrderForSubject(ctx, subject, store.ID, orderID, "confirmed", nil, "corr-1")
	if err != nil {
		t.Fatalf("Transition to confirmed: %v", err)
	}
	if confirmedOrder.Status != "confirmed" {
		t.Fatalf("Expected confirmed status, got %s", confirmedOrder.Status)
	}

	// Start Processing: confirmed -> processing
	processingOrder, err := service.TransitionStoreOrderForSubject(ctx, subject, store.ID, orderID, "processing", nil, "corr-2")
	if err != nil {
		t.Fatalf("Transition to processing: %v", err)
	}
	if processingOrder.Status != "processing" {
		t.Fatalf("Expected processing status, got %s", processingOrder.Status)
	}

	// Ready for Shipping: processing -> ready_for_shipping
	readyOrder, err := service.TransitionStoreOrderForSubject(ctx, subject, store.ID, orderID, "ready_for_shipping", nil, "corr-3")
	if err != nil {
		t.Fatalf("Transition to ready_for_shipping: %v", err)
	}
	if readyOrder.Status != "ready_for_shipping" {
		t.Fatalf("Expected ready_for_shipping status, got %s", readyOrder.Status)
	}

	// Shipping transition beyond ready_for_shipping must fail
	if _, err := service.TransitionStoreOrderForSubject(ctx, subject, store.ID, orderID, "shipped", nil, "corr-4"); err == nil {
		t.Fatalf("Expected shipped transition to be rejected")
	}

	// Verify inventory snapshot reserved/on-hand
	snapAfter, err := repo.GetInventorySnapshot(ctx, snap.ID)
	if err != nil {
		t.Fatalf("GetInventorySnapshot after order: %v", err)
	}
	if snapAfter.OnHandQty != 48 { // 50 - 2 consumed upon confirm
		t.Fatalf("Expected on_hand_qty 48 after order confirm, got %d", snapAfter.OnHandQty)
	}
}

func TestSellerProductTenantIsolationAndSecurity(t *testing.T) {
	_, service, repo, suffix := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	// Seller A & Store A
	sellerA, _ := repo.CreateSeller(ctx, "seller-a-"+suffix, "Seller A", "active", nil)
	subjectA := "user-seller-a-" + suffix
	_, _ = repo.CreateSellerMember(ctx, sellerA.ID, subjectA, "owner", "active")
	storeA, _ := repo.CreateStore(ctx, sellerA.ID, "EG", "store-a-"+suffix, "Store A", "active", nil)

	// Seller B & Store B
	sellerB, _ := repo.CreateSeller(ctx, "seller-b-"+suffix, "Seller B", "active", nil)
	subjectB := "user-seller-b-" + suffix
	_, _ = repo.CreateSellerMember(ctx, sellerB.ID, subjectB, "owner", "active")
	storeB, _ := repo.CreateStore(ctx, sellerB.ID, "EG", "store-b-"+suffix, "Store B", "active", nil)

	// Seller A creates a product
	draftA := SellerProductDraft{
		Slug:         "prod-a-" + suffix,
		Translations: []ProductTranslation{{Locale: "en", Name: "Product A"}},
	}
	detailA, err := service.CreateSellerProductForSubject(ctx, subjectA, storeA.ID, draftA)
	if err != nil {
		t.Fatalf("Seller A create product: %v", err)
	}

	// Security Test 1: Seller B cannot read Seller A's product (returns ErrNotFound)
	if _, err := service.GetSellerProductDetailForSubject(ctx, subjectB, storeB.ID, detailA.Product.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Expected ErrNotFound when Seller B reads Seller A product, got %v", err)
	}

	// Security Test 2: Seller B cannot update Seller A's product (returns ErrNotFound)
	if _, err := service.UpdateSellerProductForSubject(ctx, subjectB, storeB.ID, detailA.Product.ID, "hacked-slug", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Expected ErrNotFound when Seller B updates Seller A product, got %v", err)
	}

	// Security Test 3: Seller B cannot hijack Seller A's product into a seller-owned listing
	if _, err := service.CreateSellerListingForSubject(ctx, subjectB, storeB.ID, detailA.Product.ID, nil, "EG", "active"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Expected ErrNotFound when Seller B creates seller-owned listing for Seller A product, got %v", err)
	}
}
