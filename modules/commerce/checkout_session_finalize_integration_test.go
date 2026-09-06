package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/money"
)

type testCheckoutSetup struct {
	Store      Store
	Cart       Cart
	CartToken  string
	Session    CheckoutSession
	Capability string
	ProductID  string
	VariantID  string
	SKUID      string
	ListingID  string
	LocationID string
	SnapshotID string
}

func setupSellerCheckoutTest(t *testing.T, db *database.Pool, repo Repository, ctx context.Context, suffix string, initialStock int64, priceMinor int64) testCheckoutSetup {
	t.Helper()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}

	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')
	`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `
		INSERT INTO product_translations (product_id, locale, name, description) VALUES ($1, 'en', $2, 'Desc')
	`, productID, "Test Product "+suffix); err != nil {
		t.Fatal(err)
	}

	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')
	`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}

	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')
	`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}

	listingID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status)
		VALUES ($1, $2, $3, NULL, 'EG', 'active')
	`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}

	priceID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current)
		VALUES ($1, $2, $3, 'EGP', true)
	`, priceID, listingID, priceMinor); err != nil {
		t.Fatal(err)
	}

	locationID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, supplier_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, NULL, 'EG', $3, 'Warehouse', 'warehouse', 'active')
	`, locationID, store.ID, "LOC-"+suffix); err != nil {
		t.Fatal(err)
	}

	snapshotID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, $4, 0, 1)
	`, snapshotID, locationID, skuID, initialStock); err != nil {
		t.Fatal(err)
	}

	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}

	cart, err = repo.AddCartItem(ctx, store.ID, cartToken, skuID, 1)
	if err != nil {
		t.Fatal(err)
	}

	session, capability, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return testCheckoutSetup{
		Store:      store,
		Cart:       cart,
		CartToken:  cartToken,
		Session:    session,
		Capability: capability,
		ProductID:  productID,
		VariantID:  variantID,
		SKUID:      skuID,
		ListingID:  listingID,
		LocationID: locationID,
		SnapshotID: snapshotID,
	}
}

func testFinalizePayload(sessionID string) FinalizeRequest {
	return FinalizeRequest{
		SessionID: sessionID,
		ShippingAddress: ShippingAddress{
			RecipientName: "John Doe",
			AddressLine1:  "123 Main St",
			City:          "Cairo",
			CountryCode:   "EG",
		},
		ContactEmail: "john@example.com",
	}
}

func waitForBackendBlockedOnLock(t *testing.T, db *database.Pool, waiterPID int32, blockingPID int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var isBlocked bool
		err := db.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity sa
				WHERE sa.pid = $1
				  AND sa.wait_event_type = 'Lock'
				  AND $2 = ANY(pg_blocking_pids($1))
			)
		`, waiterPID, blockingPID).Scan(&isBlocked)
		if err == nil && isBlocked {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("timed out waiting for waiter PID to block on holder PID lock")
}

type testSupplierCheckoutSetup struct {
	Supplier   Supplier
	Offer      SupplierOffer
	Store      Store
	Cart       Cart
	CartToken  string
	Session    CheckoutSession
	Capability string
	ProductID  string
	VariantID  string
	SKUID      string
	ListingID  string
	LocationID string
	SnapshotID string
}

func setupSupplierCheckoutTest(t *testing.T, db *database.Pool, repo Repository, ctx context.Context, suffix string, initialStock int64, retailPriceMinor int64, costMinor int64) testSupplierCheckoutSetup {
	t.Helper()

	supplier, err := repo.CreateSupplier(ctx, "supplier-"+suffix, "Supplier "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	market, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	location, err := repo.CreateFulfillmentLocation(ctx, supplier.ID, market.ID, "EG", "loc-"+suffix, "Loc "+suffix, "warehouse", "active")
	if err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'Supplier Product')`, productID); err != nil {
		t.Fatal(err)
	}
	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}

	sp, err := repo.CreateSupplierProduct(ctx, supplier.ID, productID, "SUP-PROD-"+suffix, "active")
	if err != nil {
		t.Fatal(err)
	}
	offer, err := repo.CreateSupplierOffer(ctx, supplier.ID, sp.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatal(err)
	}
	costMoney, _ := money.New(costMinor, "EGP")
	if _, err := repo.SetSupplierOfferPrice(ctx, offer.ID, costMoney); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSupplierOfferAvailability(ctx, offer.ID, true, nil); err != nil {
		t.Fatal(err)
	}

	snapshotID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, $4, 0, 1)
	`, snapshotID, location.ID, skuID, initialStock); err != nil {
		t.Fatal(err)
	}

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	offerID := offer.ID
	listing, err := repo.CreateSellerListing(ctx, store.ID, productID, &offerID, "EG", "active")
	if err != nil {
		t.Fatal(err)
	}
	retailMoney, _ := money.New(retailPriceMinor, "EGP")
	if _, err := repo.SetSellerListingPrice(ctx, listing.ID, retailMoney); err != nil {
		t.Fatal(err)
	}

	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, err = repo.AddCartItem(ctx, store.ID, cartToken, skuID, 1)
	if err != nil {
		t.Fatal(err)
	}

	session, capability, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return testSupplierCheckoutSetup{
		Supplier:   supplier,
		Offer:      offer,
		Store:      store,
		Cart:       cart,
		CartToken:  cartToken,
		Session:    session,
		Capability: capability,
		ProductID:  productID,
		VariantID:  variantID,
		SKUID:      skuID,
		ListingID:  listing.ID,
		LocationID: location.ID,
		SnapshotID: snapshotID,
	}
}

func TestFinalizeCheckoutCreatesOrderAtomically(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-1")
	if err != nil {
		t.Fatalf("FinalizeCheckout failed: %v", err)
	}

	if order.ID == "" || order.OrderNumber == "" || order.Status != OrderStatusPending {
		t.Fatalf("unexpected order output: %+v", order)
	}
	if order.SubtotalMinor != 1000 || order.TotalMinor != 1000 || order.CurrencyCode != "EGP" {
		t.Fatalf("unexpected money totals: %+v", order)
	}

	// Verify Session finalized & Cart checked_out
	var sessionStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&sessionStatus); err != nil {
		t.Fatal(err)
	}
	if sessionStatus != CheckoutSessionStatusFinalized {
		t.Fatalf("session status = %s, want finalized", sessionStatus)
	}

	var cartStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&cartStatus); err != nil {
		t.Fatal(err)
	}
	if cartStatus != CartStatusCheckedOut {
		t.Fatalf("cart status = %s, want checked_out", cartStatus)
	}

	// Verify Inventory Snapshot reserved_qty = 1
	var reservedQty int64
	if err := db.QueryRow(ctx, `SELECT reserved_qty FROM inventory_snapshots WHERE id = $1`, setup.SnapshotID).Scan(&reservedQty); err != nil {
		t.Fatal(err)
	}
	if reservedQty != 1 {
		t.Fatalf("reserved_qty = %d, want 1", reservedQty)
	}

	// Verify Reservation & Movement created
	var resCount, movCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations WHERE inventory_snapshot_id = $1`, setup.SnapshotID).Scan(&resCount); err != nil {
		t.Fatal(err)
	}
	if resCount != 1 {
		t.Fatalf("resCount = %d, want 1", resCount)
	}

	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'reservation_held'`, setup.SnapshotID).Scan(&movCount); err != nil {
		t.Fatal(err)
	}
	if movCount != 1 {
		t.Fatalf("movCount = %d, want 1", movCount)
	}

	// Verify Timeline & Outbox event
	var timelineCount, outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount); err != nil {
		t.Fatal(err)
	}
	if timelineCount != 1 {
		t.Fatalf("timelineCount = %d, want 1", timelineCount)
	}

	if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'commerce.order.created.v1'`, order.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 1 {
		t.Fatalf("outboxCount = %d, want 1", outboxCount)
	}
}

func TestFinalizeCheckoutTenConcurrentIdenticalRequestsOneOrder(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	const concurrency = 10
	var wg sync.WaitGroup
	orders := make(chan Order, concurrency)
	errs := make(chan error, concurrency)

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			req := testFinalizePayload(setup.Session.ID)
			order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-concurrent")
			if err != nil {
				errs <- err
				return
			}
			orders <- order
		}()
	}

	close(startBarrier)
	wg.Wait()
	close(orders)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error during concurrent finalize: %v", err)
	}

	orderIDs := make(map[string]struct{})
	for order := range orders {
		orderIDs[order.ID] = struct{}{}
	}

	if len(orderIDs) != 1 {
		t.Fatalf("expected 1 distinct Order ID across concurrent requests, got %d", len(orderIDs))
	}

	var totalOrders int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, setup.Session.ID).Scan(&totalOrders); err != nil {
		t.Fatal(err)
	}
	if totalOrders != 1 {
		t.Fatalf("total orders in DB = %d, want 1", totalOrders)
	}

	var totalReservations int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations WHERE inventory_snapshot_id = $1`, setup.SnapshotID).Scan(&totalReservations); err != nil {
		t.Fatal(err)
	}
	if totalReservations != 1 {
		t.Fatalf("total reservations in DB = %d, want 1", totalReservations)
	}
}

