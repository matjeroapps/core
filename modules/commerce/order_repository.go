package commerce

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/matjeroapps/core/packages/outbox"
)

type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r Repository) getExec(exec DBExecutor) DBExecutor {
	if exec != nil {
		return exec
	}
	return r.pool
}

// AllocateOrderNumber atomically allocates the next sequential order number for a Store
// using store_order_sequences. The returned order number is formatted as #100001.
func (r Repository) AllocateOrderNumber(ctx context.Context, exec DBExecutor, storeID string) (string, error) {
	if strings.TrimSpace(storeID) == "" {
		return "", ErrInvalidInput
	}
	db := r.getExec(exec)
	var nextVal int64
	err := db.QueryRow(ctx, `
		INSERT INTO store_order_sequences (store_id, next_value)
		VALUES ($1, 100002)
		ON CONFLICT (store_id) DO UPDATE
		SET next_value = store_order_sequences.next_value + 1
		RETURNING next_value - 1
	`, storeID).Scan(&nextVal)
	if err != nil {
		return "", translatePGError(err, "allocate order number")
	}
	return fmt.Sprintf("#%d", nextVal), nil
}

// CreateOrder persists an Order, its items, and its address within the provided DBExecutor context.
func (r Repository) CreateOrder(ctx context.Context, exec DBExecutor, order Order) (Order, error) {
	if exec == nil {
		var created Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			created, err = r.createOrderExec(ctx, tx, order)
			return err
		})
		return created, err
	}
	return r.createOrderExec(ctx, exec, order)
}

func (r Repository) createOrderExec(ctx context.Context, db DBExecutor, order Order) (Order, error) {
	if strings.TrimSpace(order.ID) == "" ||
		strings.TrimSpace(order.OrderNumber) == "" ||
		strings.TrimSpace(order.StoreID) == "" ||
		strings.TrimSpace(order.MarketCode) == "" ||
		strings.TrimSpace(order.CheckoutSessionID) == "" ||
		strings.TrimSpace(order.Status) == "" ||
		strings.TrimSpace(order.CurrencyCode) == "" ||
		order.SubtotalMinor < 0 ||
		order.TotalMinor < 0 ||
		order.ConfirmationDeadlineAt.IsZero() {
		return Order{}, ErrInvalidInput
	}

	// Enforce guest/customer invariant
	if order.CustomerID == nil && len(order.GuestOrderAccessTokenDigest) == 0 {
		return Order{}, ErrInvalidInput
	}
	if order.CustomerID == nil && len(order.GuestOrderAccessTokenDigest) != sha256.Size {
		return Order{}, ErrInvalidInput
	}

	if order.AggregateVersion == 0 {
		order.AggregateVersion = 1
	}
	now := time.Now().UTC()
	if order.CreatedAt.IsZero() {
		order.CreatedAt = now
	}
	if order.UpdatedAt.IsZero() {
		order.UpdatedAt = now
	}

	_, err := db.Exec(ctx, `
		INSERT INTO orders (
			id, order_number, store_id, market_code, customer_id, checkout_session_id,
			status, currency_code, guest_order_access_token_digest, subtotal_minor,
			total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, order.ID, order.OrderNumber, order.StoreID, order.MarketCode, order.CustomerID,
		order.CheckoutSessionID, order.Status, order.CurrencyCode, order.GuestOrderAccessTokenDigest,
		order.SubtotalMinor, order.TotalMinor, order.ConfirmationDeadlineAt,
		order.CancellationReason, order.AggregateVersion, order.CreatedAt, order.UpdatedAt)
	if err != nil {
		return Order{}, translatePGError(err, "insert order")
	}

	for i, item := range order.Items {
		if strings.TrimSpace(item.ID) == "" {
			item.ID = uuid.NewString()
		}
		item.OrderID = order.ID
		if item.Quantity <= 0 || item.UnitPriceMinor < 0 || item.LineTotalMinor < 0 || strings.TrimSpace(item.FulfillmentLocationID) == "" || strings.TrimSpace(item.InventoryReservationID) == "" || strings.TrimSpace(item.ProductTitleSnapshot) == "" || strings.TrimSpace(item.SKUCodeSnapshot) == "" {
			return Order{}, ErrInvalidInput
		}
		if item.CurrencyCode == "" {
			item.CurrencyCode = order.CurrencyCode
		}

		// Supplier Commercial Snapshot Invariant check
		hasSupplierOffer := item.SupplierOfferID != nil
		hasSourceSupplier := item.SourceSupplierID != nil
		hasSupplierCost := item.SupplierCostMinor != nil
		hasSupplierCostCurr := item.SupplierCostCurrencyCode != nil

		isSupplierBacked := hasSupplierOffer && hasSourceSupplier && hasSupplierCost && hasSupplierCostCurr
		isSellerOwned := !hasSupplierOffer && !hasSourceSupplier && !hasSupplierCost && !hasSupplierCostCurr

		if !isSupplierBacked && !isSellerOwned {
			return Order{}, ErrInvalidInput
		}

		if isSupplierBacked {
			if *item.SupplierCostMinor < 0 || *item.SupplierCostCurrencyCode != item.CurrencyCode {
				return Order{}, ErrInvalidInput
			}
		}

		if item.CreatedAt.IsZero() {
			item.CreatedAt = order.CreatedAt
		}

		_, err := db.Exec(ctx, `
			INSERT INTO order_items (
				id, order_id, seller_listing_id, product_id, variant_id, sku_id,
				supplier_offer_id, source_supplier_id, fulfillment_location_id,
				inventory_reservation_id, product_title_snapshot, sku_code_snapshot,
				unit_price_minor, currency_code, quantity, line_total_minor,
				supplier_cost_minor, supplier_cost_currency_code, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		`, item.ID, item.OrderID, item.SellerListingID, item.ProductID, item.VariantID, item.SKUID,
			item.SupplierOfferID, item.SourceSupplierID, item.FulfillmentLocationID,
			item.InventoryReservationID, item.ProductTitleSnapshot, item.SKUCodeSnapshot,
			item.UnitPriceMinor, item.CurrencyCode, item.Quantity, item.LineTotalMinor,
			item.SupplierCostMinor, item.SupplierCostCurrencyCode, item.CreatedAt)
		if err != nil {
			return Order{}, translatePGError(err, "insert order item")
		}
		order.Items[i] = item
	}

	if order.Address != nil {
		addr := order.Address
		if strings.TrimSpace(addr.ID) == "" {
			addr.ID = uuid.NewString()
		}
		addr.OrderID = order.ID
		if addr.AddressType == "" {
			addr.AddressType = AddressTypeShipping
		}
		if strings.TrimSpace(addr.RecipientName) == "" ||
			strings.TrimSpace(addr.AddressLine1) == "" ||
			strings.TrimSpace(addr.City) == "" ||
			len(strings.TrimSpace(addr.CountryCode)) != 2 {
			return Order{}, ErrInvalidInput
		}
		if addr.CreatedAt.IsZero() {
			addr.CreatedAt = order.CreatedAt
		}

		_, err := db.Exec(ctx, `
			INSERT INTO order_addresses (
				id, order_id, address_type, recipient_name, phone, address_line_1,
				address_line_2, city, region, postal_code, country_code, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, addr.ID, addr.OrderID, addr.AddressType, addr.RecipientName, addr.Phone,
			addr.AddressLine1, addr.AddressLine2, addr.City, addr.Region,
			addr.PostalCode, addr.CountryCode, addr.CreatedAt)
		if err != nil {
			return Order{}, translatePGError(err, "insert order address")
		}
		order.Address = addr
	}

	return order, nil
}

// GetOrderByID loads an order by ID within a Store tenant boundary.
func (r Repository) GetOrderByID(ctx context.Context, exec DBExecutor, storeID, orderID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" {
		return Order{}, ErrInvalidInput
	}
	db := r.getExec(exec)

	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND store_id = $2
	`, orderID, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "get order by id")
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items

	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err != nil && err != ErrNotFound {
		return Order{}, err
	}
	if err == nil {
		order.Address = &addr
	}

	return order, nil
}

// GetGuestOrder loads an order by ID for guest access after validating store scoping and guest capability.
func (r Repository) GetGuestOrder(ctx context.Context, exec DBExecutor, storeID, orderID, rawCapability string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" || strings.TrimSpace(rawCapability) == "" {
		return Order{}, ErrUnauthorized
	}
	db := r.getExec(exec)

	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1
	`, orderID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "get guest order")
	}
	if order.StoreID != storeID {
		return Order{}, ErrNotFound
	}

	rawDigest := sha256.Sum256([]byte(rawCapability))
	if len(order.GuestOrderAccessTokenDigest) != sha256.Size || subtle.ConstantTimeCompare(rawDigest[:], order.GuestOrderAccessTokenDigest) != 1 {
		return Order{}, ErrUnauthorized
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items

	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err != nil && err != ErrNotFound {
		return Order{}, err
	}
	if err == nil {
		order.Address = &addr
	}

	return order, nil
}

