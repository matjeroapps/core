package shipping

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type DBPool interface {
	DBExecutor
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Shipment struct {
	ID                    string          `json:"id"`
	OrderID               string          `json:"order_id"`
	FulfillmentLocationID string          `json:"fulfillment_location_id"`
	Status                Status          `json:"status"`
	TrackingNumber        string          `json:"tracking_number,omitempty"`
	ShippingCostMinor     int64           `json:"shipping_cost_minor"`
	CodAmountMinor        int64           `json:"cod_amount_minor"`
	Currency              string          `json:"currency"`
	Items                 []ShipmentItem  `json:"items,omitempty"`
	Events                []ShipmentEvent `json:"events,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type ShipmentItem struct {
	ID          string    `json:"id"`
	ShipmentID  string    `json:"shipment_id"`
	OrderItemID string    `json:"order_item_id"`
	Quantity    int64     `json:"quantity"`
	CreatedAt   time.Time `json:"created_at"`
}

type ShipmentEvent struct {
	ID         string    `json:"id"`
	ShipmentID string    `json:"shipment_id"`
	Status     Status    `json:"status"`
	Notes      string    `json:"notes,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type CreateShipmentParams struct {
	OrderID               string
	FulfillmentLocationID string
	TrackingNumber        string
	ShippingCostMinor     int64
	CodAmountMinor        int64
	Currency              string
	Items                 []CreateShipmentItemParams
	CorrelationID         string
	CausationID           string
}

type CreateShipmentItemParams struct {
	OrderItemID string
	Quantity    int64
}

type UpdateStatusParams struct {
	ShipmentID     string
	NewStatus      Status
	TrackingNumber string
	Notes          string
	CorrelationID  string
	CausationID    string
}
