package commerce

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"

	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupP53Database(t *testing.T) (*database.Pool, Repository, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	for _, name := range []string{
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
	} {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	return db, NewRepository(db.Pool), context.Background()
}

func createTestStoreAndSession(t *testing.T, db *database.Pool, repo Repository, ctx context.Context, suffix string) (Store, Cart, CheckoutSession, string) {
	t.Helper()
	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, rawCapability, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return store, cart, session, rawCapability
}

func createTestLocationAndReservation(t *testing.T, db *database.Pool, ctx context.Context, storeID, skuID string) (string, string) {
	t.Helper()
	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Test Store Location', 'warehouse', 'active')
	`, locID, storeID, "LOC-"+locID[:8]); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, 10, 1, 1)
	`, snapID, skuID, locID); err != nil {
		t.Fatal(err)
	}

	resID := uuid.NewString()
	resToken := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at)
		VALUES ($1, $2, $3, 'held', 1, clock_timestamp() + interval '30 minutes')
	`, resID, resToken, snapID); err != nil {
		t.Fatal(err)
	}

	return locID, resID
}

func TestP53StoreOrderSequenceAllocationConcurrency(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffixA := uuid.NewString()
	suffixB := uuid.NewString()
	storeA, _, _, _ := createTestStoreAndSession(t, db, repo, ctx, suffixA)
	storeB, _, _, _ := createTestStoreAndSession(t, db, repo, ctx, suffixB)

	const n = 10
	var wg sync.WaitGroup
	results := make(chan string, n)

	startBarrier := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			num, err := repo.AllocateOrderNumber(ctx, nil, storeA.ID)
			if err != nil {
				t.Errorf("AllocateOrderNumber error: %v", err)
				return
			}
			results <- num
		}()
	}

	close(startBarrier)
	wg.Wait()
	close(results)

	allocated := make(map[string]bool)
	for num := range results {
		if allocated[num] {
			t.Fatalf("duplicate order number allocated for storeA: %s", num)
		}
		allocated[num] = true
	}

	if len(allocated) != n {
		t.Fatalf("expected %d unique allocations, got %d", n, len(allocated))
	}

	for i := 100001; i <= 100000+n; i++ {
		expected := fmt.Sprintf("#%d", i)
		if !allocated[expected] {
			t.Fatalf("missing expected allocated order number: %s", expected)
		}
	}

	numB, err := repo.AllocateOrderNumber(ctx, nil, storeB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if numB != "#100001" {
		t.Fatalf("expected storeB sequence to start at #100001, got %s", numB)
	}
}

func TestP53OrderConstraintsAndTenantIsolation(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffixA := uuid.NewString()
	suffixB := uuid.NewString()
	storeA, _, sessionA, _ := createTestStoreAndSession(t, db, repo, ctx, suffixA)
	storeB, _, sessionB, _ := createTestStoreAndSession(t, db, repo, ctx, suffixB)

	numA, err := repo.AllocateOrderNumber(ctx, nil, storeA.ID)
	if err != nil {
		t.Fatal(err)
	}

	digestA := sessionA.GuestOrderAccessTokenDigest
	orderIDA := uuid.NewString()
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)

	validOrderA := Order{
		ID:                          orderIDA,
		OrderNumber:                 numA,
		StoreID:                     storeA.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           sessionA.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: digestA,
		SubtotalMinor:               1500,
		TotalMinor:                  1500,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	created, err := repo.CreateOrder(ctx, nil, validOrderA)
	if err != nil {
		t.Fatalf("failed to create valid order: %v", err)
	}
	if created.ID != orderIDA || created.OrderNumber != numA {
		t.Fatalf("created order mismatch: %+v", created)
	}

	// 1. Duplicate (store_id, order_number) rejected
	dupOrderNumber := validOrderA
	dupOrderNumber.ID = uuid.NewString()
	_, cartTokenA2, _ := repo.CreateCart(ctx, storeA.ID, "EG", nil)
	sessionA2, _, _ := repo.CreateCheckoutSession(ctx, storeA.ID, cartTokenA2, nil, time.Hour)
	dupOrderNumber.CheckoutSessionID = sessionA2.ID
	dupOrderNumber.GuestOrderAccessTokenDigest = sessionA2.GuestOrderAccessTokenDigest

	if _, err := repo.CreateOrder(ctx, nil, dupOrderNumber); err == nil {
		t.Fatal("expected duplicate (store_id, order_number) to be rejected, got nil error")
	}

	// 2. Same order_number allowed in Store B
	numB, _ := repo.AllocateOrderNumber(ctx, nil, storeB.ID)
	orderInStoreB := Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 numB,
		StoreID:                     storeB.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           sessionB.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: sessionB.GuestOrderAccessTokenDigest,
		SubtotalMinor:               1500,
		TotalMinor:                  1500,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}
	if _, err := repo.CreateOrder(ctx, nil, orderInStoreB); err != nil {
		t.Fatalf("expected same order number in storeB to succeed, got %v", err)
	}

	// 3. Duplicate checkout_session_id rejected
	dupSessionOrder := validOrderA
	dupSessionOrder.ID = uuid.NewString()
	dupSessionOrder.OrderNumber = "#100002"
	if _, err := repo.CreateOrder(ctx, nil, dupSessionOrder); err == nil {
		t.Fatal("expected duplicate checkout_session_id to be rejected, got nil error")
	}

	// 4. Checkout Session from another store rejected
	sessionMismatchOrder := Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 "#100003",
		StoreID:                     storeA.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           sessionB.ID, // Belongs to Store B!
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: digestA,
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      deadline,
	}
	if _, err := repo.CreateOrder(ctx, nil, sessionMismatchOrder); err == nil {
		t.Fatal("expected checkout session from another store to be rejected, got nil error")
	}

	// 5. Guest order with neither customer_id nor capability digest rejected
	noDigestGuestOrder := Order{
		ID:                     uuid.NewString(),
		OrderNumber:            "#100004",
		StoreID:                storeA.ID,
		MarketCode:             "EG",
		CheckoutSessionID:      sessionA2.ID,
		Status:                 OrderStatusPending,
		CurrencyCode:           "EGP",
		SubtotalMinor:          1000,
		TotalMinor:             1000,
		ConfirmationDeadlineAt: deadline,
	}
	if _, err := repo.CreateOrder(ctx, nil, noDigestGuestOrder); err == nil {
		t.Fatal("expected guest order without digest to be rejected, got nil error")
	}
}

func TestP53OrderItemConstraintsAndCurrencyIntegrity(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, _, session, _ := createTestStoreAndSession(t, db, repo, ctx, suffix)

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()

	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}

	locID, resID := createTestLocationAndReservation(t, db, ctx, store.ID, skuID)
	orderNumber, _ := repo.AllocateOrderNumber(ctx, nil, store.ID)
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)

	baseOrder := Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 orderNumber,
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	// 1. Order Item currency mismatch rejected by PostgreSQL (order_id, currency_code) composite FK!
	mismatchedCurrencyOrder := baseOrder
	mismatchedCurrencyOrder.Items = []OrderItem{
		{
			ID:                     uuid.NewString(),
			FulfillmentLocationID:  locID,
			InventoryReservationID: resID,
			ProductTitleSnapshot:   "Prod",
			SKUCodeSnapshot:        "SKU-CODE",
			UnitPriceMinor:         1000,
			CurrencyCode:           "USD", // Mismatch with EGP!
			Quantity:               1,
			LineTotalMinor:         1000,
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, mismatchedCurrencyOrder); err == nil {
		t.Fatal("expected Order Item currency mismatch to be rejected by PG FK, got nil error")
	}

	// 2. Half-populated Supplier snapshot rejected
	supplierID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO suppliers (id, code, name, status) VALUES ($1, $2, 'Supp', 'active')`, supplierID, "SUPP-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	suppMarketID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_markets (id, supplier_id, market_code, status) VALUES ($1, $2, 'EG', 'active')`, suppMarketID, supplierID); err != nil {
		t.Fatal(err)
	}
	suppProdID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status) VALUES ($1, $2, $3, 'SUPP-CODE', 'active')`, suppProdID, supplierID, productID); err != nil {
		t.Fatal(err)
	}
	offerID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status) VALUES ($1, $2, $3, $4, 'EG', 'active')`, offerID, supplierID, suppProdID, suppMarketID); err != nil {
		t.Fatal(err)
	}

	cost := int64(800)
	costCurr := "EGP"

	halfSupplierOrder := baseOrder
	halfSupplierOrder.Items = []OrderItem{
		{
			ID:                       uuid.NewString(),
			SupplierOfferID:          &offerID,
			SourceSupplierID:         nil, // Half-populated! Missing source_supplier_id!
			SupplierCostMinor:        &cost,
			SupplierCostCurrencyCode: &costCurr,
			FulfillmentLocationID:    locID,
			InventoryReservationID:   resID,
			ProductTitleSnapshot:     "Prod",
			SKUCodeSnapshot:          "SKU-CODE",
			UnitPriceMinor:           1000,
			CurrencyCode:             "EGP",
			Quantity:                 1,
			LineTotalMinor:           1000,
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, halfSupplierOrder); err == nil {
		t.Fatal("expected half-populated Supplier snapshot to be rejected, got nil error")
	}

	// 3. Valid Seller-owned line (all 4 supplier fields NULL) accepted
	sellerOwnedOrder := baseOrder
	sellerOwnedOrder.Items = []OrderItem{
		{
			ID:                     uuid.NewString(),
			FulfillmentLocationID:  locID,
			InventoryReservationID: resID,
			ProductTitleSnapshot:   "Prod",
			SKUCodeSnapshot:        "SKU-CODE",
			UnitPriceMinor:         1000,
			CurrencyCode:           "EGP",
			Quantity:               1,
			LineTotalMinor:         1000,
		},
	}
	createdSellerOwned, err := repo.CreateOrder(ctx, nil, sellerOwnedOrder)
	if err != nil {
		t.Fatalf("expected seller-owned order to succeed, got %v", err)
	}
	if len(createdSellerOwned.Items) != 1 || createdSellerOwned.Items[0].SupplierOfferID != nil {
		t.Fatalf("unexpected item supplier fields for seller-owned item")
	}

	// 4. Valid Supplier-backed line (all 4 supplier fields present) accepted
	_, cartToken2, _ := repo.CreateCart(ctx, store.ID, "EG", nil)
	session2, _, _ := repo.CreateCheckoutSession(ctx, store.ID, cartToken2, nil, time.Hour)

	orderNumber2, _ := repo.AllocateOrderNumber(ctx, nil, store.ID)
	resID2 := uuid.NewString()
	resToken2 := uuid.NewString()
	var snapID string
	if err := db.QueryRow(ctx, `SELECT id FROM inventory_snapshots WHERE fulfillment_location_id = $1 AND sku_id = $2`, locID, skuID).Scan(&snapID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at)
		VALUES ($1, $2, $3, 'held', 1, clock_timestamp() + interval '30 minutes')
	`, resID2, resToken2, snapID); err != nil {
		t.Fatal(err)
	}

	supplierBackedOrder := baseOrder
	supplierBackedOrder.ID = uuid.NewString()
	supplierBackedOrder.OrderNumber = orderNumber2
	supplierBackedOrder.CheckoutSessionID = session2.ID
	supplierBackedOrder.GuestOrderAccessTokenDigest = session2.GuestOrderAccessTokenDigest
	supplierBackedOrder.Items = []OrderItem{
		{
			ID:                       uuid.NewString(),
			SupplierOfferID:          &offerID,
			SourceSupplierID:         &supplierID,
			SupplierCostMinor:        &cost,
			SupplierCostCurrencyCode: &costCurr,
			FulfillmentLocationID:    locID,
			InventoryReservationID:   resID2,
			ProductTitleSnapshot:     "Supp Prod",
			SKUCodeSnapshot:          "SUPP-SKU",
			UnitPriceMinor:           1000,
			CurrencyCode:             "EGP",
			Quantity:                 1,
			LineTotalMinor:           1000,
		},
	}

	createdSupplierBacked, err := repo.CreateOrder(ctx, nil, supplierBackedOrder)
	if err != nil {
		t.Fatalf("expected supplier-backed order to succeed, got %v", err)
	}
	if *createdSupplierBacked.Items[0].SupplierOfferID != offerID || *createdSupplierBacked.Items[0].SourceSupplierID != supplierID {
		t.Fatalf("unexpected supplier fields in created order item")
	}

	// 5. Supplier cost currency mismatch rejected by DB CHECK
	wrongCostCurr := "USD"
	wrongCostCurrOrder := baseOrder
	wrongCostCurrOrder.ID = uuid.NewString()
	wrongCostCurrOrder.CheckoutSessionID = uuid.NewString()
	wrongCostCurrOrder.Items = []OrderItem{
		{
			ID:                       uuid.NewString(),
			SupplierOfferID:          &offerID,
			SourceSupplierID:         &supplierID,
			SupplierCostMinor:        &cost,
			SupplierCostCurrencyCode: &wrongCostCurr, // USD vs line currency EGP!
			FulfillmentLocationID:    locID,
			InventoryReservationID:   uuid.NewString(),
			ProductTitleSnapshot:     "Supp Prod",
			SKUCodeSnapshot:          "SUPP-SKU",
			UnitPriceMinor:           1000,
			CurrencyCode:             "EGP",
			Quantity:                 1,
			LineTotalMinor:           1000,
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, wrongCostCurrOrder); err == nil {
		t.Fatal("expected supplier cost currency mismatch to be rejected, got nil error")
	}
}

func TestP53OperationalLineageDeletionRestrictions(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, _, session, _ := createTestStoreAndSession(t, db, repo, ctx, suffix)

	skuID := uuid.NewString()
	listingID := uuid.NewString()
	productID := uuid.NewString()
	variantID := uuid.NewString()

	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+skuID[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, market_code, product_id, status) VALUES ($1, $2, 'EG', $3, 'active')`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}

	supplierID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO suppliers (id, code, name, status) VALUES ($1, $2, 'Supp', 'active')`, supplierID, "SUPP-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	suppMarketID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_markets (id, supplier_id, market_code, status) VALUES ($1, $2, 'EG', 'active')`, suppMarketID, supplierID); err != nil {
		t.Fatal(err)
	}
	suppProdID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status) VALUES ($1, $2, $3, 'SUPP-CODE', 'active')`, suppProdID, supplierID, productID); err != nil {
		t.Fatal(err)
	}
	offerID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status) VALUES ($1, $2, $3, $4, 'EG', 'active')`, offerID, supplierID, suppProdID, suppMarketID); err != nil {
		t.Fatal(err)
	}

	locID, resID := createTestLocationAndReservation(t, db, ctx, store.ID, skuID)

	orderNumber, _ := repo.AllocateOrderNumber(ctx, nil, store.ID)
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	cost := int64(800)
	costCurr := "EGP"

	order := Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 orderNumber,
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
		Items: []OrderItem{
			{
				ID:                       uuid.NewString(),
				SellerListingID:          &listingID,
				ProductID:                &productID,
				VariantID:                &variantID,
				SKUID:                    &skuID,
				SupplierOfferID:          &offerID,
				SourceSupplierID:         &supplierID,
				FulfillmentLocationID:    locID,
				InventoryReservationID:   resID,
				ProductTitleSnapshot:     "Title",
				SKUCodeSnapshot:          "SKU",
				UnitPriceMinor:           1000,
				CurrencyCode:             "EGP",
				Quantity:                 1,
				LineTotalMinor:           1000,
				SupplierCostMinor:        &cost,
				SupplierCostCurrencyCode: &costCurr,
			},
		},
	}

	if _, err := repo.CreateOrder(ctx, nil, order); err != nil {
		t.Fatal(err)
	}

	// 1. Attempting to delete supplier_offers blocked by ON DELETE RESTRICT
	if _, err := db.Exec(ctx, `DELETE FROM supplier_offers WHERE id = $1`, offerID); err == nil {
		t.Fatal("expected deleting supplier_offers to be blocked by RESTRICT, got nil error")
	}

	// 2. Attempting to delete suppliers blocked by ON DELETE RESTRICT
	if _, err := db.Exec(ctx, `DELETE FROM suppliers WHERE id = $1`, supplierID); err == nil {
		t.Fatal("expected deleting suppliers to be blocked by RESTRICT, got nil error")
	}

	// 3. Attempting to delete fulfillment_locations blocked by ON DELETE RESTRICT
	if _, err := db.Exec(ctx, `DELETE FROM fulfillment_locations WHERE id = $1`, locID); err == nil {
		t.Fatal("expected deleting fulfillment_locations to be blocked by RESTRICT, got nil error")
	}

	// 4. Attempting to delete inventory_reservations blocked by ON DELETE RESTRICT
	if _, err := db.Exec(ctx, `DELETE FROM inventory_reservations WHERE id = $1`, resID); err == nil {
		t.Fatal("expected deleting inventory_reservations to be blocked by RESTRICT, got nil error")
	}

	// 5. Deleting seller_listings sets FK to NULL (ON DELETE SET NULL)
	if _, err := db.Exec(ctx, `DELETE FROM seller_listings WHERE id = $1`, listingID); err != nil {
		t.Fatalf("expected deleting seller_listings to succeed with SET NULL, got %v", err)
	}

	fetched, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Items[0].SellerListingID != nil {
		t.Fatalf("expected seller_listing_id to be NULL after deletion")
	}
	// Historical snapshots remain intact!
	if fetched.Items[0].ProductTitleSnapshot != "Title" || fetched.Items[0].SKUCodeSnapshot != "SKU" || fetched.Items[0].UnitPriceMinor != 1000 {
		t.Fatalf("historical snapshots mutated after catalog deletion")
	}
}