// CancelGuestOrder cancels a pending guest order after validating store scoping and guest capability.
func (r Repository) CancelGuestOrder(ctx context.Context, exec DBExecutor, storeID, orderID, rawCapability string, correlationID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" || strings.TrimSpace(rawCapability) == "" {
		return Order{}, ErrUnauthorized
	}

	var res Order
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var orderStore string
		var digest []byte
		var status string
		err := tx.QueryRow(ctx, `
			SELECT store_id, guest_order_access_token_digest, status
			FROM orders
			WHERE id = $1
			FOR UPDATE
		`, orderID).Scan(&orderStore, &digest, &status)
		if err != nil {
			return translatePGError(err, "lock guest order for cancel")
		}
		if orderStore != storeID {
			return ErrNotFound
		}

		rawDigest := sha256.Sum256([]byte(rawCapability))
		if len(digest) != sha256.Size || subtle.ConstantTimeCompare(rawDigest[:], digest) != 1 {
			return ErrUnauthorized
		}

		if status == OrderStatusCancelled {
			var getErr error
			res, getErr = r.GetOrderByID(ctx, tx, storeID, orderID)
			return getErr
		}
		if status != OrderStatusPending {
			return ErrInvalidTransition
		}

		var cancelErr error
		res, cancelErr = r.cancelPendingOrderExec(ctx, tx, storeID, orderID, AuthorityCustomer, nil, nil, correlationID)
		return cancelErr
	})
	return res, err
}

// GetOrderByNumber loads an order by order_number within a Store tenant boundary.

func (r Repository) GetOrderByNumber(ctx context.Context, exec DBExecutor, storeID, orderNumber string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderNumber) == "" {
		return Order{}, ErrInvalidInput
	}
	db := r.getExec(exec)

	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE order_number = $1 AND store_id = $2
	`, orderNumber, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "get order by number")
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items

	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err != nil && err != ErrNotFound {
		return Order{}, err
	}
	if err == nil {
		order.Address = &addr
	}

	return order, nil
}

func (r Repository) loadOrderItems(ctx context.Context, db DBExecutor, orderID string) ([]OrderItem, error) {
	rows, err := db.Query(ctx, `
		SELECT id, order_id, seller_listing_id, product_id, variant_id, sku_id,
		       supplier_offer_id, source_supplier_id, fulfillment_location_id,
		       inventory_reservation_id, product_title_snapshot, sku_code_snapshot,
		       unit_price_minor, currency_code, quantity, line_total_minor,
		       supplier_cost_minor, supplier_cost_currency_code, created_at
		FROM order_items
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, translatePGError(err, "load order items")
	}
	defer rows.Close()

	var items []OrderItem
	for rows.Next() {
		var item OrderItem
		err := rows.Scan(
			&item.ID, &item.OrderID, &item.SellerListingID, &item.ProductID, &item.VariantID, &item.SKUID,
			&item.SupplierOfferID, &item.SourceSupplierID, &item.FulfillmentLocationID,
			&item.InventoryReservationID, &item.ProductTitleSnapshot, &item.SKUCodeSnapshot,
			&item.UnitPriceMinor, &item.CurrencyCode, &item.Quantity, &item.LineTotalMinor,
			&item.SupplierCostMinor, &item.SupplierCostCurrencyCode, &item.CreatedAt,
		)
		if err != nil {
			return nil, translatePGError(err, "scan order item")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, translatePGError(err, "iterate order items")
	}
	return items, nil
}

func (r Repository) loadOrderAddress(ctx context.Context, db DBExecutor, orderID string) (OrderAddress, error) {
	var addr OrderAddress
	err := db.QueryRow(ctx, `
		SELECT id, order_id, address_type, recipient_name, phone, address_line_1,
		       address_line_2, city, region, postal_code, country_code, created_at
		FROM order_addresses
		WHERE order_id = $1
	`, orderID).Scan(
		&addr.ID, &addr.OrderID, &addr.AddressType, &addr.RecipientName, &addr.Phone,
		&addr.AddressLine1, &addr.AddressLine2, &addr.City, &addr.Region,
		&addr.PostalCode, &addr.CountryCode, &addr.CreatedAt,
	)
	if err != nil {
		return OrderAddress{}, translatePGError(err, "load order address")
	}
	return addr, nil
}

func getDBClockTimestamp(ctx context.Context, exec DBExecutor) (time.Time, error) {
	var ts time.Time
	err := exec.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&ts)
	if err != nil {
		return time.Time{}, translatePGError(err, "select clock_timestamp")
	}
	return ts.UTC(), nil
}

