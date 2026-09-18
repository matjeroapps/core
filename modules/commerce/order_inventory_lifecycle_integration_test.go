package commerce

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"core/packages/database"
	"github.com/google/uuid"
)

func createTestOrderWithInventory(t *testing.T, db *database.Pool, repo Repository, ctx context.Context, suffix string, initialOnHand, initialReserved, itemQty int64, deadlineDuration time.Duration) (Store, Order, string, string) {
	t.Helper()
	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "store-"+suffix, "Store "+suffix, "active", nil, suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	uniq := uuid.NewString()[:8]
	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Test Location', 'warehouse', 'active')
	`, locID, store.ID, "LOC-"+uniq); err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()

	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+uniq); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+uniq); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+uniq); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, $4, $5, 1)
	`, snapID, skuID, locID, initialOnHand, initialReserved); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().UTC().Add(deadlineDuration)

	resID := uuid.NewString()
	resToken := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at)
		VALUES ($1, $2, $3, 'held', $4, $5)
	`, resID, resToken, snapID, itemQty, deadline); err != nil {
		t.Fatal(err)
	}

	orderID := uuid.NewString()
	orderNumber := "#" + suffix[:8]
	order := Order{
		ID:                          orderID,
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
		CreatedAt:                   time.Now().UTC(),
		UpdatedAt:                   time.Now().UTC(),
		Items: []OrderItem{
			{
				ID:                     uuid.NewString(),
				OrderID:                orderID,
				FulfillmentLocationID:  locID,
				InventoryReservationID: resID,
				ProductTitleSnapshot:   "Test Product",
				SKUCodeSnapshot:        "SKU-TEST",
				UnitPriceMinor:         1000,
				CurrencyCode:           "EGP",
				Quantity:               itemQty,
				LineTotalMinor:         1000,
				CreatedAt:              time.Now().UTC(),
			},
		},
		Address: &OrderAddress{
			ID:            uuid.NewString(),
			OrderID:       orderID,
			AddressType:   AddressTypeShipping,
			RecipientName: "John Doe",
			AddressLine1:  "123 Street",
			City:          "Cairo",
			CountryCode:   "EG",
			CreatedAt:     time.Now().UTC(),
		},
	}

	createdOrder, err := repo.CreateOrder(ctx, nil, order)
	if err != nil {
		t.Fatal(err)
	}

	return store, createdOrder, snapID, resID
}

func TestSellerConfirmConsumption(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	actor := "seller-actor"
	confirmed, err := repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-1")
	if err != nil {
		t.Fatalf("ConfirmOrder returned error: %v", err)
	}
	if confirmed.Status != OrderStatusConfirmed {
		t.Errorf("expected status confirmed, got %s", confirmed.Status)
	}
	if confirmed.AggregateVersion != 2 {
		t.Errorf("expected aggregate_version 2, got %d", confirmed.AggregateVersion)
	}

	// Verify inventory snapshot: on_hand 10-2=8, reserved 2-2=0
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 8 || reserved != 0 {
		t.Errorf("expected on_hand 8, reserved 0; got on_hand %d, reserved %d", onHand, reserved)
	}

	// Verify reservation state: consumed
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusConsumed {
		t.Errorf("expected reservation status consumed, got %s", resStatus)
	}

	// Verify inventory movement
	var movType string
	var qtyDelta int64
	if err := db.QueryRow(ctx, `SELECT movement_type, quantity_delta FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'reservation_consumed'`, snapID).Scan(&movType, &qtyDelta); err != nil {
		t.Fatal(err)
	}
	if qtyDelta != -2 {
		t.Errorf("expected quantity_delta -2, got %d", qtyDelta)
	}
}

func TestSellerCancelRestock(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	actor := "seller-actor"
	_, err := repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-1")
	if err != nil {
		t.Fatalf("ConfirmOrder returned error: %v", err)
	}

	reason := "out of stock item"
	cancelled, err := repo.CancelConfirmedOrder(ctx, nil, store.ID, order.ID, AuthoritySeller, &actor, &reason, "corr-2")
	if err != nil {
		t.Fatalf("CancelConfirmedOrder returned error: %v", err)
	}
	if cancelled.Status != OrderStatusCancelled {
		t.Errorf("expected status cancelled, got %s", cancelled.Status)
	}

	// Verify snapshot on_hand restocked back from 8 to 10, reserved stays 0
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 || reserved != 0 {
		t.Errorf("expected on_hand 10, reserved 0; got on_hand %d, reserved %d", onHand, reserved)
	}

	// Reservation state remains consumed
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusConsumed {
		t.Errorf("expected reservation status consumed, got %s", resStatus)
	}

	// Verify restock movement
	var qtyDelta int64
	if err := db.QueryRow(ctx, `SELECT quantity_delta FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'order_cancellation_restock'`, snapID).Scan(&qtyDelta); err != nil {
		t.Fatal(err)
	}
	if qtyDelta != 2 {
		t.Errorf("expected quantity_delta +2 for restock, got %d", qtyDelta)
	}
}

func TestConfirmVsExpiryRace(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	actor := "seller-actor"
	var errConfirm, errExpiry error

	go func() {
		defer wg.Done()
		<-startGate
		_, errConfirm = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-race")
	}()

	go func() {
		defer wg.Done()
		<-startGate
		_, errExpiry = repo.ExpirePendingOrder(ctx, nil, order.ID)
	}()

	close(startGate)
	wg.Wait()

	_ = errConfirm
	_ = errExpiry

	// Exactly one wins, the other fails or is a safe no-op
	updatedOrder, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedOrder.Status != OrderStatusConfirmed && updatedOrder.Status != OrderStatusCancelled {
		t.Errorf("expected order status confirmed or cancelled, got %s", updatedOrder.Status)
	}

	// Check reserved_qty in snapshot is 0 (decremented exactly once)
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 0 {
		t.Errorf("expected reserved 0 (decremented once), got %d", reserved)
	}
}

func TestConfirmDeadlineBoundaries(t *testing.T) {
	db, repo, ctx := setupP53Database(t)

	// Case 71: Confirm before deadline -> valid
	suffixValid := uuid.NewString()
	storeV, orderV, _, _ := createTestOrderWithInventory(t, db, repo, ctx, suffixValid, 10, 2, 2, 10*time.Minute)
	actor := "seller"
	_, err := repo.ConfirmOrder(ctx, nil, storeV.ID, orderV.ID, &actor, "")
	if err != nil {
		t.Fatalf("expected valid confirm before deadline, got %v", err)
	}

	// Case 73, 74, 75: Confirm after deadline before scheduler -> rejected without inventory changes
	suffixLate := uuid.NewString()
	storeL, orderL, snapIDL, resIDL := createTestOrderWithInventory(t, db, repo, ctx, suffixLate, 10, 2, 2, -10*time.Minute)

	_, errLate := repo.ConfirmOrder(ctx, nil, storeL.ID, orderL.ID, &actor, "")
	if errLate != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition on late confirm, got %v", errLate)
	}

	// Verify reservation still held (Case 74)
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resIDL).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusHeld {
		t.Errorf("expected reservation still held after rejected confirm, got %s", resStatus)
	}

	// Verify snapshot on_hand unchanged (Case 75)
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapIDL).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 || reserved != 2 {
		t.Errorf("expected on_hand 10, reserved 2; got on_hand %d, reserved %d", onHand, reserved)
	}
}

func TestExpiryRetryIdempotency(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	_, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, -10*time.Minute)

	// First expiry run
	expired1, err1 := repo.ExpirePendingOrder(ctx, nil, order.ID)
	if err1 != nil {
		t.Fatalf("first ExpirePendingOrder failed: %v", err1)
	}
	if expired1.Status != OrderStatusCancelled {
		t.Errorf("expected status cancelled, got %s", expired1.Status)
	}

	// Second expiry run (retry)
	_, err2 := repo.ExpirePendingOrder(ctx, nil, order.ID)
	if err2 != nil {
		t.Fatalf("second ExpirePendingOrder failed: %v", err2)
	}

	// Check reserved_qty is 0 (decremented only once from 2 to 0)
	var reserved int64
	if err := db.QueryRow(ctx, `SELECT reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 0 {
		t.Errorf("expected reserved_qty 0 after retry, got %d", reserved)
	}

	// Check reservation status is expired
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusExpired {
		t.Errorf("expected reservation status expired, got %s", resStatus)
	}
}