func TestFinalizeCheckoutReplayReturnsExactSameOrder(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	firstOrder, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-replay")
	if err != nil {
		t.Fatal(err)
	}

	secondOrder, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-replay-2")
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	if firstOrder.ID != secondOrder.ID || firstOrder.OrderNumber != secondOrder.OrderNumber {
		t.Fatalf("replay returned different Order: first=%+v, second=%+v", firstOrder, secondOrder)
	}
}

func TestFinalizeCheckoutChangedSemanticReplayConflicts(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-1")
	if err != nil {
		t.Fatal(err)
	}

	// Change address
	mutatedReq := req
	mutatedReq.ShippingAddress.AddressLine1 = "999 Changed St"

	_, err = repo.FinalizeCheckout(ctx, setup.Store.ID, mutatedReq, "corr-2")
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict for changed replay, got %v", err)
	}
}

func TestFinalizeCheckoutTwoOpenSessionsOneCartConcurrentContention(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	session2, _, err := repo.CreateCheckoutSession(ctx, setup.Store.ID, setup.CartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	sessionAPIDChan := make(chan int32, 1)
	sessionBPIDChan := make(chan int32, 1)

	sessionAHolding := make(chan struct{})
	sessionACanCommit := make(chan struct{})

	testHookBeforeFinalizeCommit = func(hookCtx context.Context) error {
		close(sessionAHolding)
		<-sessionACanCommit
		return nil
	}
	defer func() { testHookBeforeFinalizeCommit = nil }()

	var wg sync.WaitGroup
	var order1 Order
	var err1 error
	var err2 error

	// Goroutine 1: Session A
	wg.Add(1)
	go func() {
		defer wg.Done()
		req1 := testFinalizePayload(setup.Session.ID)
		ctxA := WithTxPIDNotifier(ctx, sessionAPIDChan)
		order1, err1 = repo.FinalizeCheckout(ctxA, setup.Store.ID, req1, "corr-sessA")
	}()

	sessionAPID := <-sessionAPIDChan
	<-sessionAHolding

	// Goroutine 2: Session B (will attempt to lock Cart and block)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req2 := testFinalizePayload(session2.ID)
		ctxB := WithTxPIDNotifier(ctx, sessionBPIDChan)
		_, err2 = repo.FinalizeCheckout(ctxB, setup.Store.ID, req2, "corr-sessB")
	}()

	sessionBPID := <-sessionBPIDChan

	// Prove Session B is blocked waiting for Session A's Cart lock without sleep
	waitForBackendBlockedOnLock(t, db, sessionBPID, sessionAPID)

	// Release Session A to commit
	close(sessionACanCommit)

	wg.Wait()

	if err1 != nil {
		t.Fatalf("Session A finalize failed: %v", err1)
	}
	if order1.ID == "" {
		t.Fatal("Session A order ID is empty")
	}

	if !errors.Is(err2, ErrConflict) {
		t.Fatalf("Session B error = %v, want ErrConflict", err2)
	}

	// Verify exact state
	var orderCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id IN ($1, $2)`, setup.Session.ID, session2.ID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 {
		t.Fatalf("orderCount = %d, want 1", orderCount)
	}

	var session1Status, session2Status string
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&session1Status)
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, session2.ID).Scan(&session2Status)

	if session1Status != CheckoutSessionStatusFinalized {
		t.Fatalf("session 1 status = %s, want finalized", session1Status)
	}
	if session2Status != CheckoutSessionStatusOpen {
		t.Fatalf("losing session 2 status = %s, want open", session2Status)
	}

	var cartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&cartStatus)
	if cartStatus != CartStatusCheckedOut {
		t.Fatalf("cart status = %s, want checked_out", cartStatus)
	}
}

func TestFinalizeCheckoutLastUnitContention(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	// Initial stock = 1 unit
	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'Prod')`, productID); err != nil {
		t.Fatal(err)
	}
	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}
	listingID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), listingID); err != nil {
		t.Fatal(err)
	}
	locationID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locationID, store.ID, "LOC-"+suffix); err != nil {
		t.Fatal(err)
	}
	snapshotID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 1, 0, 1)`, snapshotID, locationID, skuID); err != nil {
		t.Fatal(err)
	}

	const numCheckouts = 20
	type checkoutTarget struct {
		Session CheckoutSession
		Token   string
	}
	targets := make([]checkoutTarget, 0, numCheckouts)

	for i := 0; i < numCheckouts; i++ {
		_, token, err := repo.CreateCart(ctx, store.ID, "EG", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.AddCartItem(ctx, store.ID, token, skuID, 1)
		if err != nil {
			t.Fatal(err)
		}
		s, _, err := repo.CreateCheckoutSession(ctx, store.ID, token, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, checkoutTarget{Session: s, Token: token})
	}

	var wg sync.WaitGroup
	winners := make(chan Order, numCheckouts)
	losers := make(chan error, numCheckouts)

	startBarrier := make(chan struct{})

	for _, tgt := range targets {
		wg.Add(1)
		tTarget := tgt
		go func() {
			defer wg.Done()
			<-startBarrier
			req := testFinalizePayload(tTarget.Session.ID)
			order, err := repo.FinalizeCheckout(ctx, store.ID, req, "corr-lastunit")
			if err != nil {
				losers <- err
				return
			}
			winners <- order
		}()
	}

	close(startBarrier)
	wg.Wait()
	close(winners)
	close(losers)

	var winnerList []Order
	for o := range winners {
		winnerList = append(winnerList, o)
	}

	var loserErrList []error
	for e := range losers {
		loserErrList = append(loserErrList, e)
	}

	if len(winnerList) != 1 {
		t.Fatalf("expected exactly 1 winner for 1 stock unit, got %d", len(winnerList))
	}
	if len(loserErrList) != numCheckouts-1 {
		t.Fatalf("expected %d losers, got %d", numCheckouts-1, len(loserErrList))
	}

	for _, err := range loserErrList {
		if !errors.Is(err, ErrInsufficientInventory) && !errors.Is(err, ErrConflict) {
			t.Fatalf("unexpected loser error code: %v", err)
		}
	}

	winner := winnerList[0]

	// Postcondition 1: Exactly 1 order committed for all competing carts
	allSessionIDs := make([]string, 0, len(targets))
	for _, tgt := range targets {
		allSessionIDs = append(allSessionIDs, tgt.Session.ID)
	}
	var totalOrders int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = ANY($1)`, allSessionIDs).Scan(&totalOrders); err != nil {
		t.Fatal(err)
	}
	if totalOrders != 1 {
		t.Fatalf("total orders committed = %d, want 1", totalOrders)
	}

	// Postcondition 2: Exactly 1 held reservation survived
	var totalHeldRes int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations WHERE inventory_snapshot_id = $1 AND status = 'held'`, snapshotID).Scan(&totalHeldRes); err != nil {
		t.Fatal(err)
	}
	if totalHeldRes != 1 {
		t.Fatalf("total held reservations = %d, want 1", totalHeldRes)
	}

	// Postcondition 3: Exactly 1 reservation_held movement survived
	var totalMovements int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'reservation_held'`, snapshotID).Scan(&totalMovements); err != nil {
		t.Fatal(err)
	}
	if totalMovements != 1 {
		t.Fatalf("total reservation_held movements = %d, want 1", totalMovements)
	}

	// Postcondition 4: Snapshot stock state
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapshotID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 1 || reserved != 1 {
		t.Fatalf("snapshot on_hand=%d, reserved=%d; want on_hand=1, reserved=1", onHand, reserved)
	}
	if reserved > onHand {
		t.Fatalf("OVERSOLD: reserved_qty (%d) > on_hand_qty (%d)", reserved, onHand)
	}

	// Postcondition 5: Losers leave zero side effects for losing sessions
	for _, tgt := range targets {
		if tgt.Session.ID == winner.CheckoutSessionID {
			continue
		}
		var lOrderCount int
		_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, tgt.Session.ID).Scan(&lOrderCount)
		if lOrderCount != 0 {
			t.Fatalf("losing session %s created %d orders", tgt.Session.ID, lOrderCount)
		}
	}
}