func lockSnapshotsByIDs(ctx context.Context, exec DBExecutor, snapshotIDs []string) (map[string]InventorySnapshot, error) {
	if len(snapshotIDs) == 0 {
		return map[string]InventorySnapshot{}, nil
	}
	sortedIDs := make([]string, len(snapshotIDs))
	copy(sortedIDs, snapshotIDs)
	sort.Strings(sortedIDs)

	rows, err := exec.Query(ctx, `
		SELECT id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
		FROM inventory_snapshots
		WHERE id = ANY($1)
		ORDER BY id ASC
		FOR UPDATE
	`, sortedIDs)
	if err != nil {
		return nil, translatePGError(err, "lock inventory snapshots")
	}
	defer rows.Close()

	res := make(map[string]InventorySnapshot)
	for rows.Next() {
		var s InventorySnapshot
		if err := rows.Scan(&s.ID, &s.FulfillmentLocationID, &s.SKUID, &s.OnHandQty, &s.ReservedQty, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, translatePGError(err, "scan inventory snapshot")
		}
		res[s.ID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, translatePGError(err, "iterate inventory snapshots")
	}
	return res, nil
}

func lockReservationsByIDs(ctx context.Context, exec DBExecutor, reservationIDs []string) ([]InventoryReservation, error) {
	if len(reservationIDs) == 0 {
		return nil, nil
	}
	sortedIDs := make([]string, len(reservationIDs))
	copy(sortedIDs, reservationIDs)
	sort.Strings(sortedIDs)

	rows, err := exec.Query(ctx, `
		SELECT id, inventory_snapshot_id, quantity, status, reservation_token, expires_at, created_at, updated_at
		FROM inventory_reservations
		WHERE id = ANY($1)
		ORDER BY id ASC
		FOR UPDATE
	`, sortedIDs)
	if err != nil {
		return nil, translatePGError(err, "lock inventory reservations")
	}
	defer rows.Close()

	var reservations []InventoryReservation
	for rows.Next() {
		var r InventoryReservation
		if err := rows.Scan(&r.ID, &r.InventorySnapshotID, &r.Quantity, &r.Status, &r.ReservationToken, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, translatePGError(err, "scan inventory reservation")
		}
		reservations = append(reservations, r)
	}
	if err := rows.Err(); err != nil {
		return nil, translatePGError(err, "iterate inventory reservations")
	}
	return reservations, nil
}

func requireTx(exec DBExecutor) (pgx.Tx, error) {
	if exec == nil {
		return nil, fmt.Errorf("transaction required: exec is nil")
	}
	tx, ok := exec.(pgx.Tx)
	if !ok || tx == nil {
		return nil, fmt.Errorf("transaction required: exec is not pgx.Tx")
	}
	return tx, nil
}

func isInventoryNeutralAdvance(from, to string, authority TransitionAuthority) bool {
	if authority != AuthoritySeller {
		return false
	}
	if from == OrderStatusConfirmed && to == OrderStatusProcessing {
		return true
	}
	if from == OrderStatusProcessing && to == OrderStatusReadyForShipping {
		return true
	}
	return false
}

var (
	testHookAfterOrderLock      func(ctx context.Context, orderID string)
	testHookBeforeOutboxEnqueue func(ctx context.Context) error
)

func enqueueStatusChangedEvent(ctx context.Context, exec DBExecutor, order Order, fromStatus, correlationID, causationID string, decisionNow time.Time) error {
	if testHookBeforeOutboxEnqueue != nil {
		if err := testHookBeforeOutboxEnqueue(ctx); err != nil {
			return err
		}
	}
	tx, ok := exec.(pgx.Tx)
	if !ok {
		return fmt.Errorf("outbox enqueue requires pgx.Tx")
	}
	envelope, err := NewOrderStatusChangedEvent(order, fromStatus, correlationID, causationID, decisionNow)
	if err != nil {
		return err
	}
	return outbox.NewStore().Enqueue(ctx, tx, envelope)
}

// ConfirmOrder performs the Seller pending -> confirmed inventory consumption transaction.
func (r Repository) ConfirmOrder(ctx context.Context, exec DBExecutor, storeID, orderID string, actorSubject *string, correlationID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" {
		return Order{}, ErrInvalidInput
	}
	if exec == nil {
		var res Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			res, err = r.confirmOrderExec(ctx, tx, storeID, orderID, actorSubject, correlationID)
			return err
		})
		return res, err
	}
	tx, err := requireTx(exec)
	if err != nil {
		return Order{}, err
	}
	return r.confirmOrderExec(ctx, tx, storeID, orderID, actorSubject, correlationID)
}