func TestAllPhase5EnabledAndRejectedTransitions(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, _, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	// Case 60: Rejected transition pending -> processing
	_, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusProcessing, AuthoritySeller, &actor, nil, "")
	if err != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition for pending -> processing, got %v", err)
	}

	// Case 59: Enabled pending -> confirmed
	confirmed, err := repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "")
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}
	if confirmed.Status != OrderStatusConfirmed {
		t.Errorf("expected confirmed, got %s", confirmed.Status)
	}

	// Case 59: Enabled confirmed -> processing
	processing, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusProcessing, AuthoritySeller, &actor, nil, "")
	if err != nil {
		t.Fatalf("AdvanceOrderStatus to processing failed: %v", err)
	}
	if processing.Status != OrderStatusProcessing {
		t.Errorf("expected processing, got %s", processing.Status)
	}

	// Case 59: Enabled processing -> ready_for_shipping
	ready, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusReadyForShipping, AuthoritySeller, &actor, nil, "")
	if err != nil {
		t.Fatalf("AdvanceOrderStatus to ready_for_shipping failed: %v", err)
	}
	if ready.Status != OrderStatusReadyForShipping {
		t.Errorf("expected ready_for_shipping, got %s", ready.Status)
	}
}

func TestMultiItemAggregation(t *testing.T) {
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
	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Test Location', 'warehouse', 'active')
	`, locID, store.ID, "LOC-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID1 := uuid.NewString()

	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID1, variantID, "SKU-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString() // Single snapshot referenced by 2 items!
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, 20, 5, 1)
	`, snapID, skuID1, locID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().UTC().Add(30 * time.Minute)

	resID1 := uuid.NewString()
	resID2 := uuid.NewString()
	if _, err := db.Exec(ctx, `
		INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at)
		VALUES ($1, $2, $3, 'held', 2, $4), ($5, $6, $3, 'held', 3, $4)
	`, resID1, uuid.NewString(), snapID, deadline, resID2, uuid.NewString()); err != nil {
		t.Fatal(err)
	}

	orderID := uuid.NewString()
	order := Order{
		ID:                          orderID,
		OrderNumber:                 "#" + suffix[:8],
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               2000,
		TotalMinor:                  2000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   time.Now().UTC(),
		UpdatedAt:                   time.Now().UTC(),
		Items: []OrderItem{
			{
				ID:                     uuid.NewString(),
				OrderID:                orderID,
				FulfillmentLocationID:  locID,
				InventoryReservationID: resID1,
				ProductTitleSnapshot:   "Item 1",
				SKUCodeSnapshot:        "SKU-1",
				UnitPriceMinor:         1000,
				CurrencyCode:           "EGP",
				Quantity:               2,
				LineTotalMinor:         1000,
				CreatedAt:              time.Now().UTC(),
			},
			{
				ID:                     uuid.NewString(),
				OrderID:                orderID,
				FulfillmentLocationID:  locID,
				InventoryReservationID: resID2,
				ProductTitleSnapshot:   "Item 2",
				SKUCodeSnapshot:        "SKU-1",
				UnitPriceMinor:         1000,
				CurrencyCode:           "EGP",
				Quantity:               3,
				LineTotalMinor:         1000,
				CreatedAt:              time.Now().UTC(),
			},
		},
	}

	_, err = repo.CreateOrder(ctx, nil, order)
	if err != nil {
		t.Fatal(err)
	}

	actor := "seller"
	_, err = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "")
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	// Verify snapshot on_hand 20-5=15, reserved 5-5=0 (Matrix Case 116)
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 15 || reserved != 0 {
		t.Errorf("expected aggregated on_hand 15, reserved 0; got on_hand %d, reserved %d", onHand, reserved)
	}

	// Verify exactly ONE movement created for snapID with quantity_delta -5
	var movCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'reservation_consumed'`, snapID).Scan(&movCount); err != nil {
		t.Fatal(err)
	}
	if movCount != 1 {
		t.Errorf("expected exactly 1 aggregated movement, got %d", movCount)
	}
}

func TestConfirmLockLinearizedDeadlineWithoutSleep(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	// Deadline expires in 200 milliseconds
	deadlineDuration := 200 * time.Millisecond
	store, order, _, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, deadlineDuration)

	// Hold Order lock in Tx A
	txA, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(ctx)

	var lockedID string
	if err := txA.QueryRow(ctx, `SELECT id FROM orders WHERE id = $1 FOR UPDATE`, order.ID).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}

	actor := "seller"
	var errConfirm error
	done := make(chan struct{})

	go func() {
		// ConfirmOrder starts in Tx B and blocks on order lock
		_, errConfirm = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "")
		close(done)
	}()

	// Deterministically poll PostgreSQL until:
	// 1. We observe a backend blocked waiting on lock
	// 2. PostgreSQL clock_timestamp() >= confirmation_deadline_at
	for {
		var isWaiting bool
		err := db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks l
				JOIN pg_stat_activity a ON l.pid = a.pid
				WHERE l.granted = false
				  AND a.wait_event_type = 'Lock'
			)
		`).Scan(&isWaiting)
		if err != nil {
			t.Fatal(err)
		}

		var nowPastDeadline bool
		err = db.QueryRow(ctx, `SELECT clock_timestamp() >= $1`, order.ConfirmationDeadlineAt).Scan(&nowPastDeadline)
		if err != nil {
			t.Fatal(err)
		}

		if isWaiting && nowPastDeadline {
			break
		}
	}

	// Release Tx A lock
	if err := txA.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	<-done

	// ConfirmOrder acquired lock after deadline passed, so decision_now = clock_timestamp() > deadline -> must reject!
	if errConfirm != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition on lock-linearized late confirm, got %v", errConfirm)
	}
}

