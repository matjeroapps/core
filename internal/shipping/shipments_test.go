package shipping_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"core/internal/shipping"
	"core/packages/database"
)

func createSecondOrderForStore(t *testing.T, db *database.Pool, ctx context.Context, storeID, locID string) (string, string) {
	t.Helper()
	orderID := uuid.NewString()
	cartID := uuid.NewString()
	sessionID := uuid.NewString()
	orderItemID := uuid.NewString()
	skuID := uuid.NewString()
	variantID := uuid.NewString()
	productID := uuid.NewString()
	snapshotID := uuid.NewString()
	resID := uuid.NewString()
	digest := make([]byte, 32)

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO carts (id, store_id, market_code, cart_token_digest, status) VALUES ($1, $2, 'EG', $3, 'active')
	`, cartID, storeID, "token-"+cartID)
	if err != nil {
		t.Fatalf("seed second cart: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'open', $4, $5)
	`, sessionID, storeID, cartID, time.Now().Add(time.Hour), digest)
	if err != nil {
		t.Fatalf("seed second checkout session: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, order_number, store_id, market_code, checkout_session_id, status, currency_code, subtotal_minor, total_minor, confirmation_deadline_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'EG', $4, 'confirmed', 'EGP', 2000, 2000, $5, $6)
	`, orderID, "#100002", storeID, sessionID, time.Now().Add(time.Hour), digest)
	if err != nil {
		t.Fatalf("seed second order: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')
	`, productID, "slug-"+productID[:8])
	if err != nil {
		t.Fatalf("seed second product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, 'VAR-02', 'active')
	`, variantID, productID)
	if err != nil {
		t.Fatalf("seed second variant: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, 'SKU-002', 'active')
	`, skuID, variantID)
	if err != nil {
		t.Fatalf("seed second sku: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty)
		VALUES ($1, $2, $3, 100, 1)
	`, snapshotID, locID, skuID)
	if err != nil {
		t.Fatalf("seed second snapshot: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO inventory_reservations (id, inventory_snapshot_id, quantity, status, reservation_token)
		VALUES ($1, $2, 1, 'active', $3)
	`, resID, snapshotID, "res-token-"+resID)
	if err != nil {
		t.Fatalf("seed second reservation: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO order_items (id, order_id, fulfillment_location_id, inventory_reservation_id, product_title_snapshot, sku_code_snapshot, unit_price_minor, currency_code, quantity, line_total_minor)
		VALUES ($1, $2, $3, $4, 'Sample Product 2', 'SKU-002', 2000, 'EGP', 1, 2000)
	`, orderItemID, orderID, locID, resID)
	if err != nil {
		t.Fatalf("seed second order item: %v", err)
	}

	return orderID, orderItemID
}

func TestListStoreShipments_StoreQueue(t *testing.T) {
	db, service, ctx := setupShippingDB(t)

	// Seed first order and store
	orderID1, locID, orderItemID1 := seedDatabaseForShipping(t, db, ctx)

	// Find the store ID from order 1
	var storeID string
	err := db.Pool.QueryRow(ctx, "SELECT store_id FROM orders WHERE id = $1", orderID1).Scan(&storeID)
	if err != nil {
		t.Fatalf("fetch store_id: %v", err)
	}

	// Create second order for the same store
	orderID2, orderItemID2 := createSecondOrderForStore(t, db, ctx, storeID, locID)

	// Create shipment 1 for order 1 (auto-generated tracking)
	sh1, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
		OrderID:               orderID1,
		FulfillmentLocationID: locID,
		ShippingCostMinor:     500,
		Currency:              "EGP",
		Items: []shipping.CreateShipmentItemParams{
			{OrderItemID: orderItemID1, Quantity: 2},
		},
		CorrelationID: "corr-1",
		CausationID:   "user-1",
	})
	if err != nil {
		t.Fatalf("create shipment 1: %v", err)
	}

	// Verify auto-generated tracking code format
	if !strings.HasPrefix(sh1.TrackingNumber, "TRK-") {
		t.Errorf("expected auto tracking code starting with TRK-, got %s", sh1.TrackingNumber)
	}

	// Create shipment 2 for order 2 (with explicit carrier and tracking)
	sh2, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
		OrderID:               orderID2,
		FulfillmentLocationID: locID,
		CarrierName:           "Aramex",
		TrackingNumber:        "ARX-987654321",
		ShippingCostMinor:     750,
		Currency:              "EGP",
		Items: []shipping.CreateShipmentItemParams{
			{OrderItemID: orderItemID2, Quantity: 1},
		},
		CorrelationID: "corr-2",
		CausationID:   "user-1",
	})
	if err != nil {
		t.Fatalf("create shipment 2: %v", err)
	}

	if sh2.CarrierName != "Aramex" {
		t.Errorf("carrier = %s, want Aramex", sh2.CarrierName)
	}
	if sh2.TrackingNumber != "ARX-987654321" {
		t.Errorf("tracking = %s, want ARX-987654321", sh2.TrackingNumber)
	}

	// Advance sh2 status to PROCESSING
	_, err = service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
		ShipmentID:    sh2.ID,
		NewStatus:     shipping.StatusProcessing,
		Notes:         "Package ready for carrier pickup",
		CorrelationID: "corr-3",
		CausationID:   "user-1",
	})
	if err != nil {
		t.Fatalf("advance sh2 status: %v", err)
	}

	// 1. Query all shipments for store
	shipments, total, err := service.ListShipmentsForStore(ctx, storeID, nil, 1, 20)
	if err != nil {
		t.Fatalf("list store shipments: %v", err)
	}
	if total < 2 {
		t.Errorf("totalCount = %d, want >= 2", total)
	}
	if len(shipments) < 2 {
		t.Errorf("shipments len = %d, want >= 2", len(shipments))
	}

	// 2. Filter by status: PENDING
	pendingStatus := string(shipping.StatusPending)
	pendingShipments, pendingTotal, err := service.ListShipmentsForStore(ctx, storeID, &pendingStatus, 1, 20)
	if err != nil {
		t.Fatalf("list pending shipments: %v", err)
	}
	if pendingTotal < 1 {
		t.Errorf("pendingTotal = %d, want >= 1", pendingTotal)
	}
	for _, s := range pendingShipments {
		if s.Status != shipping.StatusPending {
			t.Errorf("expected status PENDING, got %s", s.Status)
		}
	}

	// 3. Filter by status: PROCESSING
	processingStatus := string(shipping.StatusProcessing)
	procShipments, procTotal, err := service.ListShipmentsForStore(ctx, storeID, &processingStatus, 1, 20)
	if err != nil {
		t.Fatalf("list processing shipments: %v", err)
	}
	if procTotal != 1 {
		t.Errorf("procTotal = %d, want 1", procTotal)
	}
	if len(procShipments) != 1 || procShipments[0].ID != sh2.ID {
		t.Errorf("expected shipment %s in processing list", sh2.ID)
	}

	// 4. Pagination: pageSize = 1
	page1, totalP1, err := service.ListShipmentsForStore(ctx, storeID, nil, 1, 1)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(page1) != 1 {
		t.Errorf("page 1 len = %d, want 1", len(page1))
	}
	if totalP1 < 2 {
		t.Errorf("total count = %d, want >= 2", totalP1)
	}

	// 5. Tenant isolation: verify non-existent store gets 0
	otherStoreShipments, otherTotal, err := service.ListShipmentsForStore(ctx, uuid.NewString(), nil, 1, 20)
	if err != nil {
		t.Fatalf("list other store shipments: %v", err)
	}
	if otherTotal != 0 || len(otherStoreShipments) != 0 {
		t.Errorf("expected 0 shipments for unknown store, got %d", otherTotal)
	}
}

func TestShipmentTrackingCodeGeneration(t *testing.T) {
	db, service, ctx := setupShippingDB(t)

	orderID, locID, orderItemID := seedDatabaseForShipping(t, db, ctx)

	t.Run("auto-generates TRK code when omitted", func(t *testing.T) {
		sh, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
			OrderID:               orderID,
			FulfillmentLocationID: locID,
			ShippingCostMinor:     100,
			Currency:              "EGP",
			Items: []shipping.CreateShipmentItemParams{
				{OrderItemID: orderItemID, Quantity: 1},
			},
		})
		if err != nil {
			t.Fatalf("create shipment: %v", err)
		}
		if !strings.HasPrefix(sh.TrackingNumber, "TRK-") {
			t.Errorf("expected tracking number to start with TRK-, got %q", sh.TrackingNumber)
		}
		expectedOrderPrefix := orderID
		if len(expectedOrderPrefix) > 8 {
			expectedOrderPrefix = expectedOrderPrefix[:8]
		}
		if !strings.Contains(sh.TrackingNumber, expectedOrderPrefix) {
			t.Errorf("expected tracking number %q to contain order prefix %q", sh.TrackingNumber, expectedOrderPrefix)
		}
	})

	t.Run("preserves explicitly provided carrier and tracking number", func(t *testing.T) {
		sh, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
			OrderID:               orderID,
			FulfillmentLocationID: locID,
			CarrierName:           "SMSA Express",
			TrackingNumber:        "SMSA-554433221",
			ShippingCostMinor:     100,
			Currency:              "EGP",
			Items: []shipping.CreateShipmentItemParams{
				{OrderItemID: orderItemID, Quantity: 1},
			},
		})
		if err != nil {
			t.Fatalf("create shipment: %v", err)
		}
		if sh.CarrierName != "SMSA Express" {
			t.Errorf("carrier = %q, want SMSA Express", sh.CarrierName)
		}
		if sh.TrackingNumber != "SMSA-554433221" {
			t.Errorf("tracking = %q, want SMSA-554433221", sh.TrackingNumber)
		}
	})
}