func TestP53OrderStateMachineTransitionsAndVersion(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	// 1. Invalid transition (Customer attempting PENDING -> CONFIRMED) returns ErrInvalidTransition
	store, order, _, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix+"-1", 10, 2, 2, 30*time.Minute)
	if _, err := repo.UpdateOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusConfirmed, AuthorityCustomer, nil, nil, time.Now().UTC()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for customer confirm, got %v", err)
	}
	unchanged, _ := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if unchanged.Status != OrderStatusPending || unchanged.AggregateVersion != 1 {
		t.Fatalf("order status or version changed after invalid transition: %+v", unchanged)
	}

	// 2. Seller confirmation after deadline returns ErrInvalidTransition
	storeLate, orderLate, _, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix+"-late", 10, 2, 2, -10*time.Minute)
	if _, err := repo.UpdateOrderStatus(ctx, nil, storeLate.ID, orderLate.ID, OrderStatusConfirmed, AuthoritySeller, nil, nil, time.Now().UTC()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for confirm after deadline, got %v", err)
	}
	unchangedAfterDeadline, _ := repo.GetOrderByID(ctx, nil, storeLate.ID, orderLate.ID)
	if unchangedAfterDeadline.Status != OrderStatusPending || unchangedAfterDeadline.AggregateVersion != 1 {
		t.Fatalf("order status or version changed after late confirm attempt")
	}

	// 3. Valid Seller confirmation before deadline updates status to CONFIRMED and increments version to 2
	confirmed, err := repo.UpdateOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusConfirmed, AuthoritySeller, nil, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("valid seller confirm failed: %v", err)
	}
	if confirmed.Status != OrderStatusConfirmed || confirmed.AggregateVersion != 2 {
		t.Fatalf("confirmed order status or version mismatch: %+v", confirmed)
	}

	// Verify timeline entry was recorded
	var timelineCount int
	var toStatus, actorType string
	if err := db.QueryRow(ctx, `SELECT count(*), to_status, actor_type FROM order_timeline WHERE order_id = $1 GROUP BY to_status, actor_type`, order.ID).Scan(&timelineCount, &toStatus, &actorType); err != nil {
		t.Fatal(err)
	}
	if timelineCount != 1 || toStatus != OrderStatusConfirmed || actorType != string(AuthoritySeller) {
		t.Fatalf("timeline entry mismatch: count=%d, toStatus=%s, actorType=%s", timelineCount, toStatus, actorType)
	}
}