func TestAdvanceOrderStatusCannotBypassInventoryLifecycle(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	// 1. Reject pending -> confirmed via AdvanceOrderStatus
	_, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusConfirmed, AuthoritySeller, &actor, nil, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for AdvanceOrderStatus pending -> confirmed, got %v", err)
	}

	// 2. Reject pending -> cancelled via AdvanceOrderStatus
	_, err = repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusCancelled, AuthoritySeller, &actor, nil, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for AdvanceOrderStatus pending -> cancelled, got %v", err)
	}

	// Verify order state unchanged
	orderFresh, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if orderFresh.Status != OrderStatusPending || orderFresh.AggregateVersion != 1 {
		t.Errorf("expected pending order with version 1, got status %s version %d", orderFresh.Status, orderFresh.AggregateVersion)
	}

	// Verify reservation unchanged
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusHeld {
		t.Errorf("expected reservation status held, got %s", resStatus)
	}

	// Verify snapshot unchanged
	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 || reserved != 2 {
		t.Errorf("expected on_hand 10 reserved 2, got on_hand %d reserved %d", onHand, reserved)
	}

	// Verify 0 movements, 0 timeline, 0 outbox
	var movCount, timelineCount, outboxCount int
	db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount)
	db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount)
	db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount)
	if movCount != 0 || timelineCount != 0 || outboxCount != 0 {
		t.Errorf("expected 0 side effects, got mov %d timeline %d outbox %d", movCount, timelineCount, outboxCount)
	}

	// 3. Verify confirmed -> processing and processing -> ready_for_shipping work properly via AdvanceOrderStatus
	_, err = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "")
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	proc, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusProcessing, AuthoritySeller, &actor, nil, "")
	if err != nil || proc.Status != OrderStatusProcessing {
		t.Fatalf("AdvanceOrderStatus to processing failed: %v", err)
	}

	// Reject confirmed/processing -> cancelled via AdvanceOrderStatus
	_, err = repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusCancelled, AuthoritySeller, &actor, nil, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for processing -> cancelled via AdvanceOrderStatus, got %v", err)
	}

	ready, err := repo.AdvanceOrderStatus(ctx, nil, store.ID, order.ID, OrderStatusReadyForShipping, AuthoritySeller, &actor, nil, "")
	if err != nil || ready.Status != OrderStatusReadyForShipping {
		t.Fatalf("AdvanceOrderStatus to ready_for_shipping failed: %v", err)
	}
}

