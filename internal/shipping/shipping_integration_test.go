package shipping_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/shipping"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupShippingDB(t *testing.T) (*database.Pool, shipping.Service, context.Context) {
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

	repo := shipping.NewRepository(db.Pool)
	service := shipping.NewService(repo)
	return db, service, ctx
}

func seedDatabaseForShipping(t *testing.T, db *database.Pool, ctx context.Context) (string, string, string) {
	t.Helper()
	storeID := uuid.NewString()
	sellerID := uuid.NewString()
	supplierID := uuid.NewString()
	locID := uuid.NewString()
	orderID := uuid.NewString()
	sessionID := uuid.NewString()
	orderItemID := uuid.NewString()
	resID := uuid.NewString()

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status) VALUES ($1, $2, 'Test Seller', 'active')
	`, sellerID, "sel-"+sellerID[:8])
	if err != nil {
		t.Fatalf("seed seller: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status) VALUES ($1, $2, 'EG', $3, 'Test Store', 'active')
	`, storeID, sellerID, "str-"+storeID[:8])
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO suppliers (id, code, name, status) VALUES ($1, $2, 'Test Supplier', 'active')
	`, supplierID, "sup-"+supplierID[:8])
	if err != nil {
		t.Fatalf("seed supplier: %v", err)
	}

	supMarketID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO supplier_markets (id, supplier_id, market_code, status) VALUES ($1, $2, 'EG', 'active')
	`, supMarketID, supplierID)
	if err != nil {
		t.Fatalf("seed supplier market: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, supplier_id, supplier_market_id, market_code, code, name, location_type, status) VALUES ($1, $2, $3, 'EG', $4, 'Main Warehouse', 'warehouse', 'active')
	`, locID, supplierID, supMarketID, "loc-"+locID[:8])
	if err != nil {
		t.Fatalf("seed fulfillment location: %v", err)
	}

	cartID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO carts (id, store_id, market_code, cart_token_digest, status) VALUES ($1, $2, 'EG', $3, 'active')
	`, cartID, storeID, "token-"+cartID)
	if err != nil {
		t.Fatalf("seed cart: %v", err)
	}

	digest := make([]byte, 32)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'open', $4, $5)
	`, sessionID, storeID, cartID, time.Now().Add(time.Hour), digest)
	if err != nil {
		t.Fatalf("seed checkout session: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, order_number, store_id, market_code, checkout_session_id, status, currency_code, subtotal_minor, total_minor, confirmation_deadline_at, guest_order_access_token_digest)
		VALUES ($1, $2, $3, 'EG', $4, 'confirmed', 'EGP', 1000, 1000, $5, $6)
	`, orderID, "#100001", storeID, sessionID, time.Now().Add(time.Hour), digest)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')
	`, productID, "slug-"+productID[:8])
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, 'VAR-01', 'active')
	`, variantID, productID)
	if err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO skus (id, variant_id, code, status) VALUES ($1, $2, 'SKU-001', 'active')
	`, skuID, variantID)
	if err != nil {
		t.Fatalf("seed sku: %v", err)
	}

	snapshotID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty)
		VALUES ($1, $2, $3, 100, 2)
	`, snapshotID, locID, skuID)
	if err != nil {
		t.Fatalf("seed inventory snapshot: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO inventory_reservations (id, inventory_snapshot_id, quantity, status, reservation_token)
		VALUES ($1, $2, 2, 'active', $3)
	`, resID, snapshotID, "res-token-"+resID)
	if err != nil {
		t.Fatalf("seed inventory reservation: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO order_items (id, order_id, fulfillment_location_id, inventory_reservation_id, product_title_snapshot, sku_code_snapshot, unit_price_minor, currency_code, quantity, line_total_minor)
		VALUES ($1, $2, $3, $4, 'Sample Product', 'SKU-001', 500, 'EGP', 2, 1000)
	`, orderItemID, orderID, locID, resID)
	if err != nil {
		t.Fatalf("seed order item: %v", err)
	}

	return orderID, locID, orderItemID
}

