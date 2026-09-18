package commerce

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/packages/money"
)

func (r Repository) GetSupplierByID(ctx context.Context, supplierID string) (Supplier, error) {
	if supplierID == "" {
		return Supplier{}, ErrInvalidInput
	}

	var supplier Supplier
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM suppliers
		WHERE id = $1
	`, supplierID).Scan(&supplier.ID, &supplier.Code, &supplier.Name, &supplier.Status, &supplier.CreatedAt, &supplier.UpdatedAt)
	if err != nil {
		return Supplier{}, translatePGError(err, "get supplier")
	}
	return supplier, nil
}

func (r Repository) GetSellerByID(ctx context.Context, sellerID string) (Seller, error) {
	if sellerID == "" {
		return Seller{}, ErrInvalidInput
	}

	var seller Seller
	err := r.pool.QueryRow(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM sellers
		WHERE id = $1
	`, sellerID).Scan(&seller.ID, &seller.Code, &seller.Name, &seller.Status, &seller.CreatedAt, &seller.UpdatedAt)
	if err != nil {
		return Seller{}, translatePGError(err, "get seller")
	}
	return seller, nil
}

func (r Repository) GetSupplierForSubject(ctx context.Context, subject string) (Supplier, error) {
	if subject == "" {
		return Supplier{}, ErrInvalidInput
	}

	var supplier Supplier
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.code, s.name, s.status, s.created_at, s.updated_at
		FROM suppliers s
		JOIN supplier_members sm ON sm.supplier_id = s.id
		WHERE sm.principal_subject = $1
		  AND sm.status = 'active'
		ORDER BY CASE sm.role
			WHEN 'owner' THEN 0
			WHEN 'manager' THEN 1
			ELSE 2
		END, sm.created_at ASC
		LIMIT 1
	`, subject).Scan(&supplier.ID, &supplier.Code, &supplier.Name, &supplier.Status, &supplier.CreatedAt, &supplier.UpdatedAt)
	if err != nil {
		return Supplier{}, translatePGError(err, "resolve supplier")
	}
	return supplier, nil
}

func (r Repository) GetSupplierOwnerForSubject(ctx context.Context, supplierID, subject string) (Supplier, error) {
	if supplierID == "" || subject == "" {
		return Supplier{}, ErrInvalidInput
	}

	var supplier Supplier
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.code, s.name, s.status, s.created_at, s.updated_at
		FROM suppliers s
		JOIN supplier_members sm ON sm.supplier_id = s.id
		WHERE sm.supplier_id = $1
		  AND sm.principal_subject = $2
		  AND sm.status = 'active'
		  AND sm.role = 'owner'
	`, supplierID, subject).Scan(&supplier.ID, &supplier.Code, &supplier.Name, &supplier.Status, &supplier.CreatedAt, &supplier.UpdatedAt)
	if err != nil {
		return Supplier{}, translatePGError(err, "resolve supplier owner")
	}
	return supplier, nil
}