func (r Repository) confirmOrderExec(ctx context.Context, db DBExecutor, storeID, orderID string, actorSubject *string, correlationID string) (Order, error) {
	// 1. Lock Order FOR UPDATE
	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND store_id = $2
		FOR UPDATE
	`, orderID, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "lock order for confirm")
	}

	if testHookAfterOrderLock != nil {
		testHookAfterOrderLock(ctx, order.ID)
	}

	if order.Status == OrderStatusConfirmed {
		items, _ := r.loadOrderItems(ctx, db, order.ID)
		order.Items = items
		addr, _ := r.loadOrderAddress(ctx, db, order.ID)
		if addr.ID != "" {
			order.Address = &addr
		}
		return order, nil
	}

	if order.Status != OrderStatusPending {
		return Order{}, ErrInvalidTransition
	}

	// 2. Capture decision_now = clock_timestamp() AFTER order lock
	decisionNow, err := getDBClockTimestamp(ctx, db)
	if err != nil {
		return Order{}, err
	}

	// Validate deadline: decision_now < confirmation_deadline_at
	if !decisionNow.Before(order.ConfirmationDeadlineAt) {
		return Order{}, ErrInvalidTransition
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items
	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err == nil {
		order.Address = &addr
	}

	if len(items) == 0 {
		return Order{}, ErrInvalidInput
	}

	resIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.InventoryReservationID == "" {
			return Order{}, ErrInvalidInput
		}
		resIDs = append(resIDs, item.InventoryReservationID)
	}

	// Query distinct snapshot IDs linked to reservations
	var snapIDs []string
	rows, err := db.Query(ctx, `
		SELECT DISTINCT inventory_snapshot_id
		FROM inventory_reservations
		WHERE id = ANY($1)
		ORDER BY inventory_snapshot_id ASC
	`, resIDs)
	if err != nil {
		return Order{}, translatePGError(err, "query snapshot ids for confirmation")
	}
	for rows.Next() {
		var sID string
		if err := rows.Scan(&sID); err != nil {
			rows.Close()
			return Order{}, translatePGError(err, "scan snapshot id")
		}
		snapIDs = append(snapIDs, sID)
	}
	rows.Close()

	// 3. Lock linked Snapshots by id ASC
	snapshots, err := lockSnapshotsByIDs(ctx, db, snapIDs)
	if err != nil {
		return Order{}, err
	}

	// 4. Lock linked Reservations by id ASC
	reservations, err := lockReservationsByIDs(ctx, db, resIDs)
	if err != nil {
		return Order{}, err
	}
	if len(reservations) != len(resIDs) {
		return Order{}, ErrInternalError
	}

	// 5. Revalidate Reservation Invariants - Fail Closed if any mismatch occurs
	for _, res := range reservations {
		if res.Status != ReservationStatusHeld {
			return Order{}, ErrInvalidTransition
		}
		if res.ExpiresAt == nil || !decisionNow.Before(*res.ExpiresAt) {
			return Order{}, ErrInvalidTransition
		}
		if !res.ExpiresAt.Equal(order.ConfirmationDeadlineAt) {
			return Order{}, ErrInternalError
		}
	}

	// 6. Aggregate by snapshot
	aggregates := AggregateReservationsBySnapshot(reservations)

	// 7. Consume Inventory per snapshot
	for _, agg := range aggregates {
		snap, ok := snapshots[agg.SnapshotID]
		if !ok {
			return Order{}, ErrInvalidInput
		}
		newOnHand := snap.OnHandQty - agg.AggregateQuantity
		newReserved := snap.ReservedQty - agg.AggregateQuantity
		if newOnHand < 0 || newReserved < 0 || newReserved > newOnHand {
			return Order{}, ErrInsufficientInventory
		}

		newVersion := snap.Version + 1
		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_snapshots
			SET on_hand_qty = $1, reserved_qty = $2, version = $3, updated_at = $4
			WHERE id = $5 AND version = $6
		`, newOnHand, newReserved, newVersion, decisionNow, snap.ID, snap.Version)
		if err != nil {
			return Order{}, translatePGError(err, "update snapshot for confirm")
		}
		if cmdTag.RowsAffected() == 0 {
			return Order{}, ErrConflict
		}

		movID := uuid.NewString()
		_, err = db.Exec(ctx, `
			INSERT INTO inventory_movements (id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty, reason, principal_subject, correlation_id, causation_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'seller_confirmation', 'seller', $7, '', $8)
		`, movID, snap.ID, MovementTypeReservationConsumed, -agg.AggregateQuantity, newOnHand, newReserved, correlationID, decisionNow)
		if err != nil {
			return Order{}, translatePGError(err, "insert reservation_consumed movement")
		}
	}

	// 8. Mark reservations consumed
	cmdTag, err := db.Exec(ctx, `
		UPDATE inventory_reservations
		SET status = $1, updated_at = $2
		WHERE id = ANY($3) AND status = $4
	`, ReservationStatusConsumed, decisionNow, resIDs, ReservationStatusHeld)
	if err != nil {
		return Order{}, translatePGError(err, "update reservations to consumed")
	}
	if cmdTag.RowsAffected() != int64(len(resIDs)) {
		return Order{}, ErrConflict
	}

	// 9. Update Order
	fromStatus := order.Status
	order.Status = OrderStatusConfirmed
	order.AggregateVersion++
	order.UpdatedAt = decisionNow

	commandTag, err := db.Exec(ctx, `
		UPDATE orders
		SET status = $1, aggregate_version = $2, updated_at = $3
		WHERE id = $4 AND store_id = $5 AND status = $6
	`, order.Status, order.AggregateVersion, order.UpdatedAt, order.ID, order.StoreID, fromStatus)
	if err != nil {
		return Order{}, translatePGError(err, "update order status to confirmed")
	}
	if commandTag.RowsAffected() == 0 {
		return Order{}, ErrConflict
	}

	// 10. Append timeline
	timelineID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, actor_subject, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, timelineID, order.ID, fromStatus, order.Status, string(AuthoritySeller), actorSubject, decisionNow)
	if err != nil {
		return Order{}, translatePGError(err, "append order timeline for confirm")
	}

	// 11. Outbox enqueue
	if err := enqueueStatusChangedEvent(ctx, db, order, fromStatus, correlationID, "", decisionNow); err != nil {
		return Order{}, err
	}

	return order, nil
}

// CancelPendingOrder performs pending -> cancelled transition releasing held reservations.
func (r Repository) CancelPendingOrder(ctx context.Context, exec DBExecutor, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" {
		return Order{}, ErrInvalidInput
	}
	if authority == AuthorityScheduler {
		return Order{}, ErrInvalidTransition
	}
	if authority != AuthorityCustomer && authority != AuthoritySeller {
		return Order{}, ErrInvalidTransition
	}
	if exec == nil {
		var res Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			res, err = r.cancelPendingOrderExec(ctx, tx, storeID, orderID, authority, actorSubject, reason, correlationID)
			return err
		})
		return res, err
	}
	tx, err := requireTx(exec)
	if err != nil {
		return Order{}, err
	}
	return r.cancelPendingOrderExec(ctx, tx, storeID, orderID, authority, actorSubject, reason, correlationID)
}

