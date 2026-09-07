package shipping

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pkgEvents "github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

type Service struct {
	repo        Repository
	outboxStore outbox.Store
}

func NewService(repo Repository) Service {
	return Service{
		repo:        repo,
		outboxStore: outbox.NewStore(),
	}
}

func (s Service) CreateShipment(ctx context.Context, params CreateShipmentParams) (*Shipment, error) {
	if strings.TrimSpace(params.OrderID) == "" || strings.TrimSpace(params.FulfillmentLocationID) == "" {
		return nil, fmt.Errorf("%w: order_id and fulfillment_location_id are required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.Currency) == "" {
		return nil, fmt.Errorf("%w: currency is required", ErrInvalidInput)
	}
	if len(params.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one shipment item is required", ErrInvalidInput)
	}
	for _, item := range params.Items {
		if strings.TrimSpace(item.OrderItemID) == "" || item.Quantity <= 0 {
			return nil, fmt.Errorf("%w: order_item_id and positive quantity are required", ErrInvalidInput)
		}
	}

	now := time.Now().UTC()
	shipmentID := uuid.NewString()
	initialStatus := StatusPending

	shipment := Shipment{
		ID:                    shipmentID,
		OrderID:               params.OrderID,
		FulfillmentLocationID: params.FulfillmentLocationID,
		Status:                initialStatus,
		TrackingNumber:        strings.TrimSpace(params.TrackingNumber),
		ShippingCostMinor:     params.ShippingCostMinor,
		CodAmountMinor:        params.CodAmountMinor,
		Currency:              strings.ToUpper(strings.TrimSpace(params.Currency)),
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	items := make([]ShipmentItem, 0, len(params.Items))
	itemPayloads := make([]pkgEvents.ShipmentItemPayload, 0, len(params.Items))
	for _, item := range params.Items {
		itemID := uuid.NewString()
		shItem := ShipmentItem{
			ID:          itemID,
			ShipmentID:  shipmentID,
			OrderItemID: item.OrderItemID,
			Quantity:    item.Quantity,
			CreatedAt:   now,
		}
		items = append(items, shItem)
		itemPayloads = append(itemPayloads, pkgEvents.ShipmentItemPayload{
			ID:          itemID,
			ShipmentID:  shipmentID,
			OrderItemID: item.OrderItemID,
			Quantity:    item.Quantity,
		})
	}

	event := ShipmentEvent{
		ID:         uuid.NewString(),
		ShipmentID: shipmentID,
		Status:     initialStatus,
		Notes:      "Shipment created",
		OccurredAt: now,
	}

	outboxPayload := pkgEvents.ShipmentCreatedPayload{
		ShipmentID:            shipment.ID,
		OrderID:               shipment.OrderID,
		FulfillmentLocationID: shipment.FulfillmentLocationID,
		Status:                string(shipment.Status),
		TrackingNumber:        shipment.TrackingNumber,
		ShippingCostMinor:     shipment.ShippingCostMinor,
		CodAmountMinor:        shipment.CodAmountMinor,
		Currency:              shipment.Currency,
		Items:                 itemPayloads,
		CreatedAt:             now,
	}

	eventEnv, err := pkgEvents.NewShipmentCreatedEvent(outboxPayload, params.CorrelationID, params.CausationID)
	if err != nil {
		return nil, fmt.Errorf("build shipment created event: %w", err)
	}

	err = s.repo.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.repo.CreateShipmentTx(ctx, tx, shipment, items, event); err != nil {
			return err
		}
		if err := s.outboxStore.Enqueue(ctx, tx, eventEnv); err != nil {
			return fmt.Errorf("enqueue outbox event: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	shipment.Items = items
	shipment.Events = []ShipmentEvent{event}
	return &shipment, nil
}

func (s Service) UpdateShipmentStatus(ctx context.Context, params UpdateStatusParams) (*Shipment, error) {
	if strings.TrimSpace(params.ShipmentID) == "" {
		return nil, fmt.Errorf("%w: shipment_id is required", ErrInvalidInput)
	}
	if !params.NewStatus.Valid() {
		return nil, fmt.Errorf("%w: invalid target status '%s'", ErrInvalidStatus, params.NewStatus)
	}

	now := time.Now().UTC()
	var (
		updatedShipment *Shipment
		newEvent        *ShipmentEvent
		fromStatus      Status
	)

	err := s.repo.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		updatedShipment, newEvent, fromStatus, err = s.repo.UpdateShipmentStatusTx(
			ctx, tx, params.ShipmentID, params.NewStatus, params.TrackingNumber, params.Notes, now,
		)
		if err != nil {
			return err
		}

		outboxPayload := pkgEvents.ShipmentStatusChangedPayload{
			ShipmentID:     updatedShipment.ID,
			OrderID:        updatedShipment.OrderID,
			FromStatus:     string(fromStatus),
			ToStatus:       string(updatedShipment.Status),
			TrackingNumber: updatedShipment.TrackingNumber,
			Notes:          params.Notes,
			OccurredAt:     now,
		}

		eventEnv, err := pkgEvents.NewShipmentStatusChangedEvent(outboxPayload, params.CorrelationID, params.CausationID)
		if err != nil {
			return fmt.Errorf("build shipment status changed event: %w", err)
		}

		if err := s.outboxStore.Enqueue(ctx, tx, eventEnv); err != nil {
			return fmt.Errorf("enqueue outbox status changed event: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	fullShipment, err := s.repo.GetShipmentByID(ctx, nil, updatedShipment.ID)
	if err != nil {
		updatedShipment.Events = append(updatedShipment.Events, *newEvent)
		return updatedShipment, nil
	}
	return fullShipment, nil
}

func (s Service) GetShipment(ctx context.Context, shipmentID string) (*Shipment, error) {
	return s.repo.GetShipmentByID(ctx, nil, shipmentID)
}

func (s Service) ListShipmentsForOrder(ctx context.Context, orderID string) ([]Shipment, error) {
	return s.repo.ListShipmentsByOrderID(ctx, nil, orderID)
}