func (r Repository) GetSellerForSubject(ctx context.Context, subject string) (Seller, error) {
	if subject == "" {
		return Seller{}, ErrInvalidInput
	}

	var seller Seller
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.code, s.name, s.status, s.created_at, s.updated_at
		FROM sellers s
		JOIN seller_members sm ON sm.seller_id = s.id
		WHERE sm.principal_subject = $1
		  AND sm.status = 'active'
		ORDER BY CASE sm.role
			WHEN 'owner' THEN 0
			WHEN 'manager' THEN 1
			ELSE 2
		END, sm.created_at ASC
		LIMIT 1
	`, subject).Scan(&seller.ID, &seller.Code, &seller.Name, &seller.Status, &seller.CreatedAt, &seller.UpdatedAt)
	if err != nil {
		return Seller{}, translatePGError(err, "resolve seller")
	}
	return seller, nil
}

func (r Repository) ListSuppliers(ctx context.Context, page Page) ([]Supplier, error) {
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM suppliers
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list suppliers: %w", err)
	}
	defer rows.Close()
	return scanSuppliers(rows)
}

func (r Repository) ListSellers(ctx context.Context, page Page) ([]Seller, error) {
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM sellers
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list sellers: %w", err)
	}
	defer rows.Close()
	return scanSellers(rows)
}

func (r Repository) ListStores(ctx context.Context, page Page) ([]Store, error) {
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, seller_id, market_code, code, name, status, created_at, updated_at
		FROM stores
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list stores: %w", err)
	}
	defer rows.Close()
	return scanStores(rows)
}

func (r Repository) ListProducts(ctx context.Context, page Page) ([]Product, error) {
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, slug, status, created_at, updated_at
		FROM products
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	return scanProducts(rows)
}

func (r Repository) ListCategories(ctx context.Context, page Page) ([]Category, error) {
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, parent_category_id, slug, status, created_at, updated_at
		FROM categories
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	return scanCategories(rows)
}

func (r Repository) ListSupplierMarkets(ctx context.Context, supplierID string, page Page) ([]SupplierMarket, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, supplier_id, market_code, status, settings, created_at, updated_at
		FROM supplier_markets
		WHERE supplier_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list supplier markets: %w", err)
	}
	defer rows.Close()
	var markets []SupplierMarket
	for rows.Next() {
		var item SupplierMarket
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.MarketCode, &item.Status, &item.Settings, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan supplier market: %w", err)
		}
		markets = append(markets, item)
	}
	return markets, rows.Err()
}

func (r Repository) ListFulfillmentLocations(ctx context.Context, supplierID string, page Page) ([]FulfillmentLocation, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(supplier_id::text, ''), COALESCE(store_id::text, ''), COALESCE(supplier_market_id::text, ''), market_code, code, name, location_type, status, created_at, updated_at
		FROM fulfillment_locations
		WHERE supplier_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment locations: %w", err)
	}
	defer rows.Close()
	var locations []FulfillmentLocation
	for rows.Next() {
		var item FulfillmentLocation
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.StoreID, &item.SupplierMarketID, &item.MarketCode, &item.Code, &item.Name, &item.LocationType, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan fulfillment location: %w", err)
		}
		locations = append(locations, item)
	}
	return locations, rows.Err()
}

func (r Repository) ListSupplierProducts(ctx context.Context, supplierID string, page Page) ([]SupplierProduct, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, supplier_id, product_id, supplier_code, status, created_at, updated_at
		FROM supplier_products
		WHERE supplier_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list supplier products: %w", err)
	}
	defer rows.Close()
	var items []SupplierProduct
	for rows.Next() {
		var item SupplierProduct
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.ProductID, &item.SupplierCode, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan supplier product: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) ListSupplierOffers(ctx context.Context, supplierID string, page Page) ([]SupplierOffer, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, supplier_id, supplier_product_id, supplier_market_id, market_code, status, created_at, updated_at
		FROM supplier_offers
		WHERE supplier_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list supplier offers: %w", err)
	}
	defer rows.Close()
	var items []SupplierOffer
	for rows.Next() {
		var item SupplierOffer
		if err := rows.Scan(&item.ID, &item.SupplierID, &item.SupplierProductID, &item.SupplierMarketID, &item.MarketCode, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan supplier offer: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) ListSellerListings(ctx context.Context, storeID string, page Page) ([]SellerListing, error) {
	if storeID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, store_id, product_id, supplier_offer_id, market_code, status, created_at, updated_at
		FROM seller_listings
		WHERE store_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, storeID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list seller listings: %w", err)
	}
	defer rows.Close()
	var items []SellerListing
	for rows.Next() {
		var item SellerListing
		if err := rows.Scan(&item.ID, &item.StoreID, &item.ProductID, &item.SupplierOfferID, &item.MarketCode, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan seller listing: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) ListInventorySnapshots(ctx context.Context, supplierID string, page Page) ([]InventorySnapshot, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.fulfillment_location_id, s.sku_id, s.on_hand_qty, s.reserved_qty, s.version, s.created_at, s.updated_at
		FROM inventory_snapshots s
		JOIN fulfillment_locations fl ON fl.id = s.fulfillment_location_id
		WHERE fl.supplier_id = $1
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list inventory snapshots: %w", err)
	}
	defer rows.Close()
	var items []InventorySnapshot
	for rows.Next() {
		var item InventorySnapshot
		if err := rows.Scan(&item.ID, &item.FulfillmentLocationID, &item.SKUID, &item.OnHandQty, &item.ReservedQty, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan inventory snapshot: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) ListInventoryMovements(ctx context.Context, snapshotID string, page Page) ([]InventoryMovement, error) {
	if snapshotID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty, reason, principal_subject, correlation_id, causation_id, created_at
		FROM inventory_movements
		WHERE inventory_snapshot_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, snapshotID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list inventory movements: %w", err)
	}
	defer rows.Close()
	var items []InventoryMovement
	for rows.Next() {
		var item InventoryMovement
		if err := rows.Scan(&item.ID, &item.InventorySnapshotID, &item.MovementType, &item.QuantityDelta, &item.OnHandQty, &item.ReservedQty, &item.Reason, &item.PrincipalSubject, &item.CorrelationID, &item.CausationID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan inventory movement: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) GetFulfillmentLocationByID(ctx context.Context, locationID string) (FulfillmentLocation, error) {
	if locationID == "" {
		return FulfillmentLocation{}, ErrInvalidInput
	}

	var location FulfillmentLocation
	err := r.pool.QueryRow(ctx, `
		SELECT id, COALESCE(supplier_id::text, ''), COALESCE(store_id::text, ''), COALESCE(supplier_market_id::text, ''), market_code, code, name, location_type, status, created_at, updated_at
		FROM fulfillment_locations
		WHERE id = $1
	`, locationID).Scan(&location.ID, &location.SupplierID, &location.StoreID, &location.SupplierMarketID, &location.MarketCode, &location.Code, &location.Name, &location.LocationType, &location.Status, &location.CreatedAt, &location.UpdatedAt)
	if err != nil {
		return FulfillmentLocation{}, translatePGError(err, "get fulfillment location")
	}
	return location, nil
}

func (r Repository) GetSupplierMarketByID(ctx context.Context, marketID string) (SupplierMarket, error) {
	if marketID == "" {
		return SupplierMarket{}, ErrInvalidInput
	}

	var market SupplierMarket
	err := r.pool.QueryRow(ctx, `
		SELECT id, supplier_id, market_code, status, settings, created_at, updated_at
		FROM supplier_markets
		WHERE id = $1
	`, marketID).Scan(&market.ID, &market.SupplierID, &market.MarketCode, &market.Status, &market.Settings, &market.CreatedAt, &market.UpdatedAt)
	if err != nil {
		return SupplierMarket{}, translatePGError(err, "get supplier market")
	}
	return market, nil
}

func (r Repository) GetSupplierProductByID(ctx context.Context, supplierProductID string) (SupplierProduct, error) {
	if supplierProductID == "" {
		return SupplierProduct{}, ErrInvalidInput
	}

	var item SupplierProduct
	err := r.pool.QueryRow(ctx, `
		SELECT id, supplier_id, product_id, supplier_code, status, created_at, updated_at
		FROM supplier_products
		WHERE id = $1
	`, supplierProductID).Scan(&item.ID, &item.SupplierID, &item.ProductID, &item.SupplierCode, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return SupplierProduct{}, translatePGError(err, "get supplier product")
	}
	return item, nil
}

func (r Repository) GetSupplierProductBySupplierAndProduct(ctx context.Context, supplierID, productID string) (SupplierProduct, error) {
	if supplierID == "" || productID == "" {
		return SupplierProduct{}, ErrInvalidInput
	}

	var item SupplierProduct
	err := r.pool.QueryRow(ctx, `
		SELECT id, supplier_id, product_id, supplier_code, status, created_at, updated_at
		FROM supplier_products
		WHERE supplier_id = $1 AND product_id = $2
	`, supplierID, productID).Scan(&item.ID, &item.SupplierID, &item.ProductID, &item.SupplierCode, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return SupplierProduct{}, translatePGError(err, "get supplier product")
	}
	return item, nil
}

func (r Repository) GetSellerListingByID(ctx context.Context, listingID string) (SellerListing, error) {
	if listingID == "" {
		return SellerListing{}, ErrInvalidInput
	}

	var item SellerListing
	err := r.pool.QueryRow(ctx, `
		SELECT id, store_id, product_id, supplier_offer_id, market_code, status, created_at, updated_at
		FROM seller_listings
		WHERE id = $1
	`, listingID).Scan(&item.ID, &item.StoreID, &item.ProductID, &item.SupplierOfferID, &item.MarketCode, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return SellerListing{}, translatePGError(err, "get seller listing")
	}
	return item, nil
}

func (r Repository) GetSupplierSettings(ctx context.Context, supplierID string) (map[string]any, error) {
	if supplierID == "" {
		return nil, ErrInvalidInput
	}
	var settings map[string]any
	err := r.pool.QueryRow(ctx, `
		SELECT settings
		FROM supplier_settings
		WHERE supplier_id = $1
	`, supplierID).Scan(&settings)
	if err != nil {
		return nil, translatePGError(err, "get supplier settings")
	}
	return normalizeSettings(settings), nil
}

func (r Repository) GetSellerSettings(ctx context.Context, sellerID string) (map[string]any, error) {
	if sellerID == "" {
		return nil, ErrInvalidInput
	}
	var settings map[string]any
	err := r.pool.QueryRow(ctx, `
		SELECT settings
		FROM seller_settings
		WHERE seller_id = $1
	`, sellerID).Scan(&settings)
	if err != nil {
		return nil, translatePGError(err, "get seller settings")
	}
	return normalizeSettings(settings), nil
}

func (r Repository) GetStoreSettings(ctx context.Context, storeID string) (map[string]any, error) {
	if storeID == "" {
		return nil, ErrInvalidInput
	}
	var settings map[string]any
	err := r.pool.QueryRow(ctx, `
		SELECT settings
		FROM store_settings
		WHERE store_id = $1
	`, storeID).Scan(&settings)
	if err != nil {
		return nil, translatePGError(err, "get store settings")
	}
	return normalizeSettings(settings), nil
}

func (r Repository) GetProductCategoryIDs(ctx context.Context, productID string) ([]string, error) {
	return r.ListProductCategoryIDs(ctx, productID)
}

func (r Repository) SetProductCategories(ctx context.Context, productID string, categoryIDs []string) error {
	if productID == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := setProductCategoriesTx(ctx, tx, productID, categoryIDs); err != nil {
			return err
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, productID)
	})
}

func (r Repository) ListProductCategoryIDs(ctx context.Context, productID string) ([]string, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT category_id
		FROM product_categories
		WHERE product_id = $1
		ORDER BY sort_order ASC, category_id ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("list product categories: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan product category id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r Repository) AdjustInventory(ctx context.Context, snapshotID string, quantityDelta int64, movementType, reason, principalSubject, correlationID, causationID string, idempotencyKey, requestFingerprint string) (InventorySnapshot, InventoryMovement, error) {
	if snapshotID == "" || movementType == "" {
		return InventorySnapshot{}, InventoryMovement{}, ErrInvalidInput
	}
	if strings.TrimSpace(movementType) == "" {
		return InventorySnapshot{}, InventoryMovement{}, ErrInvalidInput
	}

	var updated InventorySnapshot
	var movement InventoryMovement
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if idempotencyKey != "" {
			var existingm InventoryMovement
			var keyVal, fpVal sql.NullString
			err := tx.QueryRow(ctx, `
				SELECT id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty,
				       reason, principal_subject, correlation_id, causation_id, idempotency_key, request_fingerprint, created_at
				FROM inventory_movements
				WHERE inventory_snapshot_id = $1 AND idempotency_key = $2
			`, snapshotID, idempotencyKey).Scan(
				&existingm.ID, &existingm.InventorySnapshotID, &existingm.MovementType, &existingm.QuantityDelta,
				&existingm.OnHandQty, &existingm.ReservedQty, &existingm.Reason, &existingm.PrincipalSubject,
				&existingm.CorrelationID, &existingm.CausationID, &keyVal, &fpVal, &existingm.CreatedAt,
			)
			if err == nil {
				if keyVal.Valid {
					existingm.IdempotencyKey = keyVal.String
				}
				if fpVal.Valid {
					existingm.RequestFingerprint = fpVal.String
				}
				if existingm.RequestFingerprint != requestFingerprint {
					return ErrIdempotencyConflict
				}
				errSnap := tx.QueryRow(ctx, `
					SELECT id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
					FROM inventory_snapshots
					WHERE id = $1
				`, snapshotID).Scan(&updated.ID, &updated.FulfillmentLocationID, &updated.SKUID, &updated.OnHandQty, &updated.ReservedQty, &updated.Version, &updated.CreatedAt, &updated.UpdatedAt)
				if errSnap != nil {
					return translatePGError(errSnap, "get current snapshot for idempotency replay")
				}
				movement = existingm
				return nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return translatePGError(err, "check inventory movement idempotency")
			}
		}

		if quantityDelta == 0 {
			return ErrInvalidInput
		}

		if quantityDelta > 0 {
			if err := tx.QueryRow(ctx, `
				UPDATE inventory_snapshots
				SET on_hand_qty = on_hand_qty + $2,
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
				RETURNING id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
			`, snapshotID, quantityDelta).Scan(&updated.ID, &updated.FulfillmentLocationID, &updated.SKUID, &updated.OnHandQty, &updated.ReservedQty, &updated.Version, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrConflict
				}
				return translatePGError(err, "adjust inventory")
			}
		} else {
			if err := tx.QueryRow(ctx, `
				UPDATE inventory_snapshots
				SET on_hand_qty = on_hand_qty + $2,
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
				  AND (on_hand_qty + $2) >= reserved_qty
				  AND (on_hand_qty + $2) >= 0
				RETURNING id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
			`, snapshotID, quantityDelta).Scan(&updated.ID, &updated.FulfillmentLocationID, &updated.SKUID, &updated.OnHandQty, &updated.ReservedQty, &updated.Version, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrInsufficientInventory
				}
				return translatePGError(err, "adjust inventory")
			}
		}

		if updated.ID == "" {
			return ErrInvalidInput
		}

		var ikParam, fpParam *string
		if idempotencyKey != "" {
			ikParam = &idempotencyKey
		}
		if requestFingerprint != "" {
			fpParam = &requestFingerprint
		}

		movementID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO inventory_movements (
				id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty,
				reason, principal_subject, correlation_id, causation_id, idempotency_key, request_fingerprint
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			RETURNING created_at
		`, movementID, snapshotID, movementType, quantityDelta, updated.OnHandQty, updated.ReservedQty, reason, principalSubject, correlationID, causationID, ikParam, fpParam).Scan(&movement.CreatedAt); err != nil {
			return translatePGError(err, "record inventory movement")
		}
		movement = InventoryMovement{
			ID:                  movementID,
			InventorySnapshotID: snapshotID,
			MovementType:        movementType,
			QuantityDelta:       quantityDelta,
			OnHandQty:           updated.OnHandQty,
			ReservedQty:         updated.ReservedQty,
			Reason:              reason,
			PrincipalSubject:    principalSubject,
			CorrelationID:       correlationID,
			CausationID:         causationID,
			IdempotencyKey:      idempotencyKey,
			RequestFingerprint:  requestFingerprint,
			CreatedAt:           movement.CreatedAt,
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByInventorySnapshot, snapshotID)
	})
	return updated, movement, err
}