func (r Repository) cancelPendingOrderExec(ctx context.Context, db DBExecutor, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	// 1. Lock Order FOR UPDATE
	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND store_id = $2
		FOR UPDATE
	`, orderID, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "lock order for cancel pending")
	}

	if order.Status == OrderStatusCancelled {
		items, _ := r.loadOrderItems(ctx, db, order.ID)
		order.Items = items
		addr, _ := r.loadOrderAddress(ctx, db, order.ID)
		if addr.ID != "" {
			order.Address = &addr
		}
		return order, nil
	}

	if order.Status != OrderStatusPending {
		return Order{}, ErrInvalidTransition
	}

	// 2. Capture decision_now = clock_timestamp() AFTER order lock
	decisionNow, err := getDBClockTimestamp(ctx, db)
	if err != nil {
		return Order{}, err
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items
	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err == nil {
		order.Address = &addr
	}

	resIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.InventoryReservationID == "" {
			return Order{}, ErrInvalidInput
		}
		resIDs = append(resIDs, item.InventoryReservationID)
	}

	var snapIDs []string
	if len(resIDs) > 0 {
		rows, err := db.Query(ctx, `
			SELECT DISTINCT inventory_snapshot_id
			FROM inventory_reservations
			WHERE id = ANY($1)
			ORDER BY inventory_snapshot_id ASC
		`, resIDs)
		if err != nil {
			return Order{}, translatePGError(err, "query snapshot ids for pending cancel")
		}
		for rows.Next() {
			var sID string
			if err := rows.Scan(&sID); err != nil {
				rows.Close()
				return Order{}, translatePGError(err, "scan snapshot id")
			}
			snapIDs = append(snapIDs, sID)
		}
		rows.Close()
	}

	// 3. Lock linked Snapshots by id ASC
	snapshots, err := lockSnapshotsByIDs(ctx, db, snapIDs)
	if err != nil {
		return Order{}, err
	}

	// 4. Lock linked Reservations by id ASC
	reservations, err := lockReservationsByIDs(ctx, db, resIDs)
	if err != nil {
		return Order{}, err
	}

	// Fail closed if any reservation is missing or not held, or deadline mismatch
	if len(reservations) != len(resIDs) {
		return Order{}, ErrInternalError
	}
	for _, res := range reservations {
		if res.Status != ReservationStatusHeld {
			return Order{}, ErrInternalError
		}
		if res.ExpiresAt == nil || !res.ExpiresAt.Equal(order.ConfirmationDeadlineAt) {
			return Order{}, ErrInternalError
		}
	}

	aggregates := AggregateReservationsBySnapshot(reservations)

	for _, agg := range aggregates {
		snap, ok := snapshots[agg.SnapshotID]
		if !ok {
			return Order{}, ErrInvalidInput
		}
		newReserved := snap.ReservedQty - agg.AggregateQuantity
		if newReserved < 0 {
			return Order{}, ErrInternalError
		}
		newVersion := snap.Version + 1

		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_snapshots
			SET reserved_qty = $1, version = $2, updated_at = $3
			WHERE id = $4 AND version = $5
		`, newReserved, newVersion, decisionNow, snap.ID, snap.Version)
		if err != nil {
			return Order{}, translatePGError(err, "update snapshot for pending cancel")
		}
		if cmdTag.RowsAffected() == 0 {
			return Order{}, ErrConflict
		}

		movID := uuid.NewString()
		reasonStr := "pending_cancellation"
		if reason != nil {
			reasonStr = *reason
		}
		_, err = db.Exec(ctx, `
			INSERT INTO inventory_movements (id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty, reason, principal_subject, correlation_id, causation_id, created_at)
			VALUES ($1, $2, $3, 0, $4, $5, $6, $7, $8, '', $9)
		`, movID, snap.ID, MovementTypeReservationReleased, snap.OnHandQty, newReserved, reasonStr, string(authority), correlationID, decisionNow)
		if err != nil {
			return Order{}, translatePGError(err, "insert reservation_released movement")
		}
	}

	if len(resIDs) > 0 {
		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_reservations
			SET status = $1, updated_at = $2
			WHERE id = ANY($3) AND status = $4
		`, ReservationStatusReleased, decisionNow, resIDs, ReservationStatusHeld)
		if err != nil {
			return Order{}, translatePGError(err, "update reservations to released")
		}
		if cmdTag.RowsAffected() != int64(len(resIDs)) {
			return Order{}, ErrConflict
		}
	}

	fromStatus := order.Status
	order.Status = OrderStatusCancelled
	order.CancellationReason = reason
	order.AggregateVersion++
	order.UpdatedAt = decisionNow

	commandTag, err := db.Exec(ctx, `
		UPDATE orders
		SET status = $1, cancellation_reason = $2, aggregate_version = $3, updated_at = $4
		WHERE id = $5 AND store_id = $6 AND status = $7
	`, order.Status, order.CancellationReason, order.AggregateVersion, order.UpdatedAt, order.ID, order.StoreID, fromStatus)
	if err != nil {
		return Order{}, translatePGError(err, "update order status to cancelled")
	}
	if commandTag.RowsAffected() == 0 {
		return Order{}, ErrConflict
	}

	timelineID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, actor_subject, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, timelineID, order.ID, fromStatus, order.Status, string(authority), actorSubject, reason, decisionNow)
	if err != nil {
		return Order{}, translatePGError(err, "append order timeline for pending cancel")
	}

	if err := enqueueStatusChangedEvent(ctx, db, order, fromStatus, correlationID, "", decisionNow); err != nil {
		return Order{}, err
	}

	return order, nil
}

// CancelConfirmedOrder performs post-confirm cancellation restocking on hand inventory.
func (r Repository) CancelConfirmedOrder(ctx context.Context, exec DBExecutor, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" {
		return Order{}, ErrInvalidInput
	}
	if authority != AuthoritySeller {
		return Order{}, ErrInvalidTransition
	}
	if exec == nil {
		var res Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			res, err = r.cancelConfirmedOrderExec(ctx, tx, storeID, orderID, authority, actorSubject, reason, correlationID)
			return err
		})
		return res, err
	}
	tx, err := requireTx(exec)
	if err != nil {
		return Order{}, err
	}
	return r.cancelConfirmedOrderExec(ctx, tx, storeID, orderID, authority, actorSubject, reason, correlationID)
}

func (r Repository) cancelConfirmedOrderExec(ctx context.Context, db DBExecutor, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	// 1. Lock Order FOR UPDATE
	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND store_id = $2
		FOR UPDATE
	`, orderID, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "lock order for cancel confirmed")
	}

	if order.Status == OrderStatusCancelled {
		items, _ := r.loadOrderItems(ctx, db, order.ID)
		order.Items = items
		addr, _ := r.loadOrderAddress(ctx, db, order.ID)
		if addr.ID != "" {
			order.Address = &addr
		}
		return order, nil
	}

	if order.Status != OrderStatusConfirmed && order.Status != OrderStatusProcessing {
		return Order{}, ErrInvalidTransition
	}

	// 2. Capture decision_now = clock_timestamp() AFTER order lock
	decisionNow, err := getDBClockTimestamp(ctx, db)
	if err != nil {
		return Order{}, err
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items
	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err == nil {
		order.Address = &addr
	}

	resIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.InventoryReservationID == "" {
			return Order{}, ErrInvalidInput
		}
		resIDs = append(resIDs, item.InventoryReservationID)
	}

	var snapIDs []string
	if len(resIDs) > 0 {
		rows, err := db.Query(ctx, `
			SELECT DISTINCT inventory_snapshot_id
			FROM inventory_reservations
			WHERE id = ANY($1)
			ORDER BY inventory_snapshot_id ASC
		`, resIDs)
		if err != nil {
			return Order{}, translatePGError(err, "query snapshot ids for post-confirm cancel")
		}
		for rows.Next() {
			var sID string
			if err := rows.Scan(&sID); err != nil {
				rows.Close()
				return Order{}, translatePGError(err, "scan snapshot id")
			}
			snapIDs = append(snapIDs, sID)
		}
		rows.Close()
	}

	// 3. Lock Snapshots by id ASC
	snapshots, err := lockSnapshotsByIDs(ctx, db, snapIDs)
	if err != nil {
		return Order{}, err
	}

	// 4. Lock Reservations by id ASC
	reservations, err := lockReservationsByIDs(ctx, db, resIDs)
	if err != nil {
		return Order{}, err
	}

	// Fail closed if any reservation is missing or not consumed, or deadline mismatch
	if len(reservations) != len(resIDs) {
		return Order{}, ErrInternalError
	}
	for _, res := range reservations {
		if res.Status != ReservationStatusConsumed {
			return Order{}, ErrInternalError
		}
		if res.ExpiresAt == nil || !res.ExpiresAt.Equal(order.ConfirmationDeadlineAt) {
			return Order{}, ErrInternalError
		}
	}

	aggregates := AggregateReservationsBySnapshot(reservations)

	for _, agg := range aggregates {
		snap, ok := snapshots[agg.SnapshotID]
		if !ok {
			return Order{}, ErrInvalidInput
		}
		newOnHand := snap.OnHandQty + agg.AggregateQuantity
		newVersion := snap.Version + 1

		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_snapshots
			SET on_hand_qty = $1, version = $2, updated_at = $3
			WHERE id = $4 AND version = $5
		`, newOnHand, newVersion, decisionNow, snap.ID, snap.Version)
		if err != nil {
			return Order{}, translatePGError(err, "update snapshot for restock")
		}
		if cmdTag.RowsAffected() == 0 {
			return Order{}, ErrConflict
		}

		movID := uuid.NewString()
		reasonStr := "order_cancellation_restock"
		if reason != nil {
			reasonStr = *reason
		}
		_, err = db.Exec(ctx, `
			INSERT INTO inventory_movements (id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty, reason, principal_subject, correlation_id, causation_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', $10)
		`, movID, snap.ID, MovementTypeOrderCancellationRestock, agg.AggregateQuantity, newOnHand, snap.ReservedQty, reasonStr, string(authority), correlationID, decisionNow)
		if err != nil {
			return Order{}, translatePGError(err, "insert order_cancellation_restock movement")
		}
	}

	fromStatus := order.Status
	order.Status = OrderStatusCancelled
	order.CancellationReason = reason
	order.AggregateVersion++
	order.UpdatedAt = decisionNow

	commandTag, err := db.Exec(ctx, `
		UPDATE orders
		SET status = $1, cancellation_reason = $2, aggregate_version = $3, updated_at = $4
		WHERE id = $5 AND store_id = $6 AND status = $7
	`, order.Status, order.CancellationReason, order.AggregateVersion, order.UpdatedAt, order.ID, order.StoreID, fromStatus)
	if err != nil {
		return Order{}, translatePGError(err, "update order status to cancelled from confirmed")
	}
	if commandTag.RowsAffected() == 0 {
		return Order{}, ErrConflict
	}

	timelineID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, actor_subject, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, timelineID, order.ID, fromStatus, order.Status, string(authority), actorSubject, reason, decisionNow)
	if err != nil {
		return Order{}, translatePGError(err, "append order timeline for post-confirm cancel")
	}

	if err := enqueueStatusChangedEvent(ctx, db, order, fromStatus, correlationID, "", decisionNow); err != nil {
		return Order{}, err
	}

	return order, nil
}

// ExpirePendingOrder executes Stage 2 of the confirmation-timeout expiry workflow for a single candidate Order.
func (r Repository) ExpirePendingOrder(ctx context.Context, exec DBExecutor, orderID string) (Order, error) {
	if strings.TrimSpace(orderID) == "" {
		return Order{}, ErrInvalidInput
	}
	if exec == nil {
		var res Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			res, err = r.expirePendingOrderExec(ctx, tx, orderID)
			return err
		})
		return res, err
	}
	tx, err := requireTx(exec)
	if err != nil {
		return Order{}, err
	}
	return r.expirePendingOrderExec(ctx, tx, orderID)
}

func (r Repository) expirePendingOrderExec(ctx context.Context, db DBExecutor, orderID string) (Order, error) {
	// 1. Lock Order FOR UPDATE
	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`, orderID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, nil // Safe no-op if row not found
		}
		return Order{}, translatePGError(err, "lock order for expiry")
	}

	if testHookAfterOrderLock != nil {
		testHookAfterOrderLock(ctx, order.ID)
	}

	if order.Status != OrderStatusPending {
		items, _ := r.loadOrderItems(ctx, db, order.ID)
		order.Items = items
		addr, _ := r.loadOrderAddress(ctx, db, order.ID)
		if addr.ID != "" {
			order.Address = &addr
		}
		return order, nil // Safe no-op if no longer pending
	}

	// 2. Capture decision_now = clock_timestamp() AFTER order lock
	decisionNow, err := getDBClockTimestamp(ctx, db)
	if err != nil {
		return Order{}, err
	}

	// Re-verify deadline: if decision_now < confirmation_deadline_at, no-op
	if decisionNow.Before(order.ConfirmationDeadlineAt) {
		return order, nil
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err != nil {
		return Order{}, err
	}
	order.Items = items
	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err == nil {
		order.Address = &addr
	}

	resIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.InventoryReservationID == "" {
			return Order{}, ErrInvalidInput
		}
		resIDs = append(resIDs, item.InventoryReservationID)
	}

	var snapIDs []string
	if len(resIDs) > 0 {
		rows, err := db.Query(ctx, `
			SELECT DISTINCT inventory_snapshot_id
			FROM inventory_reservations
			WHERE id = ANY($1)
			ORDER BY inventory_snapshot_id ASC
		`, resIDs)
		if err != nil {
			return Order{}, translatePGError(err, "query snapshot ids for expiry")
		}
		for rows.Next() {
			var sID string
			if err := rows.Scan(&sID); err != nil {
				rows.Close()
				return Order{}, translatePGError(err, "scan snapshot id for expiry")
			}
			snapIDs = append(snapIDs, sID)
		}
		rows.Close()
	}

	// 3. Lock linked Snapshots by id ASC
	snapshots, err := lockSnapshotsByIDs(ctx, db, snapIDs)
	if err != nil {
		return Order{}, err
	}

	// 4. Lock linked Reservations by id ASC
	reservations, err := lockReservationsByIDs(ctx, db, resIDs)
	if err != nil {
		return Order{}, err
	}

	// Fail closed if any reservation is missing or not held, or deadline mismatch
	if len(reservations) != len(resIDs) {
		return Order{}, ErrInternalError
	}
	for _, res := range reservations {
		if res.Status != ReservationStatusHeld {
			return Order{}, ErrInternalError
		}
		if res.ExpiresAt == nil || !res.ExpiresAt.Equal(order.ConfirmationDeadlineAt) {
			return Order{}, ErrInternalError
		}
	}

	aggregates := AggregateReservationsBySnapshot(reservations)

	for _, agg := range aggregates {
		snap, ok := snapshots[agg.SnapshotID]
		if !ok {
			return Order{}, ErrInvalidInput
		}
		newReserved := snap.ReservedQty - agg.AggregateQuantity
		if newReserved < 0 {
			return Order{}, ErrInternalError
		}
		newVersion := snap.Version + 1

		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_snapshots
			SET reserved_qty = $1, version = $2, updated_at = $3
			WHERE id = $4 AND version = $5
		`, newReserved, newVersion, decisionNow, snap.ID, snap.Version)
		if err != nil {
			return Order{}, translatePGError(err, "update snapshot for expiry")
		}
		if cmdTag.RowsAffected() == 0 {
			return Order{}, ErrConflict
		}

		movID := uuid.NewString()
		_, err = db.Exec(ctx, `
			INSERT INTO inventory_movements (id, inventory_snapshot_id, movement_type, quantity_delta, on_hand_qty, reserved_qty, reason, principal_subject, correlation_id, causation_id, created_at)
			VALUES ($1, $2, $3, 0, $4, $5, 'confirmation_timeout', 'scheduler', '', '', $6)
		`, movID, snap.ID, MovementTypeReservationExpired, snap.OnHandQty, newReserved, decisionNow)
		if err != nil {
			return Order{}, translatePGError(err, "insert reservation_expired movement")
		}
	}

	if len(resIDs) > 0 {
		cmdTag, err := db.Exec(ctx, `
			UPDATE inventory_reservations
			SET status = $1, updated_at = $2
			WHERE id = ANY($3) AND status = $4
		`, ReservationStatusExpired, decisionNow, resIDs, ReservationStatusHeld)
		if err != nil {
			return Order{}, translatePGError(err, "update reservations to expired")
		}
		if cmdTag.RowsAffected() != int64(len(resIDs)) {
			return Order{}, ErrConflict
		}
	}

	fromStatus := order.Status
	timeoutReason := "confirmation_timeout"
	order.Status = OrderStatusCancelled
	order.CancellationReason = &timeoutReason
	order.AggregateVersion++
	order.UpdatedAt = decisionNow

	commandTag, err := db.Exec(ctx, `
		UPDATE orders
		SET status = $1, cancellation_reason = $2, aggregate_version = $3, updated_at = $4
		WHERE id = $5 AND status = $6
	`, order.Status, order.CancellationReason, order.AggregateVersion, order.UpdatedAt, order.ID, fromStatus)
	if err != nil {
		return Order{}, translatePGError(err, "update order status to cancelled for expiry")
	}
	if commandTag.RowsAffected() == 0 {
		return order, nil
	}

	timelineID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, timelineID, order.ID, fromStatus, order.Status, string(AuthorityScheduler), timeoutReason, decisionNow)
	if err != nil {
		return Order{}, translatePGError(err, "append order timeline for expiry")
	}

	if err := enqueueStatusChangedEvent(ctx, db, order, fromStatus, "", "", decisionNow); err != nil {
		return Order{}, err
	}

	return order, nil
}

// DiscoverExpiryCandidates executes Stage 1 of the confirmation-timeout scheduler.
func (r Repository) DiscoverExpiryCandidates(ctx context.Context, exec DBExecutor, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	db := r.getExec(exec)
	rows, err := db.Query(ctx, `
		SELECT id
		FROM orders
		WHERE status = 'pending'
		  AND confirmation_deadline_at <= clock_timestamp()
		ORDER BY confirmation_deadline_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, translatePGError(err, "discover expiry candidates")
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, translatePGError(err, "scan expiry candidate id")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, translatePGError(err, "iterate expiry candidates")
	}
	return ids, nil
}

