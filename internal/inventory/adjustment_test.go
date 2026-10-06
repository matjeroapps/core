package inventory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/inventory"
	"core/internal/testdb"
	"core/packages/database"
)

func setupInventoryDB(t *testing.T) (*database.Pool, inventory.Service, context.Context) {
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
		"000010_customer_cart_domain",
		"000014_seller_catalog_authoring",
		"000016_catalog_invariants",
		"000025_seller_catalog_phase_b",
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

	repo := inventory.NewRepository(db.Pool)
	service := inventory.NewService(repo)
	return db, service, ctx
}

type testInventoryFixture struct {
	sellerID   string
	storeID    string
	locationID string
	productID  string
	variantID  string
	skuID      string
	listingID  string
}

func seedInventoryFixture(t *testing.T, ctx context.Context, db *database.Pool) testInventoryFixture {
	t.Helper()
	sellerID := uuid.NewString()
	storeID := uuid.NewString()
	locationID := uuid.NewString()
	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()
	listingID := uuid.NewString()

	// 1. Seller
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status)
		VALUES ($1, 'acme-seller', 'Acme Corp', 'active')
	`, sellerID)
	if err != nil {
		t.Fatalf("seed seller: %v", err)
	}

	// 2. Store
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status)
		VALUES ($1, $2, 'SA', 'acme-store', 'Acme Store', 'active')
	`, storeID, sellerID)
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	// 3. Fulfillment Location
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, name, code, location_type, status)
		VALUES ($1, $2, 'SA', 'Main Warehouse', 'WH-01', 'warehouse', 'active')
	`, locationID, storeID)
	if err != nil {
		t.Fatalf("seed fulfillment location: %v", err)
	}

	// 4. Product
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status)
		VALUES ($1, 'test-product', 'active')
	`, productID)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	// 5. Variant
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO variants (id, product_id, code, status)
		VALUES ($1, $2, 'VAR-001', 'active')
	`, variantID, productID)
	if err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	// 6. SKU
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO skus (id, variant_id, code, status)
		VALUES ($1, $2, 'SKU-001', 'active')
	`, skuID, variantID)
	if err != nil {
		t.Fatalf("seed sku: %v", err)
	}

	// 7. Seller Listing
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO seller_listings (id, store_id, product_id, market_code, status)
		VALUES ($1, $2, $3, 'SA', 'published')
	`, listingID, storeID, productID)
	if err != nil {
		t.Fatalf("seed seller listing: %v", err)
	}

	return testInventoryFixture{
		sellerID:   sellerID,
		storeID:    storeID,
		locationID: locationID,
		productID:  productID,
		variantID:  variantID,
		skuID:      skuID,
		listingID:  listingID,
	}
}

func TestInventoryAdjustment_DualMode(t *testing.T) {
	db, service, ctx := setupInventoryDB(t)
	f := seedInventoryFixture(t, ctx, db)

	// Step 1: Relative delta +15 on non-existent snapshot creates snapshot
	delta15 := int64(15)
	res1, err := service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:        f.storeID,
		LocationID:     f.locationID,
		SKUID:          f.skuID,
		QtyDelta:       &delta15,
		ReasonCode:     inventory.ReasonReceivedStock,
		Subject:        "seller-user-1",
		CorrelationID:  "corr-1",
		IdempotencyKey: "ik-step-1",
	})
	if err != nil {
		t.Fatalf("adjust +15 failed: %v", err)
	}
	if res1.OnHandQty != 15 || res1.AvailableQty != 15 || res1.QuantityDelta != 15 {
		t.Fatalf("unexpected res1 stock: on_hand=%d, avail=%d, delta=%d", res1.OnHandQty, res1.AvailableQty, res1.QuantityDelta)
	}

	// Step 2: Idempotency replay with same key returns identical result
	res1Replay, err := service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:        f.storeID,
		LocationID:     f.locationID,
		SKUID:          f.skuID,
		QtyDelta:       &delta15,
		ReasonCode:     inventory.ReasonReceivedStock,
		Subject:        "seller-user-1",
		CorrelationID:  "corr-1",
		IdempotencyKey: "ik-step-1",
	})
	if err != nil {
		t.Fatalf("idempotency replay failed: %v", err)
	}
	if res1Replay.MovementID != res1.MovementID || res1Replay.OnHandQty != 15 {
		t.Fatalf("expected identical movement replay, got %v", res1Replay)
	}

	// Step 3: Idempotency conflict with same key but different delta
	diffDelta := int64(20)
	_, err = service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:        f.storeID,
		LocationID:     f.locationID,
		SKUID:          f.skuID,
		QtyDelta:       &diffDelta,
		ReasonCode:     inventory.ReasonReceivedStock,
		Subject:        "seller-user-1",
		CorrelationID:  "corr-1",
		IdempotencyKey: "ik-step-1",
	})
	if !errors.Is(err, inventory.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}

	// Step 4: Cycle count target = 40 (on_hand goes from 15 to 40, delta = +25)
	target40 := int64(40)
	res2, err := service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:       f.storeID,
		LocationID:    f.locationID,
		SKUID:         f.skuID,
		TargetQty:     &target40,
		ReasonCode:    inventory.ReasonCycleCountReconciliation,
		Subject:       "seller-user-1",
		CorrelationID: "corr-2",
	})
	if err != nil {
		t.Fatalf("cycle count target 40 failed: %v", err)
	}
	if res2.OnHandQty != 40 || res2.QuantityDelta != 25 {
		t.Fatalf("unexpected res2 stock: on_hand=%d, delta=%d (want on_hand=40, delta=25)", res2.OnHandQty, res2.QuantityDelta)
	}

	// Step 5: Relative reduction -10 (on_hand goes from 40 to 30)
	deltaMinus10 := int64(-10)
	res3, err := service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:       f.storeID,
		LocationID:    f.locationID,
		SKUID:         f.skuID,
		QtyDelta:      &deltaMinus10,
		ReasonCode:    inventory.ReasonDamaged,
		Subject:       "seller-user-1",
		CorrelationID: "corr-3",
	})
	if err != nil {
		t.Fatalf("reduce -10 failed: %v", err)
	}
	if res3.OnHandQty != 30 || res3.QuantityDelta != -10 {
		t.Fatalf("unexpected res3 stock: on_hand=%d, delta=%d", res3.OnHandQty, res3.QuantityDelta)
	}

	// Step 6: Negative stock reduction below 0 rejected with ErrInsufficientInventory
	deltaMinus50 := int64(-50)
	_, err = service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:       f.storeID,
		LocationID:    f.locationID,
		SKUID:         f.skuID,
		QtyDelta:      &deltaMinus50,
		ReasonCode:    inventory.ReasonTheftLoss,
		Subject:       "seller-user-1",
		CorrelationID: "corr-4",
	})
	if !errors.Is(err, inventory.ErrInsufficientInventory) {
		t.Fatalf("expected ErrInsufficientInventory, got %v", err)
	}

	// Step 7: Store isolation - foreign location rejected with ErrNotFound
	foreignLocID := uuid.NewString()
	_, err = service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:       f.storeID,
		LocationID:    foreignLocID,
		SKUID:         f.skuID,
		QtyDelta:      &delta15,
		ReasonCode:    inventory.ReasonReceivedStock,
		Subject:       "seller-user-1",
		CorrelationID: "corr-5",
	})
	if !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign location, got %v", err)
	}

	// Step 8: Validation error when both or neither delta and target specified
	_, err = service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:    f.storeID,
		LocationID: f.locationID,
		SKUID:      f.skuID,
		ReasonCode: inventory.ReasonReceivedStock,
	})
	if !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput when neither specified, got %v", err)
	}

	_, err = service.AdjustStoreInventory(ctx, inventory.AdjustParams{
		StoreID:    f.storeID,
		LocationID: f.locationID,
		SKUID:      f.skuID,
		QtyDelta:   &delta15,
		TargetQty:  &target40,
		ReasonCode: inventory.ReasonReceivedStock,
	})
	if !errors.Is(err, inventory.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput when both specified, got %v", err)
	}
}