func (r Repository) ListSupplierCatalog(ctx context.Context, filter SupplierCatalogFilter) ([]SupplierCatalogItem, error) {
	page := normalizePage(filter.Page)
	locale := normalizeLocale(filter.Locale)
	args := []any{}
	where := []string{"1=1"}

	if filter.MarketCode != "" {
		args = append(args, filter.MarketCode)
		where = append(where, fmt.Sprintf("so.market_code = $%d", len(args)))
	}
	if filter.SupplierID != "" {
		args = append(args, filter.SupplierID)
		where = append(where, fmt.Sprintf("so.supplier_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("so.status = $%d", len(args)))
	}
	if filter.CategoryID != "" {
		args = append(args, filter.CategoryID)
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM product_categories pc_filter WHERE pc_filter.product_id = p.id AND pc_filter.category_id = $%d)", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
		where = append(where, fmt.Sprintf("(LOWER(p.slug) LIKE $%d OR LOWER(sp.supplier_code) LIKE $%d OR LOWER(sup.code) LIKE $%d)", len(args), len(args), len(args)))
	}
	if filter.Available != nil {
		args = append(args, *filter.Available)
		where = append(where, fmt.Sprintf("COALESCE(av.is_available, false) = $%d", len(args)))
	}
	if filter.MinPrice != nil {
		args = append(args, filter.MinPrice.AmountMinor, filter.MinPrice.Currency)
		where = append(where, fmt.Sprintf("(price.amount_minor >= $%d AND price.currency_code = $%d)", len(args)-1, len(args)))
	}
	if filter.MaxPrice != nil {
		args = append(args, filter.MaxPrice.AmountMinor, filter.MaxPrice.Currency)
		where = append(where, fmt.Sprintf("(price.amount_minor <= $%d AND price.currency_code = $%d)", len(args)-1, len(args)))
	}

	localeIndex := len(args) + 1
	args = append(args, locale, locale)
	limitIndex := len(args) + 1
	offsetIndex := len(args) + 2
	args = append(args, page.Limit, page.Offset)
	query := fmt.Sprintf(`
		SELECT
			so.id, so.status, so.market_code,
			p.id, p.slug, p.status,
			COALESCE(pt.name, p.slug),
			sup.id, sup.code, sup.name,
			COALESCE(cat.category_id::text, ''),
			COALESCE(cat.name, cat.slug, ''),
			price.amount_minor, price.currency_code,
			av.is_available, av.available_qty,
			so.updated_at
		FROM supplier_offers so
		JOIN supplier_products sp ON sp.id = so.supplier_product_id
		JOIN products p ON p.id = sp.product_id
		JOIN suppliers sup ON sup.id = so.supplier_id
		LEFT JOIN LATERAL (
			SELECT name
			FROM product_translations pt2
			WHERE pt2.product_id = p.id AND pt2.locale = $%d
			LIMIT 1
		) pt ON true
		LEFT JOIN LATERAL (
			SELECT pc.category_id, c.slug, ct.name
			FROM product_categories pc
			JOIN categories c ON c.id = pc.category_id
			LEFT JOIN LATERAL (
				SELECT name
				FROM category_translations ct2
				WHERE ct2.category_id = c.id AND ct2.locale = $%d
				LIMIT 1
			) ct ON true
			WHERE pc.product_id = p.id
			ORDER BY pc.sort_order ASC, pc.category_id ASC
			LIMIT 1
		) cat ON true
		LEFT JOIN LATERAL (
			SELECT amount_minor, currency_code
			FROM supplier_offer_prices
			WHERE supplier_offer_id = so.id AND is_current = true
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) price ON true
		LEFT JOIN LATERAL (
			SELECT is_available, available_qty
			FROM supplier_offer_availability
			WHERE supplier_offer_id = so.id
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) av ON true
		WHERE %s
		ORDER BY so.created_at DESC, so.id DESC
		LIMIT $%d OFFSET $%d
	`, localeIndex, localeIndex+1, strings.Join(where, " AND "), limitIndex, offsetIndex)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list supplier catalog: %w", err)
	}
	defer rows.Close()

	var items []SupplierCatalogItem
	for rows.Next() {
		var item SupplierCatalogItem
		var amountMinor *int64
		var currencyCode *string
		if err := rows.Scan(
			&item.OfferID, &item.OfferStatus, &item.MarketCode,
			&item.ProductID, &item.ProductSlug, &item.ProductStatus,
			&item.ProductName,
			&item.SupplierID, &item.SupplierCode, &item.SupplierName,
			&item.CategoryID,
			&item.CategoryName,
			&amountMinor, &currencyCode,
			&item.IsAvailable, &item.AvailableQty,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan supplier catalog item: %w", err)
		}
		if amountMinor != nil && currencyCode != nil {
			price := money.Money{AmountMinor: *amountMinor, Currency: *currencyCode}
			item.Price = &price
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) UpdateSupplierStatus(ctx context.Context, supplierID, status string) error {
	return updateStatus(ctx, r.pool, "suppliers", supplierID, status)
}

func (r Repository) UpdateSupplierProfile(ctx context.Context, supplierID, name, status string, settings map[string]any) error {
	if supplierID == "" || name == "" || status == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE suppliers
			SET name = $2, status = $3, updated_at = now()
			WHERE id = $1
		`, supplierID, name, status); err != nil {
			return translatePGError(err, "update supplier")
		}
		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO supplier_settings (supplier_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (supplier_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, supplierID, settings); err != nil {
			return err
		}
		return nil
	})
}

func (r Repository) UpdateSellerStatus(ctx context.Context, sellerID, status string) error {
	return updateStatus(ctx, r.pool, "sellers", sellerID, status)
}

func (r Repository) UpdateSellerProfile(ctx context.Context, sellerID, name, status string, settings map[string]any) error {
	if sellerID == "" || name == "" || status == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE sellers
			SET name = $2, status = $3, updated_at = now()
			WHERE id = $1
		`, sellerID, name, status); err != nil {
			return translatePGError(err, "update seller")
		}
		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO seller_settings (seller_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (seller_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, sellerID, settings); err != nil {
			return err
		}
		return nil
	})
}