func TestLifecycleCommandRejectsNonTransactionalExecutor(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	// Passing pool `db` (which is non-transactional DBExecutor) directly to confirmOrderExec / ConfirmOrder
	_, err := repo.ConfirmOrder(ctx, db, store.ID, order.ID, &actor, "")
	if err == nil || !strings.Contains(err.Error(), "transaction required") {
		t.Fatalf("expected transaction required error, got %v", err)
	}

	// Verify complete state unchanged
	orderFresh, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if orderFresh.Status != OrderStatusPending {
		t.Errorf("expected status pending, got %s", orderFresh.Status)
	}

	var resStatus string
	db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus)
	if resStatus != ReservationStatusHeld {
		t.Errorf("expected reservation status held, got %s", resStatus)
	}

	var onHand, reserved int64
	db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved)
	if onHand != 10 || reserved != 2 {
		t.Errorf("expected snapshot on_hand 10 reserved 2, got on_hand %d reserved %d", onHand, reserved)
	}
}

func TestHoldReservationRequiresTransaction(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()

	_, _, snapID, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 0, 2, 30*time.Minute)

	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)

	params := HoldReservationParams{
		SnapshotID:             snapID,
		Quantity:               2,
		ReservationToken:       uuid.NewString(),
		ConfirmationDeadlineAt: deadline,
		DecisionNow:            now,
	}

	// 1. Calling with non-transactional pool -> rejected
	_, err := repo.HoldReservation(ctx, db, params)
	if err == nil || !strings.Contains(err.Error(), "transaction required") {
		t.Fatalf("expected transaction required error, got %v", err)
	}

	// 2. Calling with deadline <= decisionNow -> rejected
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	invalidParams := params
	invalidParams.ConfirmationDeadlineAt = now
	_, err = repo.HoldReservation(ctx, tx, invalidParams)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for invalid deadline sequence, got %v", err)
	}

	// 3. Valid call inside tx -> success
	res, err := repo.HoldReservation(ctx, tx, params)
	if err != nil {
		t.Fatalf("HoldReservation failed: %v", err)
	}
	if res.Status != ReservationStatusHeld || res.Quantity != 2 {
		t.Errorf("expected held reservation for quantity 2, got status %s qty %d", res.Status, res.Quantity)
	}

	// Verify snapshot reserved_qty = 2, version = 2
	var reserved, version int64
	if err := tx.QueryRow(ctx, `SELECT reserved_qty, version FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&reserved, &version); err != nil {
		t.Fatal(err)
	}
	if reserved != 2 || version != 2 {
		t.Errorf("expected reserved 2 version 2, got reserved %d version %d", reserved, version)
	}

	// Verify movement quantity_delta = 0
	var delta int64
	if err := tx.QueryRow(ctx, `SELECT quantity_delta FROM inventory_movements WHERE inventory_snapshot_id = $1 AND movement_type = 'reservation_held'`, snapID).Scan(&delta); err != nil {
		t.Fatal(err)
	}
	if delta != 0 {
		t.Errorf("expected movement quantity_delta 0, got %d", delta)
	}
}

func TestSchedulerCannotUseCancelPendingOrder(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	// Call CancelPendingOrder with AuthorityScheduler -> must reject!
	_, err := repo.CancelPendingOrder(ctx, nil, store.ID, order.ID, AuthorityScheduler, nil, nil, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition when scheduler calls CancelPendingOrder, got %v", err)
	}

	// Assert order unchanged
	orderFresh, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if orderFresh.Status != OrderStatusPending {
		t.Errorf("expected pending, got %s", orderFresh.Status)
	}

	// Now run ExpirePendingOrder after deadline passes
	if _, err := db.Exec(ctx, `UPDATE orders SET confirmation_deadline_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE inventory_reservations SET expires_at = (SELECT confirmation_deadline_at FROM orders WHERE id = $1) WHERE id = $2`, order.ID, resID); err != nil {
		t.Fatal(err)
	}

	expired, err := repo.ExpirePendingOrder(ctx, nil, order.ID)
	if err != nil {
		t.Fatalf("ExpirePendingOrder failed: %v", err)
	}
	if expired.Status != OrderStatusCancelled || expired.CancellationReason == nil || *expired.CancellationReason != "confirmation_timeout" {
		t.Errorf("expected cancelled with confirmation_timeout, got status %s", expired.Status)
	}

	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusExpired {
		t.Errorf("expected reservation status expired, got %s", resStatus)
	}

	var movType string
	var delta int64
	if err := db.QueryRow(ctx, `SELECT movement_type, quantity_delta FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movType, &delta); err != nil {
		t.Fatal(err)
	}
	if movType != MovementTypeReservationExpired || delta != 0 {
		t.Errorf("expected movement reservation_expired delta 0, got %s %d", movType, delta)
	}
}

