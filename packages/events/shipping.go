package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeShipmentCreated       = "shipping.shipment.created.v1"
	EventTypeShipmentStatusChanged = "shipping.shipment.status_changed.v1"
)

type ShipmentItemPayload struct {
	ID          string `json:"id"`
	ShipmentID  string `json:"shipment_id"`
	OrderItemID string `json:"order_item_id"`
	Quantity    int64  `json:"quantity"`
}

type ShipmentCreatedPayload struct {
	ShipmentID            string                `json:"shipment_id"`
	OrderID               string                `json:"order_id"`
	FulfillmentLocationID string                `json:"fulfillment_location_id"`
	Status                string                `json:"status"`
	TrackingNumber        string                `json:"tracking_number,omitempty"`
	ShippingCostMinor     int64                 `json:"shipping_cost_minor"`
	CodAmountMinor        int64                 `json:"cod_amount_minor"`
	Currency              string                `json:"currency"`
	Items                 []ShipmentItemPayload `json:"items"`
	CreatedAt             time.Time             `json:"created_at"`
}

type ShipmentStatusChangedPayload struct {
	ShipmentID     string    `json:"shipment_id"`
	OrderID        string    `json:"order_id"`
	FromStatus     string    `json:"from_status"`
	ToStatus       string    `json:"to_status"`
	TrackingNumber string    `json:"tracking_number,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func NewShipmentCreatedEvent(payload ShipmentCreatedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.ShipmentID == "" || payload.OrderID == "" {
		return EventEnvelope{}, fmt.Errorf("invalid shipment created payload")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeShipmentCreated,
		SchemaVersion:    1,
		AggregateType:    "shipment",
		AggregateID:      payload.ShipmentID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.CreatedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate shipment created event: %w", err)
	}

	return envelope, nil
}

func NewShipmentStatusChangedEvent(payload ShipmentStatusChangedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.ShipmentID == "" || payload.OrderID == "" {
		return EventEnvelope{}, fmt.Errorf("invalid shipment status changed payload")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeShipmentStatusChanged,
		SchemaVersion:    1,
		AggregateType:    "shipment",
		AggregateID:      payload.ShipmentID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.OccurredAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate shipment status changed event: %w", err)
	}

	return envelope, nil
}

func payloadToMap(payload any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	out := make(map[string]any)
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return out, nil
}