func (r Repository) UpdateStoreStatus(ctx context.Context, storeID, targetStatus string, maxActiveStores ...int) error {
	if storeID == "" || targetStatus == "" {
		return ErrInvalidInput
	}
	targetStatus = strings.ToLower(strings.TrimSpace(targetStatus))
	if targetStatus != "draft" && targetStatus != "active" && targetStatus != "inactive" {
		return fmt.Errorf("%w: invalid store status %q", ErrInvalidInput, targetStatus)
	}

	maxActive := 1
	if len(maxActiveStores) > 0 && maxActiveStores[0] > 0 {
		maxActive = maxActiveStores[0]
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var currentStatus, sellerID string
		if err := tx.QueryRow(ctx, `
			SELECT status, seller_id
			FROM stores
			WHERE id = $1
			FOR UPDATE
		`, storeID).Scan(&currentStatus, &sellerID); err != nil {
			return translatePGError(err, "get store for status update")
		}

		if currentStatus == targetStatus {
			return nil
		}

		if targetStatus == "draft" {
			return fmt.Errorf("%w: store status cannot transition back to draft from %s", ErrInvalidTransition, currentStatus)
		}

		if targetStatus == "active" {
			var sellerDummy string
			if err := tx.QueryRow(ctx, `SELECT id FROM sellers WHERE id = $1 FOR UPDATE`, sellerID).Scan(&sellerDummy); err != nil {
				return translatePGError(err, "lock seller for store activation")
			}

			var activeCount int
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM stores
				WHERE seller_id = $1
				  AND status = 'active'
				  AND id != $2
			`, sellerID, storeID).Scan(&activeCount); err != nil {
				return translatePGError(err, "count active stores")
			}

			if activeCount >= maxActive {
				return ErrStoreEntitlementExceeded
			}
		}

		if _, err := tx.Exec(ctx, `
			UPDATE stores
			SET status = $2, updated_at = now()
			WHERE id = $1
		`, storeID, targetStatus); err != nil {
			return translatePGError(err, "update store status")
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
}

func (r Repository) UpdateStoreProfile(ctx context.Context, storeID, name, status string, settings map[string]any) error {
	if storeID == "" || name == "" || status == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE stores
			SET name = $2, status = $3, updated_at = now()
			WHERE id = $1
		`, storeID, name, status); err != nil {
			return translatePGError(err, "update store")
		}
		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO store_settings (store_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (store_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, storeID, settings); err != nil {
			return err
		}
		// The store name and its public settings are both part of the storefront
		// bootstrap payload.
		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
}

func (r Repository) UpdateProductStatus(ctx context.Context, productID, status string) error {
	return r.updateStorefrontStatus(ctx, "products", revisionStoresByProduct, productID, status)
}

func (r Repository) UpdateProduct(ctx context.Context, productID, slug, status string) error {
	if productID == "" || slug == "" || status == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE products
			SET slug = $2, status = $3, updated_at = now()
			WHERE id = $1
		`, productID, slug, status); err != nil {
			return translatePGError(err, "update product")
		}
		// The slug is the public product URL, so a rename changes every public
		// path that resolves to it.
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, productID)
	})
}

func (r Repository) UpdateCategoryStatus(ctx context.Context, categoryID, status string) error {
	return r.updateStorefrontStatus(ctx, "categories", revisionStoresByCategorySubtree, categoryID, status)
}

func (r Repository) UpdateCategory(ctx context.Context, categoryID, slug, status string, parentCategoryID *string) error {
	if categoryID == "" || slug == "" || status == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE categories
			SET slug = $2, parent_category_id = $3, status = $4, updated_at = now()
			WHERE id = $1
		`, categoryID, slug, parentCategoryID, status); err != nil {
			return translatePGError(err, "update category")
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByCategorySubtree, categoryID)
	})
}