func TestFinalizeCheckoutLateFailureRollsBackEverything(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// 1. Snapshot initial state of all 11 tables/rows
	var initialSeqVal int64
	_ = db.QueryRow(ctx, `SELECT COALESCE((SELECT next_value FROM store_order_sequences WHERE store_id = $1), 0)`, setup.Store.ID).Scan(&initialSeqVal)

	var snapOnHand, snapReserved, snapVersion int64
	_ = db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty, version FROM inventory_snapshots WHERE id = $1`, setup.SnapshotID).Scan(&snapOnHand, &snapReserved, &snapVersion)

	var initialResCount, initialMovCount, initialOrderCount, initialItemCount, initialAddrCount, initialTimelineCount, initialOutboxCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations`).Scan(&initialResCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements`).Scan(&initialMovCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&initialOrderCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_items`).Scan(&initialItemCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_addresses`).Scan(&initialAddrCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_timeline`).Scan(&initialTimelineCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&initialOutboxCount)

	var initSessStatus, initCartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&initSessStatus)
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&initCartStatus)

	// Set test hook to simulate late failure right before commit
	testHookBeforeFinalizeCommit = func(ctx context.Context) error {
		return errors.New("simulated late transaction failure")
	}
	defer func() { testHookBeforeFinalizeCommit = nil }()

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-rollback-test")
	if err == nil {
		t.Fatal("expected failure from test hook, got nil")
	}

	// 2. Assert exact equality for all 11 tables/states after rollback
	var afterSeqVal int64
	_ = db.QueryRow(ctx, `SELECT COALESCE((SELECT next_value FROM store_order_sequences WHERE store_id = $1), 0)`, setup.Store.ID).Scan(&afterSeqVal)
	if afterSeqVal != initialSeqVal {
		t.Fatalf("sequence rolled back mismatch: after=%d, initial=%d", afterSeqVal, initialSeqVal)
	}

	var afterSnapOnHand, afterSnapReserved, afterSnapVersion int64
	_ = db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty, version FROM inventory_snapshots WHERE id = $1`, setup.SnapshotID).Scan(&afterSnapOnHand, &afterSnapReserved, &afterSnapVersion)
	if afterSnapOnHand != snapOnHand || afterSnapReserved != snapReserved || afterSnapVersion != snapVersion {
		t.Fatalf("inventory snapshot changed: on_hand=%d/%d reserved=%d/%d ver=%d/%d", afterSnapOnHand, snapOnHand, afterSnapReserved, snapReserved, afterSnapVersion, snapVersion)
	}

	var afterResCount, afterMovCount, afterOrderCount, afterItemCount, afterAddrCount, afterTimelineCount, afterOutboxCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations`).Scan(&afterResCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements`).Scan(&afterMovCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&afterOrderCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_items`).Scan(&afterItemCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_addresses`).Scan(&afterAddrCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM order_timeline`).Scan(&afterTimelineCount)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&afterOutboxCount)

	if afterResCount != initialResCount || afterMovCount != initialMovCount || afterOrderCount != initialOrderCount ||
		afterItemCount != initialItemCount || afterAddrCount != initialAddrCount || afterTimelineCount != initialTimelineCount ||
		afterOutboxCount != initialOutboxCount {
		t.Fatalf("table counts changed after rollback: res=%d/%d mov=%d/%d order=%d/%d item=%d/%d addr=%d/%d timeline=%d/%d outbox=%d/%d",
			afterResCount, initialResCount, afterMovCount, initialMovCount, afterOrderCount, initialOrderCount,
			afterItemCount, initialItemCount, afterAddrCount, initialAddrCount, afterTimelineCount, initialTimelineCount, afterOutboxCount, initialOutboxCount)
	}

	var afterSessStatus, afterCartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&afterSessStatus)
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&afterCartStatus)
	if afterSessStatus != initSessStatus {
		t.Fatalf("session status = %s, want %s", afterSessStatus, initSessStatus)
	}
	if afterCartStatus != initCartStatus {
		t.Fatalf("cart status = %s, want %s", afterCartStatus, initCartStatus)
	}
}

func TestFinalizeCheckoutConfiguredConfirmationDuration(t *testing.T) {
	db, origRepo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, origRepo, ctx, suffix, 10, 1000)

	customRepo := origRepo
	customRepo.OrderConfirmationDuration = 45 * time.Minute

	req := testFinalizePayload(setup.Session.ID)
	order, err := customRepo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-conf-duration")
	if err != nil {
		t.Fatal(err)
	}

	duration := order.ConfirmationDeadlineAt.Sub(order.CreatedAt)
	if duration != 45*time.Minute {
		t.Fatalf("confirmation deadline duration = %s, want 45m", duration)
	}
}

func TestFinalizeCheckoutSupplierOfferAvailabilityFailClosed(t *testing.T) {
	db, repo, ctx := setupP53Database(t)

	t.Run("no availability row checkout allowed", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 1000, 500)
		if _, err := db.Exec(ctx, `DELETE FROM supplier_offer_availability WHERE supplier_offer_id = $1`, setup.Offer.ID); err != nil {
			t.Fatal(err)
		}
		req := testFinalizePayload(setup.Session.ID)
		order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-avail-none")
		if err != nil {
			t.Fatalf("expected checkout allowed for no availability row, got %v", err)
		}
		if order.ID == "" {
			t.Fatal("empty order ID")
		}
	})

	t.Run("availability true checkout allowed", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 1000, 500)
		req := testFinalizePayload(setup.Session.ID)
		order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-avail-true")
		if err != nil {
			t.Fatalf("expected checkout allowed when is_available=true, got %v", err)
		}
		if order.ID == "" {
			t.Fatal("empty order ID")
		}
	})

	t.Run("availability false returns ErrListingUnavailable", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 1000, 500)
		if _, err := repo.SetSupplierOfferAvailability(ctx, setup.Offer.ID, false, nil); err != nil {
			t.Fatal(err)
		}
		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-avail-false")
		if !errors.Is(err, ErrListingUnavailable) {
			t.Fatalf("expected ErrListingUnavailable for is_available=false, got %v", err)
		}
	})
}

func TestFinalizeCheckoutCopiesSessionCapabilityDigest(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-digest")
	if err != nil {
		t.Fatal(err)
	}

	var orderDigest []byte
	if err := db.QueryRow(ctx, `SELECT guest_order_access_token_digest FROM orders WHERE id = $1`, order.ID).Scan(&orderDigest); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(orderDigest, setup.Session.GuestOrderAccessTokenDigest) {
		t.Fatalf("order guest digest %x != session guest digest %x", orderDigest, setup.Session.GuestOrderAccessTokenDigest)
	}

	expectedDigest := sha256.Sum256([]byte(setup.Capability))
	if !bytes.Equal(orderDigest, expectedDigest[:]) {
		t.Fatalf("order guest digest %x != sha256(raw capability) %x", orderDigest, expectedDigest)
	}
}