func TestExpiryRejectsMixedReservationStates(t *testing.T) {
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
	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locID, store.ID, "LOC-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 5, 1)`, snapID, skuID, locID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().UTC().Add(-10 * time.Minute) // Past deadline

	resID1 := uuid.NewString()
	resID2 := uuid.NewString()
	// Mixed state: resID1 = held, resID2 = released!
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't1', $2, 'held', 2, $3)`, resID1, snapID, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't2', $2, 'released', 3, $3)`, resID2, snapID, deadline); err != nil {
		t.Fatal(err)
	}

	orderID := uuid.NewString()
	order := Order{
		ID:                          orderID,
		OrderNumber:                 "#" + suffix[:8],
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               2000,
		TotalMinor:                  2000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   time.Now().UTC(),
		UpdatedAt:                   time.Now().UTC(),
		Items: []OrderItem{
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID1, ProductTitleSnapshot: "P1", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 2, LineTotalMinor: 1000},
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID2, ProductTitleSnapshot: "P2", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 3, LineTotalMinor: 1000},
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, order); err != nil {
		t.Fatal(err)
	}

	// ExpirePendingOrder must fail closed!
	_, err = repo.ExpirePendingOrder(ctx, nil, order.ID)
	if !errors.Is(err, ErrInternalError) {
		t.Errorf("expected ErrInternalError on mixed reservation state expiry, got %v", err)
	}

	// Assert order remains pending
	orderFresh, _ := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if orderFresh.Status != OrderStatusPending {
		t.Errorf("expected order status pending after failed expiry, got %s", orderFresh.Status)
	}

	// Assert res1 remains held, res2 remains released
	var s1, s2 string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID1).Scan(&s1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID2).Scan(&s2); err != nil {
		t.Fatal(err)
	}
	if s1 != ReservationStatusHeld || s2 != ReservationStatusReleased {
		t.Errorf("expected res1 held res2 released, got %s %s", s1, s2)
	}

	// Assert snapshot reserved_qty unchanged at 5
	var reserved int64
	if err := db.QueryRow(ctx, `SELECT reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 5 {
		t.Errorf("expected reserved_qty 5, got %d", reserved)
	}
}

func TestPendingCancelRejectsMixedReservationStates(t *testing.T) {
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
	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locID, store.ID, "LOC-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 5, 1)`, snapID, skuID, locID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().UTC().Add(30 * time.Minute)

	resID1 := uuid.NewString()
	resID2 := uuid.NewString()
	// Mixed state: resID1 = held, resID2 = expired!
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't1', $2, 'held', 2, $3)`, resID1, snapID, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't2', $2, 'expired', 3, $3)`, resID2, snapID, deadline); err != nil {
		t.Fatal(err)
	}

	orderID := uuid.NewString()
	order := Order{
		ID:                          orderID,
		OrderNumber:                 "#" + suffix[:8],
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusPending,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               2000,
		TotalMinor:                  2000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            1,
		CreatedAt:                   time.Now().UTC(),
		UpdatedAt:                   time.Now().UTC(),
		Items: []OrderItem{
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID1, ProductTitleSnapshot: "P1", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 2, LineTotalMinor: 1000},
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID2, ProductTitleSnapshot: "P2", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 3, LineTotalMinor: 1000},
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, order); err != nil {
		t.Fatal(err)
	}

	_, err = repo.CancelPendingOrder(ctx, nil, store.ID, order.ID, AuthorityCustomer, nil, nil, "")
	if !errors.Is(err, ErrInternalError) {
		t.Errorf("expected ErrInternalError on mixed reservation state pending cancel, got %v", err)
	}
}