func (r Repository) UpdateSupplierOfferStatus(ctx context.Context, offerID, status string) error {
	return r.updateStorefrontStatus(ctx, "supplier_offers", revisionStoresBySupplierOffer, offerID, status)
}

func (r Repository) UpdateSupplierMarketStatus(ctx context.Context, supplierMarketID, status string) error {
	return updateStatus(ctx, r.pool, "supplier_markets", supplierMarketID, status)
}

func (r Repository) UpdateSellerListingStatus(ctx context.Context, listingID, status string) error {
	return r.updateStorefrontStatus(ctx, "seller_listings", revisionStoresByListing, listingID, status)
}

func (r Repository) UpdateFulfillmentLocationStatus(ctx context.Context, locationID, status string) error {
	return r.updateStorefrontStatus(ctx, "fulfillment_locations", revisionStoresByFulfillmentLocation, locationID, status)
}

func updateStatus(ctx context.Context, pool *pgxpool.Pool, table, id, status string) error {
	if id == "" || status == "" {
		return ErrInvalidInput
	}
	_, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = $2, updated_at = now() WHERE id = $1`, table), id, status)
	return translatePGError(err, "update status")
}

func normalizePage(page Page) Page {
	if page.Limit <= 0 || page.Limit > 100 {
		page.Limit = 25
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page
}

func normalizeLocale(value string) string {
	if strings.EqualFold(value, "ar") {
		return "ar"
	}
	return "en"
}

func scanSuppliers(rows pgx.Rows) ([]Supplier, error) {
	var items []Supplier
	for rows.Next() {
		var item Supplier
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan supplier: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSellers(rows pgx.Rows) ([]Seller, error) {
	var items []Seller
	for rows.Next() {
		var item Seller
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan seller: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanStores(rows pgx.Rows) ([]Store, error) {
	var items []Store
	for rows.Next() {
		var item Store
		if err := rows.Scan(&item.ID, &item.SellerID, &item.MarketCode, &item.Code, &item.Name, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan store: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanProducts(rows pgx.Rows) ([]Product, error) {
	var items []Product
	for rows.Next() {
		var item Product
		if err := rows.Scan(&item.ID, &item.Slug, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCategories(rows pgx.Rows) ([]Category, error) {
	var items []Category
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.ParentCategoryID, &item.Slug, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// StoreDomainResolution is the joined store + domain record resolved from a
// trusted domain-to-store mapping. It is the source of tenant identity for the
// public storefront.
type StoreDomainResolution struct {
	Store       Store
	StoreDomain StoreDomain
}

// GetStoreByDomain resolves a normalized domain to its owning store and domain
// record. Returns ErrNotFound when no active mapping exists.
func (r Repository) GetStoreByDomain(ctx context.Context, domain string) (StoreDomainResolution, error) {
	if domain == "" {
		return StoreDomainResolution{}, ErrInvalidInput
	}

	var res StoreDomainResolution
	err := r.pool.QueryRow(ctx, `
		SELECT
			s.id, s.seller_id, s.market_code, s.code, s.name, s.status, s.created_at, s.updated_at,
			d.id, d.store_id, d.domain, d.is_primary, d.verified_at, d.status, d.domain_type, d.verification_token, d.last_checked_at, d.created_at, d.updated_at
		FROM store_domains d
		JOIN stores s ON s.id = d.store_id
		WHERE d.domain = $1
	`, domain).Scan(
		&res.Store.ID, &res.Store.SellerID, &res.Store.MarketCode, &res.Store.Code, &res.Store.Name, &res.Store.Status, &res.Store.CreatedAt, &res.Store.UpdatedAt,
		&res.StoreDomain.ID, &res.StoreDomain.StoreID, &res.StoreDomain.Domain, &res.StoreDomain.IsPrimary, &res.StoreDomain.VerifiedAt, &res.StoreDomain.Status, &res.StoreDomain.DomainType, &res.StoreDomain.VerificationToken, &res.StoreDomain.LastCheckedAt, &res.StoreDomain.CreatedAt, &res.StoreDomain.UpdatedAt,
	)
	if err != nil {
		return StoreDomainResolution{}, translatePGError(err, "get store by domain")
	}
	return res, nil
}

// GetActivePrimaryStoreDomain returns the authoritative primary active domain for
// a store. Returns ErrNotFound when no active primary domain exists.
func (r Repository) GetActivePrimaryStoreDomain(ctx context.Context, storeID string) (StoreDomain, error) {
	if storeID == "" {
		return StoreDomain{}, ErrInvalidInput
	}

	var d StoreDomain
	err := r.pool.QueryRow(ctx, `
		SELECT id, store_id, domain, is_primary, verified_at, status, domain_type, verification_token, last_checked_at, created_at, updated_at
		FROM store_domains
		WHERE store_id = $1
		  AND is_primary = true
		  AND status = 'active'
	`, storeID).Scan(
		&d.ID, &d.StoreID, &d.Domain, &d.IsPrimary, &d.VerifiedAt, &d.Status, &d.DomainType, &d.VerificationToken, &d.LastCheckedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return StoreDomain{}, translatePGError(err, "get active primary store domain")
	}
	return d, nil
}

func (r Repository) CreateSupplierSellerAffiliation(ctx context.Context, supplierID, sellerID string) (SupplierSellerAffiliation, error) {
	if supplierID == "" || sellerID == "" {
		return SupplierSellerAffiliation{}, ErrInvalidInput
	}
	var affiliation SupplierSellerAffiliation
	err := r.pool.QueryRow(ctx, `
		INSERT INTO supplier_seller_affiliations (supplier_id, seller_id)
		VALUES ($1, $2)
		RETURNING supplier_id, seller_id, created_at
	`, supplierID, sellerID).Scan(&affiliation.SupplierID, &affiliation.SellerID, &affiliation.CreatedAt)
	if err != nil {
		return SupplierSellerAffiliation{}, translatePGError(err, "create supplier seller affiliation")
	}
	return affiliation, nil
}

func (r Repository) GetSupplierSellerAffiliationBySupplierID(ctx context.Context, supplierID string) (SupplierSellerAffiliation, error) {
	if supplierID == "" {
		return SupplierSellerAffiliation{}, ErrInvalidInput
	}
	var affiliation SupplierSellerAffiliation
	err := r.pool.QueryRow(ctx, `
		SELECT supplier_id, seller_id, created_at
		FROM supplier_seller_affiliations
		WHERE supplier_id = $1
	`, supplierID).Scan(&affiliation.SupplierID, &affiliation.SellerID, &affiliation.CreatedAt)
	if err != nil {
		return SupplierSellerAffiliation{}, translatePGError(err, "get supplier seller affiliation by supplier id")
	}
	return affiliation, nil
}

func (r Repository) GetSupplierSellerAffiliationBySellerID(ctx context.Context, sellerID string) (SupplierSellerAffiliation, error) {
	if sellerID == "" {
		return SupplierSellerAffiliation{}, ErrInvalidInput
	}
	var affiliation SupplierSellerAffiliation
	err := r.pool.QueryRow(ctx, `
		SELECT supplier_id, seller_id, created_at
		FROM supplier_seller_affiliations
		WHERE seller_id = $1
	`, sellerID).Scan(&affiliation.SupplierID, &affiliation.SellerID, &affiliation.CreatedAt)
	if err != nil {
		return SupplierSellerAffiliation{}, translatePGError(err, "get supplier seller affiliation by seller id")
	}
	return affiliation, nil
}

func (r Repository) ListStoresBySellerID(ctx context.Context, sellerID string, page Page) ([]Store, error) {
	if sellerID == "" {
		return nil, ErrInvalidInput
	}
	page = normalizePage(page)
	rows, err := r.pool.Query(ctx, `
		SELECT id, seller_id, market_code, code, name, status, created_at, updated_at
		FROM stores
		WHERE seller_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, sellerID, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list stores by seller id: %w", err)
	}
	defer rows.Close()
	return scanStores(rows)
}