func TestFinalizeCheckoutMissingGuestDigestRejected(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Safely drop CHECK constraint in test DB to allow setting invalid digest in session row
	if _, err := db.Exec(ctx, `ALTER TABLE checkout_sessions DROP CONSTRAINT checkout_sessions_guest_digest_check`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(ctx, `ALTER TABLE checkout_sessions ADD CONSTRAINT checkout_sessions_guest_digest_check CHECK (octet_length(guest_order_access_token_digest) = 32)`)
	}()

	// Corrupt digest in DB to 16 bytes
	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET guest_order_access_token_digest = E'\\\\x12345678901234567890123456789012' WHERE id = $1`, setup.Session.ID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-invalid-digest")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid digest len, got %v", err)
	}

	// Assert zero side effects
	var orderCount, resCount, movCount, outboxCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, setup.Session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order created despite invalid digest: %d", orderCount)
	}
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_reservations WHERE inventory_snapshot_id = $1`, setup.SnapshotID).Scan(&resCount)
	if resCount != 0 {
		t.Fatalf("reservation created despite invalid digest: %d", resCount)
	}
	_ = db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, setup.SnapshotID).Scan(&movCount)
	if movCount != 0 {
		t.Fatalf("movement created despite invalid digest: %d", movCount)
	}
	_ = db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, setup.Session.ID).Scan(&outboxCount)
	if outboxCount != 0 {
		t.Fatalf("outbox event created despite invalid digest: %d", outboxCount)
	}

	var sessionStatus, cartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&sessionStatus)
	if sessionStatus != CheckoutSessionStatusOpen {
		t.Fatalf("session status = %s, want open", sessionStatus)
	}
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&cartStatus)
	if cartStatus != CartStatusActive {
		t.Fatalf("cart status = %s, want active", cartStatus)
	}
}

func TestFinalizeCheckoutMoneyMultiplicationOverflow(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	largePrice := int64(1<<62 - 1)
	// Set retail price and cart expected price to the SAME value so price revalidation passes
	if _, err := db.Exec(ctx, `UPDATE seller_listing_prices SET amount_minor = $1 WHERE seller_listing_id = $2 AND is_current = true`, largePrice, setup.ListingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE cart_items SET expected_unit_price_minor = $1, quantity = 100 WHERE cart_id = $2`, largePrice, setup.Cart.ID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-overflow")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for multiplication overflow, got %v", err)
	}
	if errors.Is(err, ErrPriceChanged) {
		t.Fatal("got ErrPriceChanged instead of reaching checkedMultiply")
	}

	// Verify zero side effects
	var orderCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, setup.Session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order created on multiplication overflow: %d", orderCount)
	}
}

func TestFinalizeCheckoutMaximumValidMultiplication(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	maxHalf := (int64(math.MaxInt64) - 10) / 2
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, maxHalf)

	if _, err := db.Exec(ctx, `UPDATE cart_items SET quantity = 2 WHERE cart_id = $1`, setup.Cart.ID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-maxmult")
	if err != nil {
		t.Fatalf("FinalizeCheckout failed for max valid multiplication: %v", err)
	}

	expectedTotal := maxHalf * 2
	if order.SubtotalMinor != expectedTotal || order.TotalMinor != expectedTotal {
		t.Fatalf("order total = %d, want %d", order.TotalMinor, expectedTotal)
	}
	if len(order.Items) != 1 || order.Items[0].LineTotalMinor != expectedTotal {
		t.Fatalf("line total = %d, want %d", order.Items[0].LineTotalMinor, expectedTotal)
	}
}

func TestFinalizeCheckoutSubtotalAdditionOverflow(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	prod1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod1, "p1-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P1')`, prod1)
	var1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var1, prod1, "v1-"+suffix)
	sku1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, sku1, var1, "k1-"+suffix)

	prod2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod2, "p2-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P2')`, prod2)
	var2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var2, prod2, "v2-"+suffix)
	sku2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, sku2, var2, "k2-"+suffix)

	list1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list1, store.ID, prod1)
	price1 := int64(math.MaxInt64 - 100)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, $3, 'EGP', true)`, uuid.NewString(), list1, price1)

	list2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list2, store.ID, prod2)
	price2 := int64(200)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, $3, 'EGP', true)`, uuid.NewString(), list2, price2)

	locID := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locID, store.ID, "loc-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, uuid.NewString(), locID, sku1)
	_, _ = db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, uuid.NewString(), locID, sku2)

	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = repo.AddCartItem(ctx, store.ID, cartToken, sku1, 1)
	_, _ = repo.AddCartItem(ctx, store.ID, cartToken, sku2, 1)

	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(session.ID)
	_, err = repo.FinalizeCheckout(ctx, store.ID, req, "corr-subtotal-overflow")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for subtotal addition overflow, got %v", err)
	}

	// Verify complete rollback
	var orderCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order created on subtotal overflow: %d", orderCount)
	}
}

