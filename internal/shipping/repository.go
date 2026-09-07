package shipping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Repository struct {
	pool DBPool
}

func NewRepository(pool DBPool) Repository {
	return Repository{pool: pool}
}

func (r Repository) getExec(exec DBExecutor) DBExecutor {
	if exec != nil {
		return exec
	}
	return r.pool
}

func (r Repository) withTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if r.pool == nil {
		return errors.New("database pool is not initialized")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r Repository) CreateShipmentTx(ctx context.Context, tx pgx.Tx, shipment Shipment, items []ShipmentItem, event ShipmentEvent) error {
	var tracking *string
	if strings.TrimSpace(shipment.TrackingNumber) != "" {
		tn := strings.TrimSpace(shipment.TrackingNumber)
		tracking = &tn
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO shipments (
			id, order_id, fulfillment_location_id, status, tracking_number,
			shipping_cost_minor, cod_amount_minor, currency, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, shipment.ID, shipment.OrderID, shipment.FulfillmentLocationID, string(shipment.Status), tracking,
		shipment.ShippingCostMinor, shipment.CodAmountMinor, shipment.Currency, shipment.CreatedAt, shipment.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert shipment: %w", err)
	}

	for _, item := range items {
		_, err := tx.Exec(ctx, `
			INSERT INTO shipment_items (id, shipment_id, order_item_id, quantity, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, item.ID, item.ShipmentID, item.OrderItemID, item.Quantity, item.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert shipment item: %w", err)
		}
	}

	var notes *string
	if strings.TrimSpace(event.Notes) != "" {
		n := strings.TrimSpace(event.Notes)
		notes = &n
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO shipment_events (id, shipment_id, status, notes, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`, event.ID, event.ShipmentID, string(event.Status), notes, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert shipment event: %w", err)
	}

	return nil
}

func (r Repository) UpdateShipmentStatusTx(
	ctx context.Context,
	tx pgx.Tx,
	shipmentID string,
	newStatus Status,
	trackingNumber string,
	notes string,
	occurredAt time.Time,
) (*Shipment, *ShipmentEvent, Status, error) {
	var (
		s             Shipment
		currentStatus string
		dbTracking    sql.NullString
	)

	err := tx.QueryRow(ctx, `
		SELECT id, order_id, fulfillment_location_id, status, tracking_number,
		       shipping_cost_minor, cod_amount_minor, currency, created_at, updated_at
		FROM shipments
		WHERE id = $1
		FOR UPDATE
	`, shipmentID).Scan(
		&s.ID, &s.OrderID, &s.FulfillmentLocationID, &currentStatus, &dbTracking,
		&s.ShippingCostMinor, &s.CodAmountMinor, &s.Currency, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, "", ErrShipmentNotFound
		}
		return nil, nil, "", fmt.Errorf("lock shipment: %w", err)
	}
	if dbTracking.Valid {
		s.TrackingNumber = dbTracking.String
	}

	fromStatus := Status(currentStatus)
	if err := ValidateTransition(fromStatus, newStatus); err != nil {
		return nil, nil, fromStatus, err
	}

	finalTracking := s.TrackingNumber
	if strings.TrimSpace(trackingNumber) != "" {
		finalTracking = strings.TrimSpace(trackingNumber)
	}

	var trackingParam *string
	if finalTracking != "" {
		trackingParam = &finalTracking
	}

	updatedAt := occurredAt
	_, err = tx.Exec(ctx, `
		UPDATE shipments
		SET status = $2, tracking_number = $3, updated_at = $4
		WHERE id = $1
	`, shipmentID, string(newStatus), trackingParam, updatedAt)
	if err != nil {
		return nil, nil, fromStatus, fmt.Errorf("update shipment status: %w", err)
	}

	s.Status = newStatus
	s.TrackingNumber = finalTracking
	s.UpdatedAt = updatedAt

	eventID := uuid.NewString()
	event := ShipmentEvent{
		ID:         eventID,
		ShipmentID: shipmentID,
		Status:     newStatus,
		Notes:      notes,
		OccurredAt: occurredAt,
	}

	var notesParam *string
	if strings.TrimSpace(notes) != "" {
		n := strings.TrimSpace(notes)
		notesParam = &n
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO shipment_events (id, shipment_id, status, notes, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`, event.ID, event.ShipmentID, string(event.Status), notesParam, event.OccurredAt)
	if err != nil {
		return nil, nil, fromStatus, fmt.Errorf("insert shipment event: %w", err)
	}

	return &s, &event, fromStatus, nil
}

func (r Repository) GetShipmentByID(ctx context.Context, exec DBExecutor, shipmentID string) (*Shipment, error) {
	if strings.TrimSpace(shipmentID) == "" {
		return nil, ErrInvalidInput
	}
	db := r.getExec(exec)

	var (
		s          Shipment
		statusStr  string
		dbTracking sql.NullString
	)

	err := db.QueryRow(ctx, `
		SELECT id, order_id, fulfillment_location_id, status, tracking_number,
		       shipping_cost_minor, cod_amount_minor, currency, created_at, updated_at
		FROM shipments
		WHERE id = $1
	`, shipmentID).Scan(
		&s.ID, &s.OrderID, &s.FulfillmentLocationID, &statusStr, &dbTracking,
		&s.ShippingCostMinor, &s.CodAmountMinor, &s.Currency, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrShipmentNotFound
		}
		return nil, fmt.Errorf("get shipment by id: %w", err)
	}

	s.Status = Status(statusStr)
	if dbTracking.Valid {
		s.TrackingNumber = dbTracking.String
	}

	items, err := r.ListShipmentItems(ctx, db, s.ID)
	if err != nil {
		return nil, err
	}
	s.Items = items

	events, err := r.ListShipmentEvents(ctx, db, s.ID)
	if err != nil {
		return nil, err
	}
	s.Events = events

	return &s, nil
}

func (r Repository) ListShipmentsByOrderID(ctx context.Context, exec DBExecutor, orderID string) ([]Shipment, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, ErrInvalidInput
	}
	db := r.getExec(exec)

	rows, err := db.Query(ctx, `
		SELECT id, order_id, fulfillment_location_id, status, tracking_number,
		       shipping_cost_minor, cod_amount_minor, currency, created_at, updated_at
		FROM shipments
		WHERE order_id = $1
		ORDER BY created_at ASC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list shipments by order id: %w", err)
	}
	defer rows.Close()

	var shipments []Shipment
	for rows.Next() {
		var (
			s          Shipment
			statusStr  string
			dbTracking sql.NullString
		)
		if err := rows.Scan(
			&s.ID, &s.OrderID, &s.FulfillmentLocationID, &statusStr, &dbTracking,
			&s.ShippingCostMinor, &s.CodAmountMinor, &s.Currency, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan shipment row: %w", err)
		}
		s.Status = Status(statusStr)
		if dbTracking.Valid {
			s.TrackingNumber = dbTracking.String
		}
		shipments = append(shipments, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing shipments: %w", err)
	}

	for i := range shipments {
		items, err := r.ListShipmentItems(ctx, db, shipments[i].ID)
		if err != nil {
			return nil, err
		}
		shipments[i].Items = items

		evs, err := r.ListShipmentEvents(ctx, db, shipments[i].ID)
		if err != nil {
			return nil, err
		}
		shipments[i].Events = evs
	}

	return shipments, nil
}

func (r Repository) ListShipmentItems(ctx context.Context, exec DBExecutor, shipmentID string) ([]ShipmentItem, error) {
	db := r.getExec(exec)
	rows, err := db.Query(ctx, `
		SELECT id, shipment_id, order_item_id, quantity, created_at
		FROM shipment_items
		WHERE shipment_id = $1
		ORDER BY created_at ASC
	`, shipmentID)
	if err != nil {
		return nil, fmt.Errorf("list shipment items: %w", err)
	}
	defer rows.Close()

	var items []ShipmentItem
	for rows.Next() {
		var item ShipmentItem
		if err := rows.Scan(&item.ID, &item.ShipmentID, &item.OrderItemID, &item.Quantity, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan shipment item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r Repository) ListShipmentEvents(ctx context.Context, exec DBExecutor, shipmentID string) ([]ShipmentEvent, error) {
	db := r.getExec(exec)
	rows, err := db.Query(ctx, `
		SELECT id, shipment_id, status, notes, occurred_at
		FROM shipment_events
		WHERE shipment_id = $1
		ORDER BY occurred_at ASC
	`, shipmentID)
	if err != nil {
		return nil, fmt.Errorf("list shipment events: %w", err)
	}
	defer rows.Close()

	var eventsList []ShipmentEvent
	for rows.Next() {
		var (
			e       ShipmentEvent
			stStr   string
			dbNotes sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.ShipmentID, &stStr, &dbNotes, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan shipment event: %w", err)
		}
		e.Status = Status(stStr)
		if dbNotes.Valid {
			e.Notes = dbNotes.String
		}
		eventsList = append(eventsList, e)
	}
	return eventsList, rows.Err()
}