func TestPostConfirmCancelRejectsMixedReservationStates(t *testing.T) {
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
	_, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	locID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status) VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')`, locID, store.ID, "LOC-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "slug-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "VAR-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, $3, 'active')`, skuID, variantID, "SKU-"+suffix[:8]); err != nil {
		t.Fatal(err)
	}

	snapID := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version) VALUES ($1, $2, $3, 10, 0, 1)`, snapID, skuID, locID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().UTC().Add(30 * time.Minute)

	resID1 := uuid.NewString()
	resID2 := uuid.NewString()
	// Mixed state: resID1 = consumed, resID2 = held!
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't1', $2, 'consumed', 2, $3)`, resID1, snapID, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO inventory_reservations (id, reservation_token, inventory_snapshot_id, status, quantity, expires_at) VALUES ($1, 't2', $2, 'held', 3, $3)`, resID2, snapID, deadline); err != nil {
		t.Fatal(err)
	}

	orderID := uuid.NewString()
	order := Order{
		ID:                          orderID,
		OrderNumber:                 "#" + suffix[:8],
		StoreID:                     store.ID,
		MarketCode:                  "EG",
		CheckoutSessionID:           session.ID,
		Status:                      OrderStatusConfirmed,
		CurrencyCode:                "EGP",
		GuestOrderAccessTokenDigest: session.GuestOrderAccessTokenDigest,
		SubtotalMinor:               2000,
		TotalMinor:                  2000,
		ConfirmationDeadlineAt:      deadline,
		AggregateVersion:            2,
		CreatedAt:                   time.Now().UTC(),
		UpdatedAt:                   time.Now().UTC(),
		Items: []OrderItem{
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID1, ProductTitleSnapshot: "P1", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 2, LineTotalMinor: 1000},
			{ID: uuid.NewString(), OrderID: orderID, FulfillmentLocationID: locID, InventoryReservationID: resID2, ProductTitleSnapshot: "P2", SKUCodeSnapshot: "S1", UnitPriceMinor: 1000, CurrencyCode: "EGP", Quantity: 3, LineTotalMinor: 1000},
		},
	}
	if _, err := repo.CreateOrder(ctx, nil, order); err != nil {
		t.Fatal(err)
	}

	actor := "seller"
	_, err = repo.CancelConfirmedOrder(ctx, nil, store.ID, order.ID, AuthoritySeller, &actor, nil, "")
	if !errors.Is(err, ErrInternalError) {
		t.Errorf("expected ErrInternalError on mixed reservation state post-confirm cancel, got %v", err)
	}

	var onHand int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 {
		t.Errorf("expected on_hand_qty 10 (no partial restock), got %d", onHand)
	}
}

func TestReservationDeadlineMismatchFailsClosed(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, _, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	// Tamper reservation expires_at to differ from order.ConfirmationDeadlineAt
	db.Exec(ctx, `UPDATE inventory_reservations SET expires_at = $1 WHERE id = $2`, order.ConfirmationDeadlineAt.Add(5*time.Minute), resID)

	actor := "seller"
	_, err := repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "")
	if !errors.Is(err, ErrInternalError) {
		t.Errorf("expected ErrInternalError on deadline mismatch confirm, got %v", err)
	}
}

func TestConfirmVsExpirySellerWinsUnderLockContention(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	lockedCh := make(chan struct{})
	continueCh := make(chan struct{})

	var once1 sync.Once
	testHookAfterOrderLock = func(ctx context.Context, oID string) {
		if oID == order.ID {
			once1.Do(func() {
				close(lockedCh)
				<-continueCh
			})
		}
	}
	defer func() { testHookAfterOrderLock = nil }()

	var sellerOrder Order
	var sellerErr error
	go func() {
		sellerOrder, sellerErr = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-seller-win")
	}()

	// Wait until Seller holds Order lock FOR UPDATE
	<-lockedCh

	// Expiry concurrently attempts same order and blocks on lock
	var expiryOrder Order
	var expiryErr error
	expiryDone := make(chan struct{})
	go func() {
		expiryOrder, expiryErr = repo.ExpirePendingOrder(ctx, nil, order.ID)
		close(expiryDone)
	}()

	// Poll pg_locks + pg_stat_activity to prove Expiry is ACTUALLY WAITING on lock
	for {
		var isWaiting bool
		err := db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks l
				JOIN pg_stat_activity a ON l.pid = a.pid
				WHERE l.granted = false
				  AND a.wait_event_type = 'Lock'
			)
		`).Scan(&isWaiting)
		if err != nil {
			t.Fatal(err)
		}
		if isWaiting {
			break
		}
	}

	// Unblock Seller to commit transaction
	close(continueCh)

	<-expiryDone

	if sellerErr != nil {
		t.Fatalf("expected seller confirm success, got %v", sellerErr)
	}
	if sellerOrder.Status != OrderStatusConfirmed {
		t.Fatalf("expected seller order status confirmed, got %s", sellerOrder.Status)
	}
	if expiryErr != nil {
		t.Fatalf("expected expiry success (no-op), got %v", expiryErr)
	}
	if expiryOrder.Status != OrderStatusConfirmed {
		t.Fatalf("expected expiry returned order status confirmed, got %s", expiryOrder.Status)
	}

	// Assert exactly 1 business effect
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusConsumed {
		t.Errorf("expected reservation consumed, got %s", resStatus)
	}

	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 8 || reserved != 0 {
		t.Errorf("expected on_hand 8, reserved 0, got on_hand %d reserved %d", onHand, reserved)
	}

	var movCount, timelineCount, outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if movCount != 1 || timelineCount != 1 || outboxCount != 1 {
		t.Errorf("expected 1 movement, 1 timeline, 1 outbox; got mov %d timeline %d outbox %d", movCount, timelineCount, outboxCount)
	}
}