func TestShipmentCreationAndOutbox(t *testing.T) {
	db, service, ctx := setupShippingDB(t)

	orderID, locID, orderItemID := seedDatabaseForShipping(t, db, ctx)

	params := shipping.CreateShipmentParams{
		OrderID:               orderID,
		FulfillmentLocationID: locID,
		TrackingNumber:        "TRK123456",
		ShippingCostMinor:     500,
		CodAmountMinor:        0,
		Currency:              "EGP",
		Items: []shipping.CreateShipmentItemParams{
			{OrderItemID: orderItemID, Quantity: 2},
		},
		CorrelationID: "corr-1",
		CausationID:   "caus-1",
	}

	sh, err := service.CreateShipment(ctx, params)
	if err != nil {
		t.Fatalf("create shipment: %v", err)
	}

	if sh.ID == "" {
		t.Error("expected non-empty shipment ID")
	}
	if sh.Status != shipping.StatusPending {
		t.Errorf("status = %s, want PENDING", sh.Status)
	}
	if len(sh.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(sh.Items))
	}
	if sh.Items[0].OrderItemID != orderItemID {
		t.Errorf("item order_item_id = %s, want %s", sh.Items[0].OrderItemID, orderItemID)
	}

	var outboxCount int
	var eventType string
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MAX(event_type), '')
		FROM outbox_events
		WHERE aggregate_type = 'shipment' AND aggregate_id = $1
	`, sh.ID).Scan(&outboxCount, &eventType)
	if err != nil {
		t.Fatalf("query outbox events: %v", err)
	}

	if outboxCount != 1 {
		t.Errorf("outbox events count = %d, want 1", outboxCount)
	}
	if eventType != "shipping.shipment.created.v1" {
		t.Errorf("outbox event_type = %s, want shipping.shipment.created.v1", eventType)
	}
}

func TestShipmentStatusTransitionLifecycle(t *testing.T) {
	db, service, ctx := setupShippingDB(t)
	orderID, locID, orderItemID := seedDatabaseForShipping(t, db, ctx)

	sh, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
		OrderID:               orderID,
		FulfillmentLocationID: locID,
		Currency:              "EGP",
		Items: []shipping.CreateShipmentItemParams{
			{OrderItemID: orderItemID, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("create shipment: %v", err)
	}

	sh, err = service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
		ShipmentID: sh.ID,
		NewStatus:  shipping.StatusProcessing,
		Notes:      "Order processing begun",
	})
	if err != nil {
		t.Fatalf("update status to PROCESSING: %v", err)
	}
	if sh.Status != shipping.StatusProcessing {
		t.Errorf("status = %s, want PROCESSING", sh.Status)
	}

	_, err = service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
		ShipmentID: sh.ID,
		NewStatus:  shipping.StatusDelivered,
	})
	if err == nil {
		t.Error("expected error for invalid transition PROCESSING -> DELIVERED, got nil")
	}

	sh, err = service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
		ShipmentID:     sh.ID,
		NewStatus:      shipping.StatusShipped,
		TrackingNumber: "TRK-SHIPPED-999",
		Notes:          "Package handed to courier",
	})
	if err != nil {
		t.Fatalf("update status to SHIPPED: %v", err)
	}
	if sh.Status != shipping.StatusShipped {
		t.Errorf("status = %s, want SHIPPED", sh.Status)
	}
	if sh.TrackingNumber != "TRK-SHIPPED-999" {
		t.Errorf("tracking_number = %s, want TRK-SHIPPED-999", sh.TrackingNumber)
	}

	var outboxCount int
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM outbox_events
		WHERE aggregate_type = 'shipment' AND aggregate_id = $1
	`, sh.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox events count: %v", err)
	}
	if outboxCount != 3 {
		t.Errorf("outbox count = %d, want 3", outboxCount)
	}
}

func TestShipmentConcurrentStatusUpdates(t *testing.T) {
	db, service, ctx := setupShippingDB(t)
	orderID, locID, orderItemID := seedDatabaseForShipping(t, db, ctx)

	sh, err := service.CreateShipment(ctx, shipping.CreateShipmentParams{
		OrderID:               orderID,
		FulfillmentLocationID: locID,
		Currency:              "EGP",
		Items: []shipping.CreateShipmentItemParams{
			{OrderItemID: orderItemID, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("create shipment: %v", err)
	}

	sh, err = service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
		ShipmentID: sh.ID,
		NewStatus:  shipping.StatusProcessing,
	})
	if err != nil {
		t.Fatalf("setup status PROCESSING: %v", err)
	}

	var wg sync.WaitGroup
	workers := 10
	errChan := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var target shipping.Status
			if idx%2 == 0 {
				target = shipping.StatusReadyForPickup
			} else {
				target = shipping.StatusShipped
			}
			_, updateErr := service.UpdateShipmentStatus(ctx, shipping.UpdateStatusParams{
				ShipmentID: sh.ID,
				NewStatus:  target,
				Notes:      "Concurrent update test",
			})
			errChan <- updateErr
		}(i)
	}

	wg.Wait()
	close(errChan)

	var successCount int
	for updateErr := range errChan {
		if updateErr == nil {
			successCount++
		}
	}

	if successCount == 0 {
		t.Error("expected at least one concurrent status update to succeed")
	}

	finalSh, err := service.GetShipment(ctx, sh.ID)
	if err != nil {
		t.Fatalf("get shipment after concurrent updates: %v", err)
	}
	if !finalSh.Status.Valid() {
		t.Errorf("final status %s is invalid", finalSh.Status)
	}
}