// AdvanceOrderStatus performs inventory-neutral Order state transitions.
func (r Repository) AdvanceOrderStatus(
	ctx context.Context,
	exec DBExecutor,
	storeID, orderID string,
	targetStatus string,
	authority TransitionAuthority,
	actorSubject *string,
	reason *string,
	correlationID string,
) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" || strings.TrimSpace(targetStatus) == "" {
		return Order{}, ErrInvalidInput
	}
	if exec == nil {
		var res Order
		err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			res, err = r.advanceOrderStatusExec(ctx, tx, storeID, orderID, targetStatus, authority, actorSubject, reason, correlationID)
			return err
		})
		return res, err
	}
	tx, err := requireTx(exec)
	if err != nil {
		return Order{}, err
	}
	return r.advanceOrderStatusExec(ctx, tx, storeID, orderID, targetStatus, authority, actorSubject, reason, correlationID)
}

func (r Repository) advanceOrderStatusExec(
	ctx context.Context,
	db DBExecutor,
	storeID, orderID string,
	targetStatus string,
	authority TransitionAuthority,
	actorSubject *string,
	reason *string,
	correlationID string,
) (Order, error) {
	// 1. Lock Order FOR UPDATE
	var order Order
	err := db.QueryRow(ctx, `
		SELECT id, order_number, store_id, market_code, customer_id, checkout_session_id,
		       status, currency_code, guest_order_access_token_digest, subtotal_minor,
		       total_minor, confirmation_deadline_at, cancellation_reason, aggregate_version,
		       created_at, updated_at
		FROM orders
		WHERE id = $1 AND store_id = $2
		FOR UPDATE
	`, orderID, storeID).Scan(
		&order.ID, &order.OrderNumber, &order.StoreID, &order.MarketCode, &order.CustomerID,
		&order.CheckoutSessionID, &order.Status, &order.CurrencyCode, &order.GuestOrderAccessTokenDigest,
		&order.SubtotalMinor, &order.TotalMinor, &order.ConfirmationDeadlineAt,
		&order.CancellationReason, &order.AggregateVersion, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return Order{}, translatePGError(err, "lock order for advance status")
	}

	if order.Status == targetStatus {
		items, _ := r.loadOrderItems(ctx, db, order.ID)
		order.Items = items
		addr, _ := r.loadOrderAddress(ctx, db, order.ID)
		if addr.ID != "" {
			order.Address = &addr
		}
		return order, nil
	}

	// Restrict to whitelisted inventory-neutral transitions only: confirmed -> processing, processing -> ready_for_shipping
	if !isInventoryNeutralAdvance(order.Status, targetStatus, authority) {
		return Order{}, ErrInvalidTransition
	}

	// 2. Capture decision_now = clock_timestamp() AFTER order lock
	decisionNow, err := getDBClockTimestamp(ctx, db)
	if err != nil {
		return Order{}, err
	}

	if err := ValidateOrderTransition(order.Status, authority, targetStatus, order.ConfirmationDeadlineAt, decisionNow); err != nil {
		return Order{}, err
	}

	fromStatus := order.Status
	order.Status = targetStatus
	if reason != nil {
		order.CancellationReason = reason
	}
	order.AggregateVersion++
	order.UpdatedAt = decisionNow

	commandTag, err := db.Exec(ctx, `
		UPDATE orders
		SET status = $1, cancellation_reason = $2, aggregate_version = $3, updated_at = $4
		WHERE id = $5 AND store_id = $6 AND status = $7
	`, order.Status, order.CancellationReason, order.AggregateVersion, order.UpdatedAt, order.ID, order.StoreID, fromStatus)
	if err != nil {
		return Order{}, translatePGError(err, "update order status advance")
	}
	if commandTag.RowsAffected() == 0 {
		return Order{}, ErrConflict
	}

	timelineID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, actor_subject, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, timelineID, order.ID, fromStatus, order.Status, string(authority), actorSubject, reason, decisionNow)
	if err != nil {
		return Order{}, translatePGError(err, "append order timeline for advance status")
	}

	items, err := r.loadOrderItems(ctx, db, order.ID)
	if err == nil {
		order.Items = items
	}
	addr, err := r.loadOrderAddress(ctx, db, order.ID)
	if err == nil {
		order.Address = &addr
	}

	if err := enqueueStatusChangedEvent(ctx, db, order, fromStatus, correlationID, "", decisionNow); err != nil {
		return Order{}, err
	}

	return order, nil
}