func TestConfirmVsExpirySchedulerWinsUnderLockContention(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, -10*time.Minute)
	actor := "seller"

	lockedCh := make(chan struct{})
	continueCh := make(chan struct{})

	var once2 sync.Once
	testHookAfterOrderLock = func(ctx context.Context, oID string) {
		if oID == order.ID {
			once2.Do(func() {
				close(lockedCh)
				<-continueCh
			})
		}
	}
	defer func() { testHookAfterOrderLock = nil }()

	var expiryOrder Order
	var expiryErr error
	expiryDone := make(chan struct{})
	go func() {
		expiryOrder, expiryErr = repo.ExpirePendingOrder(ctx, nil, order.ID)
		close(expiryDone)
	}()

	// Wait until Expiry holds Order lock FOR UPDATE
	<-lockedCh

	// Seller concurrently attempts confirmation and blocks on lock
	var sellerOrder Order
	var sellerErr error
	sellerDone := make(chan struct{})
	go func() {
		sellerOrder, sellerErr = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-seller")
		close(sellerDone)
	}()

	// Poll pg_locks + pg_stat_activity to prove Seller is ACTUALLY WAITING on lock
	for {
		var isWaiting bool
		err := db.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks l
				JOIN pg_stat_activity a ON l.pid = a.pid
				WHERE l.granted = false
				  AND a.wait_event_type = 'Lock'
			)
		`).Scan(&isWaiting)
		if err != nil {
			t.Fatal(err)
		}
		if isWaiting {
			break
		}
	}

	// Unblock Expiry to commit transaction
	close(continueCh)

	<-sellerDone
	<-expiryDone

	if expiryErr != nil {
		t.Fatalf("expected expiry success, got %v", expiryErr)
	}
	if expiryOrder.Status != OrderStatusCancelled || expiryOrder.CancellationReason == nil || *expiryOrder.CancellationReason != "confirmation_timeout" {
		t.Fatalf("expected expiry cancelled with confirmation_timeout, got status %s", expiryOrder.Status)
	}
	if sellerErr != ErrInvalidTransition {
		t.Fatalf("expected seller confirm rejected with ErrInvalidTransition, got %v", sellerErr)
	}
	_ = sellerOrder

	// Assert exactly 1 business effect
	var resStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus); err != nil {
		t.Fatal(err)
	}
	if resStatus != ReservationStatusExpired {
		t.Errorf("expected reservation expired, got %s", resStatus)
	}

	var onHand, reserved int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&onHand, &reserved); err != nil {
		t.Fatal(err)
	}
	if onHand != 10 || reserved != 0 {
		t.Errorf("expected on_hand 10, reserved 0, got on_hand %d reserved %d", onHand, reserved)
	}

	var movCount, timelineCount, outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if movCount != 1 || timelineCount != 1 || outboxCount != 1 {
		t.Errorf("expected 1 movement, 1 timeline, 1 outbox; got mov %d timeline %d outbox %d", movCount, timelineCount, outboxCount)
	}
}

func TestConfirmVsCancelExactlyOneWins(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, _ := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	actor := "seller"
	var errConfirm, errCancel error

	go func() {
		defer wg.Done()
		<-startGate
		_, errConfirm = repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-confirm")
	}()

	go func() {
		defer wg.Done()
		<-startGate
		_, errCancel = repo.CancelPendingOrder(ctx, nil, store.ID, order.ID, AuthorityCustomer, nil, nil, "corr-cancel")
	}()

	close(startGate)
	wg.Wait()

	_ = errConfirm
	_ = errCancel

	finalOrder, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}

	if finalOrder.Status != OrderStatusConfirmed && finalOrder.Status != OrderStatusCancelled {
		t.Fatalf("expected status confirmed or cancelled, got %s", finalOrder.Status)
	}

	var reserved int64
	db.QueryRow(ctx, `SELECT reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&reserved)
	if reserved != 0 {
		t.Errorf("expected reserved_qty 0 (decremented exactly once), got %d", reserved)
	}

	var movCount, timelineCount, outboxCount int
	db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount)
	db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount)
	db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount)
	if movCount != 1 || timelineCount != 1 || outboxCount != 1 {
		t.Errorf("expected 1 movement, 1 timeline, 1 outbox; got mov %d timeline %d outbox %d", movCount, timelineCount, outboxCount)
	}
}