func TestFinalizeCheckoutPriceChanged(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Mutate current retail listing price in DB
	if _, err := db.Exec(ctx, `UPDATE seller_listing_prices SET amount_minor = 1500 WHERE seller_listing_id = $1 AND is_current = true`, setup.ListingID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-pricechange")
	if !errors.Is(err, ErrPriceChanged) {
		t.Fatalf("expected ErrPriceChanged, got %v", err)
	}
}

func TestFinalizeCheckoutInactiveProduct(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Deactivate product
	if _, err := db.Exec(ctx, `UPDATE products SET status = 'inactive' WHERE id = $1`, setup.ProductID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-inactiveprod")
	if !errors.Is(err, ErrListingUnavailable) {
		t.Fatalf("expected ErrListingUnavailable for inactive product, got %v", err)
	}
}

func TestFinalizeCheckoutSellerOwnedSupplierFieldsNull(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sellerowned")
	if err != nil {
		t.Fatal(err)
	}

	if len(order.Items) != 1 {
		t.Fatalf("order items count = %d, want 1", len(order.Items))
	}
	item := order.Items[0]
	if item.SupplierOfferID != nil || item.SourceSupplierID != nil || item.SupplierCostMinor != nil || item.SupplierCostCurrencyCode != nil {
		t.Fatalf("seller-owned order item has non-null supplier fields: %+v", item)
	}
}

func TestOrderCreatedEnvelopeCompleteAndPrivate(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-envelope-test")
	if err != nil {
		t.Fatal(err)
	}

	var evtID, evtType, aggType, aggID, corrID, causID string
	var schemaVer, aggVer int64
	var occurredAt time.Time
	var payloadRaw []byte

	err = db.QueryRow(ctx, `
		SELECT event_id, event_type, schema_version, aggregate_type, aggregate_id, aggregate_version, correlation_id, causation_id, occurred_at, payload
		FROM outbox_events
		WHERE aggregate_id = $1 AND event_type = 'commerce.order.created.v1'
	`, order.ID).Scan(&evtID, &evtType, &schemaVer, &aggType, &aggID, &aggVer, &corrID, &causID, &occurredAt, &payloadRaw)
	if err != nil {
		t.Fatalf("query outbox event failed: %v", err)
	}

	if _, err := uuid.Parse(evtID); err != nil || evtID == "" {
		t.Fatalf("event_id = %s, expected valid non-empty UUID: %v", evtID, err)
	}
	if causID != "" {
		t.Fatalf("causation_id = %s, want empty string for root HTTP checkout", causID)
	}

	if evtType != EventTypeOrderCreated {
		t.Fatalf("event_type = %s, want %s", evtType, EventTypeOrderCreated)
	}
	if schemaVer != 1 {
		t.Fatalf("schema_version = %d, want 1", schemaVer)
	}
	if aggType != "order" {
		t.Fatalf("aggregate_type = %s, want order", aggType)
	}
	if aggID != order.ID {
		t.Fatalf("aggregate_id = %s, want %s", aggID, order.ID)
	}
	if aggVer != 1 {
		t.Fatalf("aggregate_version = %d, want 1", aggVer)
	}
	if corrID != "corr-envelope-test" {
		t.Fatalf("correlation_id = %s, want corr-envelope-test", corrID)
	}
	if !occurredAt.Equal(order.CreatedAt) {
		t.Fatalf("occurred_at = %v, want %v", occurredAt, order.CreatedAt)
	}

	payloadStr := string(payloadRaw)
	for _, required := range []string{"order_id", "order_number", "store_id", "market_code", "status", "currency_code", "subtotal_minor", "total_minor", "confirmation_deadline_at", "created_at", "items"} {
		if !containsString(payloadStr, required) {
			t.Fatalf("outbox event payload missing required safe field %s: %s", required, payloadStr)
		}
	}
	for _, forbidden := range []string{"supplier_cost_minor", "supplier_cost_currency_code", "source_supplier_id", "supplier_offer_id", "fulfillment_location_id", "inventory_reservation_id", "reservation_token", "guest_order_access_token_digest", "123 Main St", "john@example.com"} {
		if containsString(payloadStr, forbidden) {
			t.Fatalf("outbox event payload contains private field %s: %s", forbidden, payloadStr)
		}
	}
}

func TestFinalizeCheckoutCurrencyChanged(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	if _, err := db.Exec(ctx, `UPDATE seller_listing_prices SET currency_code = 'SAR' WHERE seller_listing_id = $1 AND is_current = true`, setup.ListingID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-currchange")
	if !errors.Is(err, ErrPriceChanged) {
		t.Fatalf("expected ErrPriceChanged for currency change, got %v", err)
	}
}

func TestFinalizeCheckoutInactiveVariant(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	if _, err := db.Exec(ctx, `UPDATE variants SET status = 'inactive' WHERE id = $1`, setup.VariantID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-inactvar")
	if !errors.Is(err, ErrListingUnavailable) {
		t.Fatalf("expected ErrListingUnavailable for inactive variant, got %v", err)
	}
}

func TestFinalizeCheckoutInactiveSKU(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	if _, err := db.Exec(ctx, `UPDATE skus SET status = 'inactive' WHERE id = $1`, setup.SKUID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-inactsku")
	if !errors.Is(err, ErrListingUnavailable) {
		t.Fatalf("expected ErrListingUnavailable for inactive SKU, got %v", err)
	}
}

func TestFinalizeCheckoutNoSplitFulfillment(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'Prod')`, productID); err != nil {
		t.Fatal(err)
	}
	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}
	listingID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), listingID); err != nil {
		t.Fatal(err)
	}

	locA := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc A', 'warehouse', 'active')`, locA, store.ID, "LOCA-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 3, 0, 1)`, uuid.NewString(), locA, skuID); err != nil {
		t.Fatal(err)
	}

	locB := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc B', 'warehouse', 'active')`, locB, store.ID, "LOCB-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 3, 0, 1)`, uuid.NewString(), locB, skuID); err != nil {
		t.Fatal(err)
	}

	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.AddCartItem(ctx, store.ID, cartToken, skuID, 5)
	if err != nil {
		t.Fatal(err)
	}

	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(session.ID)
	_, err = repo.FinalizeCheckout(ctx, store.ID, req, "corr-nosplit")
	if !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("expected ErrInsufficientInventory for no-split stock failure, got %v", err)
	}
}

func TestFinalizeCheckoutCumulativeSnapshotDemand(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	prod1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod1, "p1-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P1')`, prod1)
	var1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var1, prod1, "v1-"+suffix)
	sku1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, sku1, var1, "shared-sku-"+suffix)

	prod2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod2, "p2-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P2')`, prod2)
	var2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var2, prod2, "v2-"+suffix)

	list1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list1, store.ID, prod1)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), list1)

	list2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list2, store.ID, prod1)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), list2)

	locID := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locID, store.ID, "loc-"+suffix)

	// Snapshot X has total 5 on hand
	_, _ = db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 5, 0, 1)`, uuid.NewString(), locID, sku1)

	c, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1, $2, $3, $4, 4, 1000, 'EGP')`, uuid.NewString(), c.ID, list1, sku1)
	_, _ = db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1, $2, $3, $4, 2, 1000, 'EGP')`, uuid.NewString(), c.ID, list2, sku1)

	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(session.ID)
	_, err = repo.FinalizeCheckout(ctx, store.ID, req, "corr-cumul-insufficient")
	if !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("expected ErrInsufficientInventory for cumulative demand exceeding single snapshot, got %v", err)
	}
}

func TestFinalizeCheckoutDeterministicAllocationFallback(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	prod1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod1, "p1-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P1')`, prod1)
	var1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var1, prod1, "v1-"+suffix)
	sku1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, sku1, var1, "k1-"+suffix)

	prod2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, prod2, "p2-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'P2')`, prod2)
	var2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, var2, prod2, "v2-"+suffix)

	list1 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list1, store.ID, prod1)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), list1)

	list2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, market_code, status) VALUES ($1, $2, $3, 'EG', 'active')`, list2, store.ID, prod1)
	_, _ = db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), list2)

	// Create 2 locations/snapshots ordered by ID
	id1 := "00000000-0000-0000-0000-000000000001"
	id2 := "00000000-0000-0000-0000-000000000002"
	loc1 := uuid.NewString()
	loc2 := uuid.NewString()
	_, _ = db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc1', 'warehouse', 'active')`, loc1, store.ID, "loc1-"+suffix)
	_, _ = db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc2', 'warehouse', 'active')`, loc2, store.ID, "loc2-"+suffix)

	// Snapshot 1 (lowest ID) has 3 on hand. Snapshot 2 (higher ID) has 10 on hand.
	_, _ = db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 3, 0, 1)`, id1, loc1, sku1)
	_, _ = db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, id2, loc2, sku1)

	c, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Line 1 requests 2 units. Line 2 requests 2 units.
	_, _ = db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1, $2, $3, $4, 2, 1000, 'EGP')`, uuid.NewString(), c.ID, list1, sku1)
	_, _ = db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1, $2, $3, $4, 2, 1000, 'EGP')`, uuid.NewString(), c.ID, list2, sku1)

	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(session.ID)
	order, err := repo.FinalizeCheckout(ctx, store.ID, req, "corr-fallback-success")
	if err != nil {
		t.Fatalf("expected successful allocation fallback, got %v", err)
	}

	if len(order.Items) != 2 {
		t.Fatalf("items count = %d, want 2", len(order.Items))
	}

	// Line 1 allocates from Loc1 (Snap1). Line 2 evaluates Snap1 (remaining 1 < 2), falls back to Loc2 (Snap2)!
	if order.Items[0].FulfillmentLocationID != loc1 {
		t.Fatalf("line 1 location = %s, want loc1 (%s)", order.Items[0].FulfillmentLocationID, loc1)
	}
	if order.Items[1].FulfillmentLocationID != loc2 {
		t.Fatalf("line 2 location = %s, want loc2 (%s)", order.Items[1].FulfillmentLocationID, loc2)
	}
}

func TestFinalizeCheckoutSessionExpiryAfterCartLockWait(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Helper Tx 1 locks Cart FOR UPDATE
	// Set Session expiry to 50ms in the future before Tx 2 locks Session row
	var targetExpiry time.Time
	if err := db.QueryRow(ctx, `UPDATE checkout_sessions SET expires_at = clock_timestamp() + interval '50 milliseconds' WHERE id = $1 RETURNING expires_at`, setup.Session.ID).Scan(&targetExpiry); err != nil {
		t.Fatal(err)
	}

	tx1, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()

	var holderPID int32
	if err := tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}

	var cartID string
	if err := tx1.QueryRow(ctx, `SELECT id FROM carts WHERE id = $1 FOR UPDATE`, setup.Cart.ID).Scan(&cartID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var finalizeErr error
	waiterPIDChan := make(chan int32, 1)

	// Tx 2: call FinalizeCheckout (will lock Session, then block on Cart FOR UPDATE)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := testFinalizePayload(setup.Session.ID)
		ctxWaiter := WithTxPIDNotifier(ctx, waiterPIDChan)
		_, finalizeErr = repo.FinalizeCheckout(ctxWaiter, setup.Store.ID, req, "corr-expiry-wait")
	}()

	waiterPID := <-waiterPIDChan

	// Detect Tx 2 blocked on Cart lock (zero sleep)
	waitForBackendBlockedOnLock(t, db, waiterPID, holderPID)

	// Poll DB clock until clock_timestamp() > targetExpiry
	for {
		var past bool
		if err := tx1.QueryRow(ctx, `SELECT clock_timestamp() >= $1`, targetExpiry).Scan(&past); err != nil {
			t.Fatal(err)
		}
		if past {
			break
		}
		runtime.Gosched()
	}

	// Helper Tx 1 commits, releasing Cart lock
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	wg.Wait()

	if !errors.Is(finalizeErr, ErrCheckoutExpired) {
		t.Fatalf("expected ErrCheckoutExpired when session expires during lock wait, got %v", finalizeErr)
	}

	// Verify zero side effects
	var orderCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, setup.Session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order created despite session expiry: %d", orderCount)
	}
}

func TestFinalizeCheckoutConfirmationWindowAfterInventoryWait(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Helper Tx 1 locks candidate inventory snapshot FOR UPDATE
	tx1, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()

	var holderPID int32
	if err := tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}

	var snapID string
	if err := tx1.QueryRow(ctx, `SELECT id FROM inventory_snapshots WHERE id = $1 FOR UPDATE`, setup.SnapshotID).Scan(&snapID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var order Order
	var finalizeErr error
	waiterPIDChan := make(chan int32, 1)

	// Tx 2: call FinalizeCheckout (locks session, cart, sequence, then blocks on inventory snapshot lock)
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := testFinalizePayload(setup.Session.ID)
		ctxWaiter := WithTxPIDNotifier(ctx, waiterPIDChan)
		order, finalizeErr = repo.FinalizeCheckout(ctxWaiter, setup.Store.ID, req, "corr-window-wait")
	}()

	waiterPID := <-waiterPIDChan

	// Detect Tx 2 blocked on snapshot lock (zero sleep)
	waitForBackendBlockedOnLock(t, db, waiterPID, holderPID)

	// Record DB clock while waiter is confirmed blocked
	var blockedTimestamp time.Time
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&blockedTimestamp); err != nil {
		t.Fatal(err)
	}
	blockedTimestamp = blockedTimestamp.UTC()

	// Advance DB clock beyond a meaningful boundary by polling DB clock
	for {
		var currentDBTime time.Time
		if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&currentDBTime); err != nil {
			t.Fatal(err)
		}
		if currentDBTime.After(blockedTimestamp.Add(5 * time.Millisecond)) {
			break
		}
		runtime.Gosched()
	}

	// Release snapshot lock by committing Tx 1
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	wg.Wait()

	if finalizeErr != nil {
		t.Fatalf("FinalizeCheckout failed: %v", finalizeErr)
	}

	if !order.CreatedAt.After(blockedTimestamp) {
		t.Fatalf("order.created_at (%v) must be strictly after blockedTimestamp (%v)", order.CreatedAt, blockedTimestamp)
	}

	duration := order.ConfirmationDeadlineAt.Sub(order.CreatedAt)
	if duration != DefaultConfirmationDuration {
		t.Fatalf("confirmation deadline window = %s, want %s", duration, DefaultConfirmationDuration)
	}
}

func TestFinalizeCheckoutSupplierBacked(t *testing.T) {
	db, repo, ctx := setupP53Database(t)

	t.Run("successful supplier-backed checkout", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)

		req := testFinalizePayload(setup.Session.ID)
		order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sup-success")
		if err != nil {
			t.Fatalf("supplier-backed checkout failed: %v", err)
		}

		if len(order.Items) != 1 {
			t.Fatalf("items count = %d, want 1", len(order.Items))
		}
		item := order.Items[0]
		if item.SupplierOfferID == nil || *item.SupplierOfferID != setup.Offer.ID {
			t.Fatalf("supplier_offer_id = %v, want %s", item.SupplierOfferID, setup.Offer.ID)
		}
		if item.SourceSupplierID == nil || *item.SourceSupplierID != setup.Supplier.ID {
			t.Fatalf("source_supplier_id = %v, want %s", item.SourceSupplierID, setup.Supplier.ID)
		}
		if item.SupplierCostMinor == nil || *item.SupplierCostMinor != 1200 {
			t.Fatalf("supplier_cost_minor = %v, want 1200", item.SupplierCostMinor)
		}
		if item.SupplierCostCurrencyCode == nil || *item.SupplierCostCurrencyCode != "EGP" {
			t.Fatalf("supplier_cost_currency_code = %v, want EGP", item.SupplierCostCurrencyCode)
		}
		if item.FulfillmentLocationID != setup.LocationID {
			t.Fatalf("fulfillment_location_id = %s, want %s", item.FulfillmentLocationID, setup.LocationID)
		}
	})

	t.Run("supplier B stock never eligible for supplier A listing", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 0, 2000, 1200)

		supplierB, err := repo.CreateSupplier(ctx, "supB-"+suffix, "Supplier B", "active", nil)
		if err != nil {
			t.Fatal(err)
		}
		supBMarket, err := repo.CreateSupplierMarket(ctx, supplierB.ID, "EG", "active", nil)
		if err != nil {
			t.Fatal(err)
		}
		locB := uuid.NewString()
		if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, supplier_id, supplier_market_id, market_code, code, name, location_type, status) VALUES ($1, $2, $3, 'EG', $4, 'Loc B', 'warehouse', 'active')`, locB, supplierB.ID, supBMarket.ID, "locB-"+suffix); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, uuid.NewString(), locB, setup.SKUID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err = repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-supB-ineligible")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory when Supplier A stock is 0, got %v", err)
		}
	})

	t.Run("store-owned stock never eligible for supplier A listing", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 0, 2000, 1200)

		storeLoc := uuid.NewString()
		if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Store Loc', 'warehouse', 'active')`, storeLoc, setup.Store.ID, "storeLoc-"+suffix); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, uuid.NewString(), storeLoc, setup.SKUID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-store-ineligible")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory when Supplier A stock is 0, got %v", err)
		}
	})

	t.Run("inactive supplier location returns ErrInsufficientInventory", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)
		if _, err := db.Exec(ctx, `UPDATE fulfillment_locations SET status = 'inactive' WHERE id = $1`, setup.LocationID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sup-inact-loc")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory when supplier location is inactive, got %v", err)
		}
	})

	t.Run("inactive supplier offer returns ErrListingUnavailable", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)
		if _, err := db.Exec(ctx, `UPDATE supplier_offers SET status = 'inactive' WHERE id = $1`, setup.Offer.ID); err != nil {
			t.Fatal(err)
		}
		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sup-off-inact")
		if !errors.Is(err, ErrListingUnavailable) {
			t.Fatalf("expected ErrListingUnavailable, got %v", err)
		}
	})

	t.Run("missing current supplier offer price returns ErrListingUnavailable", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)
		if _, err := db.Exec(ctx, `UPDATE supplier_offer_prices SET is_current = false WHERE supplier_offer_id = $1`, setup.Offer.ID); err != nil {
			t.Fatal(err)
		}
		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sup-noprice")
		if !errors.Is(err, ErrListingUnavailable) {
			t.Fatalf("expected ErrListingUnavailable, got %v", err)
		}
	})
}