// UpdateOrderStatus delegates to appropriate transition execution command.
func (r Repository) UpdateOrderStatus(
	ctx context.Context,
	exec DBExecutor,
	storeID, orderID string,
	targetStatus string,
	authority TransitionAuthority,
	actorSubject *string,
	reason *string,
	decisionNow time.Time,
) (Order, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" || strings.TrimSpace(targetStatus) == "" {
		return Order{}, ErrInvalidInput
	}
	if authority == AuthorityScheduler {
		return Order{}, ErrInvalidTransition
	}

	db := r.getExec(exec)
	var currentStatus string
	err := db.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1 AND store_id = $2`, orderID, storeID).Scan(&currentStatus)
	if err != nil {
		return Order{}, translatePGError(err, "peek order status for transition")
	}

	if currentStatus == targetStatus {
		return r.GetOrderByID(ctx, exec, storeID, orderID)
	}

	switch currentStatus {
	case OrderStatusPending:
		switch targetStatus {
		case OrderStatusConfirmed:
			if authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.ConfirmOrder(ctx, exec, storeID, orderID, actorSubject, "")
		case OrderStatusCancelled:
			if authority != AuthorityCustomer && authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.CancelPendingOrder(ctx, exec, storeID, orderID, authority, actorSubject, reason, "")
		default:
			return Order{}, ErrInvalidTransition
		}
	case OrderStatusConfirmed:
		switch targetStatus {
		case OrderStatusProcessing:
			if authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.AdvanceOrderStatus(ctx, exec, storeID, orderID, targetStatus, authority, actorSubject, reason, "")
		case OrderStatusCancelled:
			if authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.CancelConfirmedOrder(ctx, exec, storeID, orderID, authority, actorSubject, reason, "")
		default:
			return Order{}, ErrInvalidTransition
		}
	case OrderStatusProcessing:
		switch targetStatus {
		case OrderStatusReadyForShipping:
			if authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.AdvanceOrderStatus(ctx, exec, storeID, orderID, targetStatus, authority, actorSubject, reason, "")
		case OrderStatusCancelled:
			if authority != AuthoritySeller {
				return Order{}, ErrInvalidTransition
			}
			return r.CancelConfirmedOrder(ctx, exec, storeID, orderID, authority, actorSubject, reason, "")
		default:
			return Order{}, ErrInvalidTransition
		}
	default:
		return Order{}, ErrInvalidTransition
	}
}

// AppendTimeline appends an immutable entry to order_timeline.
func (r Repository) AppendTimeline(ctx context.Context, exec DBExecutor, timeline OrderTimeline) (OrderTimeline, error) {
	if strings.TrimSpace(timeline.OrderID) == "" || strings.TrimSpace(timeline.ToStatus) == "" || strings.TrimSpace(timeline.ActorType) == "" {
		return OrderTimeline{}, ErrInvalidInput
	}
	db := r.getExec(exec)
	if strings.TrimSpace(timeline.ID) == "" {
		timeline.ID = uuid.NewString()
	}
	if timeline.CreatedAt.IsZero() {
		timeline.CreatedAt = time.Now().UTC()
	}

	_, err := db.Exec(ctx, `
		INSERT INTO order_timeline (id, order_id, from_status, to_status, actor_type, actor_subject, reason, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, timeline.ID, timeline.OrderID, timeline.FromStatus, timeline.ToStatus, timeline.ActorType, timeline.ActorSubject, timeline.Reason, timeline.Metadata, timeline.CreatedAt)
	if err != nil {
		return OrderTimeline{}, translatePGError(err, "append timeline")
	}
	return timeline, nil
}