func TestCancelVsExpiryExactlyOneWins(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, -10*time.Minute)

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	var errCancel, errExpiry error

	go func() {
		defer wg.Done()
		<-startGate
		_, errCancel = repo.CancelPendingOrder(ctx, nil, store.ID, order.ID, AuthorityCustomer, nil, nil, "corr-cancel")
	}()

	go func() {
		defer wg.Done()
		<-startGate
		_, errExpiry = repo.ExpirePendingOrder(ctx, nil, order.ID)
	}()

	close(startGate)
	wg.Wait()

	_ = errCancel
	_ = errExpiry

	finalOrder, err := repo.GetOrderByID(ctx, nil, store.ID, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalOrder.Status != OrderStatusCancelled {
		t.Fatalf("expected order cancelled, got %s", finalOrder.Status)
	}

	var resStatus string
	db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&resStatus)
	if resStatus != ReservationStatusReleased && resStatus != ReservationStatusExpired {
		t.Errorf("expected reservation status released or expired, got %s", resStatus)
	}

	var reserved int64
	db.QueryRow(ctx, `SELECT reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&reserved)
	if reserved != 0 {
		t.Errorf("expected reserved_qty 0 (decremented exactly once), got %d", reserved)
	}

	var movCount, timelineCount, outboxCount int
	db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount)
	db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount)
	db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount)
	if movCount != 1 || timelineCount != 1 || outboxCount != 1 {
		t.Errorf("expected 1 movement, 1 timeline, 1 outbox; got mov %d timeline %d outbox %d", movCount, timelineCount, outboxCount)
	}
}

func TestFullAtomicRollbackOnFailure(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	// Calling ConfirmOrder with non-tx exec fails before mutation
	_, err := repo.ConfirmOrder(ctx, db, store.ID, order.ID, &actor, "")
	if err == nil {
		t.Fatal("expected error")
	}

	// Verify all 6 tables have zero partial effects
	var oStatus string
	db.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, order.ID).Scan(&oStatus)

	var snapOnHand, snapReserved int64
	db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&snapOnHand, &snapReserved)

	var rStatus string
	db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&rStatus)

	var movCount, timelineCount, outboxCount int
	db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount)
	db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount)
	db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount)

	if oStatus != OrderStatusPending || snapOnHand != 10 || snapReserved != 2 || rStatus != ReservationStatusHeld || movCount != 0 || timelineCount != 0 || outboxCount != 0 {
		t.Errorf("atomic rollback assertion failed: order=%s snap=(%d,%d) res=%s mov=%d timeline=%d outbox=%d", oStatus, snapOnHand, snapReserved, rStatus, movCount, timelineCount, outboxCount)
	}
}

func TestLifecycleTransactionRollsBackAfterDownstreamFailure(t *testing.T) {
	db, repo, ctx := setupP53Database(t)
	suffix := uuid.NewString()
	store, order, snapID, resID := createTestOrderWithInventory(t, db, repo, ctx, suffix, 10, 2, 2, 30*time.Minute)
	actor := "seller"

	// Inject failure at outbox enqueue (AFTER snapshot, reservation, movement, order update, and timeline insert have run)
	testHookBeforeOutboxEnqueue = func(ctx context.Context) error {
		return errors.New("simulated outbox enqueue failure after mutations")
	}
	defer func() { testHookBeforeOutboxEnqueue = nil }()

	_, err := repo.ConfirmOrder(ctx, nil, store.ID, order.ID, &actor, "corr-rollback-test")
	if err == nil || !strings.Contains(err.Error(), "simulated outbox enqueue failure") {
		t.Fatalf("expected simulated outbox error, got %v", err)
	}

	// Verify complete PostgreSQL transaction rollback across all 6 tables after mutations executed
	var oStatus string
	var oVersion int64
	if err := db.QueryRow(ctx, `SELECT status, aggregate_version FROM orders WHERE id = $1`, order.ID).Scan(&oStatus, &oVersion); err != nil {
		t.Fatal(err)
	}

	var snapOnHand, snapReserved, snapVersion int64
	if err := db.QueryRow(ctx, `SELECT on_hand_qty, reserved_qty, version FROM inventory_snapshots WHERE id = $1`, snapID).Scan(&snapOnHand, &snapReserved, &snapVersion); err != nil {
		t.Fatal(err)
	}

	var rStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id = $1`, resID).Scan(&rStatus); err != nil {
		t.Fatal(err)
	}

	var movCount, timelineCount, outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM inventory_movements WHERE inventory_snapshot_id = $1`, snapID).Scan(&movCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM order_timeline WHERE order_id = $1`, order.ID).Scan(&timelineCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1`, order.ID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}

	if oStatus != OrderStatusPending || oVersion != 1 {
		t.Errorf("expected order status pending version 1, got status %s version %d", oStatus, oVersion)
	}
	if snapOnHand != 10 || snapReserved != 2 || snapVersion != 1 {
		t.Errorf("expected snapshot 10/2 version 1, got on_hand %d reserved %d version %d", snapOnHand, snapReserved, snapVersion)
	}
	if rStatus != ReservationStatusHeld {
		t.Errorf("expected reservation status held, got %s", rStatus)
	}
	if movCount != 0 {
		t.Errorf("expected 0 movements after rollback, got %d", movCount)
	}
	if timelineCount != 0 {
		t.Errorf("expected 0 timeline entries after rollback, got %d", timelineCount)
	}
	if outboxCount != 0 {
		t.Errorf("expected 0 outbox events after rollback, got %d", outboxCount)
	}
}