func TestFinalizeCheckoutSellerOwnedSourceIsolation(t *testing.T) {
	db, repo, ctx := setupP53Database(t)

	t.Run("seller-owned listing cannot allocate another store stock", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

		storeB, err := repo.CreateStore(ctx, setup.Store.SellerID, "EG", "storeb-"+suffix, "Store B", "active", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `UPDATE fulfillment_locations SET store_id = $1 WHERE id = $2`, storeB.ID, setup.LocationID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err = repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-other-store")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory when stock belongs to another store, got %v", err)
		}
	})

	t.Run("seller-owned listing cannot allocate supplier-owned stock", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

		supplier, err := repo.CreateSupplier(ctx, "sup-iso-"+suffix, "Sup Iso", "active", nil)
		if err != nil {
			t.Fatal(err)
		}
		supMarket, err := repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(ctx, `UPDATE fulfillment_locations SET store_id = NULL, supplier_id = $1, supplier_market_id = $2 WHERE id = $3`, supplier.ID, supMarket.ID, setup.LocationID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err = repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-sup-stock-seller-listing")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory when seller listing tries to allocate supplier stock, got %v", err)
		}
	})

	t.Run("inactive seller location rejected", func(t *testing.T) {
		suffix := uuid.NewString()
		setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

		if _, err := db.Exec(ctx, `UPDATE fulfillment_locations SET status = 'inactive' WHERE id = $1`, setup.LocationID); err != nil {
			t.Fatal(err)
		}

		req := testFinalizePayload(setup.Session.ID)
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-inact-loc")
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("expected ErrInsufficientInventory for inactive location, got %v", err)
		}
	})
}