func (r Repository) GetSellerMember(ctx context.Context, sellerID, subject string) (SellerMember, error) {
	if sellerID == "" || subject == "" {
		return SellerMember{}, ErrInvalidInput
	}

	var member SellerMember
	err := r.pool.QueryRow(ctx, `
		SELECT sm.id, sm.seller_id, sm.principal_subject, sm.role, sm.status, sm.created_at, sm.updated_at
		FROM seller_members sm
		WHERE sm.seller_id = $1
		  AND sm.principal_subject = $2
		  AND sm.status = 'active'
		ORDER BY CASE sm.role
			WHEN 'owner' THEN 0
			WHEN 'seller_owner' THEN 0
			WHEN 'manager' THEN 1
			WHEN 'seller_manager' THEN 1
			ELSE 2
		END, sm.created_at ASC
		LIMIT 1
	`, sellerID, subject).Scan(&member.ID, &member.SellerID, &member.PrincipalSubject, &member.Role, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	if err != nil {
		return SellerMember{}, translatePGError(err, "resolve seller member")
	}
	return member, nil
}

func (r Repository) CountActiveStoresBySellerID(ctx context.Context, sellerID string) (int, error) {
	if sellerID == "" {
		return 0, ErrInvalidInput
	}
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM stores
		WHERE seller_id = $1
		  AND status = 'active'
	`, sellerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active stores by seller id: %w", err)
	}
	return count, nil
}

func (r Repository) CreateSupplierRetailCapabilityForSubject(ctx context.Context, subject, supplierID string, draft RetailCapabilityDraft) (Seller, SupplierSellerAffiliation, error) {
	if subject == "" || supplierID == "" || draft.Code == "" || draft.Name == "" {
		return Seller{}, SupplierSellerAffiliation{}, ErrInvalidInput
	}

	var createdSeller Seller
	var createdAffiliation SupplierSellerAffiliation

	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var dummy int
		if err := tx.QueryRow(ctx, `
			SELECT 1
			FROM suppliers s
			JOIN supplier_members sm ON sm.supplier_id = s.id
			WHERE s.id = $1 AND sm.principal_subject = $2 AND sm.status = 'active' AND sm.role = 'owner'
		`, supplierID, subject).Scan(&dummy); err != nil {
			return translatePGError(err, "verify supplier owner for retail capability")
		}

		sellerID := uuid.NewString()
		status := "active"
		if err := tx.QueryRow(ctx, `
			INSERT INTO sellers (id, code, name, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, sellerID, draft.Code, draft.Name, status).Scan(&createdSeller.CreatedAt, &createdSeller.UpdatedAt); err != nil {
			return translatePGError(err, "create seller for retail capability")
		}
		createdSeller.ID = sellerID
		createdSeller.Code = draft.Code
		createdSeller.Name = draft.Name
		createdSeller.Status = status

		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO seller_settings (seller_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (seller_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, sellerID, draft.Settings); err != nil {
			return err
		}

		memberID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO seller_members (id, seller_id, principal_subject, role, status)
			VALUES ($1, $2, $3, 'owner', 'active')
		`, memberID, sellerID, subject); err != nil {
			return translatePGError(err, "create seller member for retail capability")
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_seller_affiliations (supplier_id, seller_id)
			VALUES ($1, $2)
			RETURNING created_at
		`, supplierID, sellerID).Scan(&createdAffiliation.CreatedAt); err != nil {
			return translatePGError(err, "create supplier seller affiliation")
		}
		createdAffiliation.SupplierID = supplierID
		createdAffiliation.SellerID = sellerID

		return nil
	})

	return createdSeller, createdAffiliation, err
}