func TestP53OrderNotes(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, _, session, _ := createTestStoreAndSession(t, db, repo, ctx, suffix)

	orderNumber, _ := repo.AllocateOrderNumber(ctx, nil, store.ID)
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)

	order := Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 orderNumber,
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	if _, err := repo.CreateOrder(ctx, nil, order); err != nil {
		t.Fatal(err)
	}

	note, err := repo.CreateNote(ctx, nil, OrderNote{
		OrderID:       order.ID,
		AuthorSubject: "seller-user-1",
		Visibility:    NoteVisibilityInternal,
		Body:          "Internal seller note for order",
	})
	if err != nil {
		t.Fatalf("failed to create note: %v", err)
	}
	if note.ID == "" || note.Visibility != NoteVisibilityInternal {
		t.Fatalf("created note mismatch: %+v", note)
	}

	notes, err := repo.GetNotes(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatalf("failed to get notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "Internal seller note for order" {
		t.Fatalf("notes retrieval mismatch: %+v", notes)
	}
}

func TestP57GuestOrderReadAndCancel(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffixA := uuid.NewString()
	suffixB := uuid.NewString()
	storeA, _, sessionA, rawCapabilityA := createTestStoreAndSession(t, db, repo, ctx, suffixA)
	storeB, _, _, _ := createTestStoreAndSession(t, db, repo, ctx, suffixB)

	// Finalize checkout transactionally
	now := time.Now()
	orderNumber, _ := repo.AllocateOrderNumber(ctx, nil, storeA.ID)
	createdOrder, err := repo.CreateOrder(ctx, nil, Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 orderNumber,
		StoreID:                     storeA.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           sessionA.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: digestFromRawCap(rawCapabilityA),
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      now.Add(15 * time.Minute),
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if err != nil {
		t.Fatalf("failed to create order for test: %v", err)
	}

	// 1. Matrix 32: Correct Store + token reads Order
	fetchedOrder, err := repo.GetGuestOrder(ctx, nil, storeA.ID, createdOrder.ID, rawCapabilityA)
	if err != nil {
		t.Fatalf("expected successful guest order read, got: %v", err)
	}
	if fetchedOrder.ID != createdOrder.ID {
		t.Fatalf("expected order ID %s, got %s", createdOrder.ID, fetchedOrder.ID)
	}

	// 2. Matrix 33: Wrong Store Host rejected (ErrNotFound)
	_, err = repo.GetGuestOrder(ctx, nil, storeB.ID, createdOrder.ID, rawCapabilityA)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for wrong store host, got: %v", err)
	}

	// 3. Matrix 34: Wrong guest token rejected (ErrUnauthorized)
	_, err = repo.GetGuestOrder(ctx, nil, storeA.ID, createdOrder.ID, "wrong-token-value")
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for wrong guest token, got: %v", err)
	}

	// 4. Matrix 35/36: Empty capability (UUID-only or order-number-only) rejected
	_, err = repo.GetGuestOrder(ctx, nil, storeA.ID, createdOrder.ID, "")
	if err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for missing capability, got: %v", err)
	}

	// 5. Matrix 40: Public DTO sanitation (ToPublic contains no raw capability or digest)
	publicOrder := fetchedOrder.ToPublic()
	if len(publicOrder.ID) == 0 || publicOrder.Status != OrderStatusPending {
		t.Fatalf("invalid public order struct: %+v", publicOrder)
	}

	// 6. Matrix 37: Guest pending cancellation
	cancelledOrder, err := repo.CancelGuestOrder(ctx, nil, storeA.ID, createdOrder.ID, rawCapabilityA, "corr-guest-cancel-1")
	if err != nil {
		t.Fatalf("expected successful guest pending cancellation, got: %v", err)
	}
	if cancelledOrder.Status != OrderStatusCancelled {
		t.Fatalf("expected status cancelled, got %s", cancelledOrder.Status)
	}

	// 7. Matrix 39: Guest cancellation retry is idempotent
	cancelledOrderRetry, err := repo.CancelGuestOrder(ctx, nil, storeA.ID, createdOrder.ID, rawCapabilityA, "corr-guest-cancel-2")
	if err != nil {
		t.Fatalf("expected idempotent cancellation retry, got: %v", err)
	}
	if cancelledOrderRetry.Status != OrderStatusCancelled {
		t.Fatalf("expected status cancelled on retry, got %s", cancelledOrderRetry.Status)
	}

	// 8. Matrix 38: Guest cancellation on confirmed order rejected
	// Create another order for storeA and confirm it
	cart2, cartToken2, _ := repo.CreateCart(ctx, storeA.ID, "EG", nil)
	_ = cart2
	session2, rawCapability2, _ := repo.CreateCheckoutSession(ctx, storeA.ID, cartToken2, nil, time.Hour)
	orderNumber2, _ := repo.AllocateOrderNumber(ctx, nil, storeA.ID)
	confirmedOrder, err := repo.CreateOrder(ctx, nil, Order{
		ID:                          uuid.NewString(),
		OrderNumber:                 orderNumber2,
		StoreID:                     storeA.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session2.ID,
		Status:                      OrderStatusConfirmed,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: digestFromRawCap(rawCapability2),
		SubtotalMinor:               1000,
		TotalMinor:                  1000,
		ConfirmationDeadlineAt:      now.Add(15 * time.Minute),
		CreatedAt:                   now,
		UpdatedAt:                   now,
	})
	if err != nil {
		t.Fatalf("failed to create confirmed order: %v", err)
	}

	_, err = repo.CancelGuestOrder(ctx, nil, storeA.ID, confirmedOrder.ID, rawCapability2, "corr-guest-cancel-3")
	if err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition for confirmed order cancel, got: %v", err)
	}
}

func digestFromRawCap(rawCap string) []byte {
	d := sha256.Sum256([]byte(rawCap))
	return d[:]
}

func TestMigration000012_UpAndDown(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	ctx := context.Background()

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

	upSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000012_order_aggregate_schema.up.sql"))
	if err != nil {
		t.Fatalf("failed to read migration 12 up: %v", err)
	}
	if _, err := db.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("failed to apply migration 12 up: %v", err)
	}

	downSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000012_order_aggregate_schema.down.sql"))
	if err != nil {
		t.Fatalf("failed to read migration 12 down: %v", err)
	}
	if _, err := db.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("failed to apply migration 12 down: %v", err)
	}

	if _, err := db.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("failed to re-apply migration 12 up: %v", err)
	}
}