func TestFinalizeCheckoutCommercialPreAcceptanceRace(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Helper Tx 1 locks candidate inventory snapshot to block checkout before final revalidation
	tx1, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()

	var holderPID int32
	if err := tx1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}

	var snapID string
	_ = tx1.QueryRow(ctx, `SELECT id FROM inventory_snapshots WHERE id = $1 FOR UPDATE`, setup.SnapshotID).Scan(&snapID)

	var wg sync.WaitGroup
	var finalizeErr error
	waiterPIDChan := make(chan int32, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		req := testFinalizePayload(setup.Session.ID)
		ctxWaiter := WithTxPIDNotifier(ctx, waiterPIDChan)
		_, finalizeErr = repo.FinalizeCheckout(ctxWaiter, setup.Store.ID, req, "corr-prerace")
	}()

	waiterPID := <-waiterPIDChan

	// Wait until checkout reaches snapshot locking and blocks
	waitForBackendBlockedOnLock(t, db, waiterPID, holderPID)

	// Mutate retail listing price while checkout is blocked
	if _, err := db.Exec(ctx, `UPDATE seller_listing_prices SET amount_minor = 2500 WHERE seller_listing_id = $1 AND is_current = true`, setup.ListingID); err != nil {
		t.Fatal(err)
	}

	// Release checkout
	_ = tx1.Commit(ctx)
	wg.Wait()

	if !errors.Is(finalizeErr, ErrPriceChanged) {
		t.Fatalf("expected ErrPriceChanged for pre-acceptance price race, got %v", finalizeErr)
	}
}

func TestFinalizeCheckoutCommercialPostAcceptanceImmutability(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)

	hookReached := make(chan struct{})
	canContinue := make(chan struct{})
	testHookAfterCommercialAcceptance = func(hookCtx context.Context) error {
		close(hookReached)
		<-canContinue
		return nil
	}
	defer func() { testHookAfterCommercialAcceptance = nil }()

	var wg sync.WaitGroup
	var order Order
	var finalizeErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		req := testFinalizePayload(setup.Session.ID)
		order, finalizeErr = repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-postrace")
	}()

	// Wait until checkout reaches post-acceptance hook inside open transaction
	<-hookReached

	// Create Variant B for the same product
	variantB := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantB, setup.ProductID, "VAR-B-"+suffix); err != nil {
		t.Fatal(err)
	}

	// Mutate commercial data externally while transaction remains open post-acceptance:
	// Re-associate SKU to Variant B
	if _, err := db.Exec(ctx, `UPDATE skus SET variant_id = $1 WHERE id = $2`, variantB, setup.SKUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE seller_listing_prices SET amount_minor = 9999 WHERE seller_listing_id = $1`, setup.ListingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE supplier_offer_prices SET amount_minor = 7777 WHERE supplier_offer_id = $1`, setup.Offer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE seller_listings SET status = 'inactive' WHERE id = $1`, setup.ListingID); err != nil {
		t.Fatal(err)
	}

	// Unblock hook
	close(canContinue)
	wg.Wait()

	if finalizeErr != nil {
		t.Fatalf("FinalizeCheckout failed: %v", finalizeErr)
	}

	// Re-load persisted order and order item from DB
	var dbSubtotal, dbTotal int64
	if err := db.QueryRow(ctx, `SELECT subtotal_minor, total_minor FROM orders WHERE id = $1`, order.ID).Scan(&dbSubtotal, &dbTotal); err != nil {
		t.Fatal(err)
	}
	if dbSubtotal != 2000 || dbTotal != 2000 {
		t.Fatalf("persisted order totals changed: subtotal=%d, total=%d", dbSubtotal, dbTotal)
	}

	var dbUnitPrice, dbCost int64
	var dbVariantID string
	if err := db.QueryRow(ctx, `SELECT unit_price_minor, supplier_cost_minor, variant_id FROM order_items WHERE order_id = $1`, order.ID).Scan(&dbUnitPrice, &dbCost, &dbVariantID); err != nil {
		t.Fatal(err)
	}
	if dbUnitPrice != 2000 || dbCost != 1200 {
		t.Fatalf("persisted item snapshots changed: unit_price=%d, cost=%d", dbUnitPrice, dbCost)
	}
	if dbVariantID != setup.VariantID {
		t.Fatalf("persisted variant_id = %s, want original Variant A (%s), not Variant B (%s)", dbVariantID, setup.VariantID, variantB)
	}
}

func TestFinalizeCheckoutCustomerAddressImmutability(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	customer, err := repo.CreateCustomer(ctx, setup.Store.ID, "EG", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	custAddrID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO customer_addresses (id, customer_id, store_id, recipient_name, address_line_1, city, country_code, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, 'Original Recipient', 'Original Line 1', 'Cairo', 'EG', true, clock_timestamp(), clock_timestamp())
	`, custAddrID, customer.ID, setup.Store.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `UPDATE carts SET customer_id = $1 WHERE id = $2`, customer.ID, setup.Cart.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET customer_id = $1 WHERE id = $2`, customer.ID, setup.Session.ID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-addr-immut")
	if err != nil {
		t.Fatalf("FinalizeCheckout failed: %v", err)
	}

	if _, err := db.Exec(ctx, `UPDATE customer_addresses SET recipient_name = 'Mutated Recipient', address_line_1 = 'Mutated Line 1' WHERE id = $1`, custAddrID); err != nil {
		t.Fatal(err)
	}

	var recipientName, line1 string
	if err := db.QueryRow(ctx, `SELECT recipient_name, address_line_1 FROM order_addresses WHERE order_id = $1`, order.ID).Scan(&recipientName, &line1); err != nil {
		t.Fatal(err)
	}
	if recipientName != "John Doe" || line1 != "123 Main St" {
		t.Fatalf("order_addresses mutated: recipient=%s, line1=%s", recipientName, line1)
	}
}

func TestFinalizeCheckoutSessionCreationLossSafe(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'Prod')`, productID); err != nil {
		t.Fatal(err)
	}
	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}
	listingID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status) VALUES ($1, $2, $3, NULL, 'EG', 'active')`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), listingID); err != nil {
		t.Fatal(err)
	}

	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddCartItem(ctx, store.ID, cartToken, skuID, 1); err != nil {
		t.Fatal(err)
	}

	sessionA, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_ = sessionA

	var orderCountA int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE store_id = $1`, store.ID).Scan(&orderCountA)
	if orderCountA != 0 {
		t.Fatalf("Order created simply by session creation: %d", orderCountA)
	}

	sessionB, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatalf("CreateCheckoutSession B failed: %v", err)
	}

	if sessionB.ID == "" {
		t.Fatal("Session B ID is empty")
	}

	var orderCountB int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE store_id = $1`, store.ID).Scan(&orderCountB)
	if orderCountB != 0 {
		t.Fatalf("orderCountB = %d, want 0", orderCountB)
	}

	var cartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, cart.ID).Scan(&cartStatus)
	if cartStatus != CartStatusActive {
		t.Fatalf("cartStatus = %s, want active", cartStatus)
	}
}

func TestFinalizeCheckoutMarketCurrencyMismatch(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	productID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "prod-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO product_translations (product_id, locale, name) VALUES ($1, 'en', 'Prod')`, productID); err != nil {
		t.Fatal(err)
	}
	variantID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix); err != nil {
		t.Fatal(err)
	}
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix); err != nil {
		t.Fatal(err)
	}
	listingID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status) VALUES ($1, $2, $3, NULL, 'EG', 'active')`, listingID, store.ID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'SAR', true)`, uuid.NewString(), listingID); err != nil {
		t.Fatal(err)
	}
	locationID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, supplier_id, market_code, code, name, location_type, status) VALUES ($1, $2, NULL, 'EG', $3, 'Loc', 'warehouse', 'active')`, locationID, store.ID, "LOC-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, uuid.NewString(), locationID, skuID); err != nil {
		t.Fatal(err)
	}

	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO cart_items (id, cart_id, seller_listing_id, sku_id, quantity, expected_unit_price_minor, expected_currency_code) VALUES ($1, $2, $3, $4, 1, 1000, 'SAR')`, uuid.NewString(), cart.ID, listingID, skuID); err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(session.ID)
	_, err = repo.FinalizeCheckout(ctx, store.ID, req, "corr-mismatch")
	if !errors.Is(err, ErrMarketMismatch) {
		t.Fatalf("expected ErrMarketMismatch when listing/cart currency SAR != Store EGP, got %v", err)
	}

	var orderCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("order created despite market currency mismatch: %d", orderCount)
	}
}