// CreateNote adds an internal note to an Order.
func (r Repository) CreateNote(ctx context.Context, exec DBExecutor, note OrderNote) (OrderNote, error) {
	if strings.TrimSpace(note.OrderID) == "" || strings.TrimSpace(note.AuthorSubject) == "" || strings.TrimSpace(note.Body) == "" {
		return OrderNote{}, ErrInvalidInput
	}
	if note.Visibility == "" {
		note.Visibility = NoteVisibilityInternal
	}
	if note.Visibility != NoteVisibilityInternal {
		return OrderNote{}, ErrInvalidInput
	}
	db := r.getExec(exec)
	if strings.TrimSpace(note.ID) == "" {
		note.ID = uuid.NewString()
	}
	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now().UTC()
	}

	_, err := db.Exec(ctx, `
		INSERT INTO order_notes (id, order_id, author_subject, visibility, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, note.ID, note.OrderID, note.AuthorSubject, note.Visibility, note.Body, note.CreatedAt)
	if err != nil {
		return OrderNote{}, translatePGError(err, "create order note")
	}
	return note, nil
}

// GetNotes lists notes for an Order within a Store boundary.
func (r Repository) GetNotes(ctx context.Context, exec DBExecutor, storeID, orderID string) ([]OrderNote, error) {
	if strings.TrimSpace(storeID) == "" || strings.TrimSpace(orderID) == "" {
		return nil, ErrInvalidInput
	}
	db := r.getExec(exec)
	// Verify order store ownership
	var foundID string
	err := db.QueryRow(ctx, `SELECT id FROM orders WHERE id = $1 AND store_id = $2`, orderID, storeID).Scan(&foundID)
	if err != nil {
		return nil, translatePGError(err, "verify order store for notes")
	}

	rows, err := db.Query(ctx, `
		SELECT id, order_id, author_subject, visibility, body, created_at
		FROM order_notes
		WHERE order_id = $1
		ORDER BY created_at DESC
	`, orderID)
	if err != nil {
		return nil, translatePGError(err, "get order notes")
	}
	defer rows.Close()

	var notes []OrderNote
	for rows.Next() {
		var n OrderNote
		if err := rows.Scan(&n.ID, &n.OrderID, &n.AuthorSubject, &n.Visibility, &n.Body, &n.CreatedAt); err != nil {
			return nil, translatePGError(err, "scan order note")
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, translatePGError(err, "iterate order notes")
	}
	return notes, nil
}