func TestFinalizeCheckoutSupplierOfferAvailabilityUnexpectedDBError(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)

	ctxCancelled, cancel := context.WithCancel(ctx)
	cancel()

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctxCancelled, setup.Store.ID, req, "corr-sup-dberr")
	if err == nil {
		t.Fatal("expected DB error for canceled context, got nil")
	}
	if errors.Is(err, ErrListingUnavailable) || errors.Is(err, ErrPriceChanged) || errors.Is(err, ErrMarketMismatch) {
		t.Fatalf("expected unexpected DB error, got domain error %v", err)
	}

	var orderCount int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE checkout_session_id = $1`, setup.Session.ID).Scan(&orderCount)
	if orderCount != 0 {
		t.Fatalf("orderCount = %d, want 0", orderCount)
	}
}

func TestOrderToPublicPrivacy(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-pub-privacy")
	if err != nil {
		t.Fatal(err)
	}

	pub := order.ToPublic()
	raw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}

	jsonStr := string(raw)
	for _, forbidden := range []string{
		"supplier_cost_minor",
		"supplier_cost_currency_code",
		"source_supplier_id",
		"supplier_offer_id",
		"fulfillment_location_id",
		"inventory_reservation_id",
		"guest_order_access_token_digest",
		"reservation_token",
	} {
		if containsString(jsonStr, forbidden) {
			t.Fatalf("public order DTO exposes private field %s: %s", forbidden, jsonStr)
		}
	}
}

func TestFinalizeCheckoutPersistedListingNoRemap(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	// Create Cart 2 and Session 2 with Listing A BEFORE Listing B exists
	_, token2, err := repo.CreateCart(ctx, setup.Store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddCartItem(ctx, setup.Store.ID, token2, setup.SKUID, 1); err != nil {
		t.Fatal(err)
	}
	session2, _, err := repo.CreateCheckoutSession(ctx, setup.Store.ID, token2, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Subtest 1: Listing A is active -> Checkout Session 1 succeeds with Listing A
	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-noremap-1")
	if err != nil {
		t.Fatalf("FinalizeCheckout failed: %v", err)
	}
	if *order.Items[0].SellerListingID != setup.ListingID {
		t.Fatalf("seller_listing_id = %s, want %s (Listing A)", *order.Items[0].SellerListingID, setup.ListingID)
	}

	// Create Listing B (a newer active listing for the same Product and SKU)
	listingB := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status) VALUES ($1, $2, $3, NULL, 'EG', 'active')`, listingB, setup.Store.ID, setup.ProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current) VALUES ($1, $2, 1000, 'EGP', true)`, uuid.NewString(), listingB); err != nil {
		t.Fatal(err)
	}

	// Deactivate Listing A
	if _, err := db.Exec(ctx, `UPDATE seller_listings SET status = 'inactive' WHERE id = $1`, setup.ListingID); err != nil {
		t.Fatal(err)
	}

	// Subtest 2: Checkout Session 2 (persisted Listing A) fails with ErrListingUnavailable without remapping to Listing B
	req2 := testFinalizePayload(session2.ID)
	_, err = repo.FinalizeCheckout(ctx, setup.Store.ID, req2, "corr-noremap-2")
	if !errors.Is(err, ErrListingUnavailable) {
		t.Fatalf("expected ErrListingUnavailable when Listing A is inactive, got %v", err)
	}
}

func TestFinalizeCheckoutSessionFinalizedCartActiveInvariant(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET status = 'finalized', finalize_fingerprint = 'fp' WHERE id = $1`, setup.Session.ID); err != nil {
		t.Fatal(err)
	}

	req := testFinalizePayload(setup.Session.ID)
	_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-inv-fp")
	if !errors.Is(err, ErrCheckoutCartInvariant) {
		t.Fatalf("expected ErrCheckoutCartInvariant, got %v", err)
	}
}

func TestFinalizeCheckoutImmutabilitySnapshots(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSupplierCheckoutTest(t, db, repo, ctx, suffix, 10, 2000, 1200)

	req := testFinalizePayload(setup.Session.ID)
	order, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-immut")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(ctx, `UPDATE product_translations SET name = 'Mutated Product' WHERE product_id = $1`, setup.ProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE skus SET code = 'MUTATED-SKU' WHERE id = $1`, setup.SKUID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE supplier_offer_prices SET amount_minor = 5555 WHERE supplier_offer_id = $1`, setup.Offer.ID); err != nil {
		t.Fatal(err)
	}

	var pTitle, kCode string
	var supCost int64
	if err := db.QueryRow(ctx, `SELECT product_title_snapshot, sku_code_snapshot, supplier_cost_minor FROM order_items WHERE order_id = $1`, order.ID).Scan(&pTitle, &kCode, &supCost); err != nil {
		t.Fatal(err)
	}
	if pTitle == "Mutated Product" {
		t.Fatalf("product_title_snapshot mutated to %s", pTitle)
	}
	if kCode == "MUTATED-SKU" {
		t.Fatalf("sku_code_snapshot mutated to %s", kCode)
	}
	if supCost != 1200 {
		t.Fatalf("supplier_cost_minor mutated to %d", supCost)
	}

	_, err = db.Exec(ctx, `DELETE FROM supplier_offers WHERE id = $1`, setup.Offer.ID)
	if err == nil {
		t.Fatal("expected foreign key deletion error for supplier offer referenced by order item, got nil")
	}
}

func TestFinalizeCheckoutIncompletePayloadRollback(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	setup := setupSellerCheckoutTest(t, db, repo, ctx, suffix, 10, 1000)

	t.Run("missing recipient name", func(t *testing.T) {
		req := testFinalizePayload(setup.Session.ID)
		req.ShippingAddress.RecipientName = ""
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-no-name")
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("missing contact email", func(t *testing.T) {
		req := testFinalizePayload(setup.Session.ID)
		req.ContactEmail = ""
		_, err := repo.FinalizeCheckout(ctx, setup.Store.ID, req, "corr-no-email")
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput, got %v", err)
		}
	})

	var sessionStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM checkout_sessions WHERE id = $1`, setup.Session.ID).Scan(&sessionStatus)
	if sessionStatus != "open" {
		t.Fatalf("session status = %s, want open", sessionStatus)
	}
	var cartStatus string
	_ = db.QueryRow(ctx, `SELECT status FROM carts WHERE id = $1`, setup.Cart.ID).Scan(&cartStatus)
	if cartStatus != "active" {
		t.Fatalf("cart status = %s, want active", cartStatus)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
