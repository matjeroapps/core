package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound              = errors.New("not found")
	ErrInvalidInput          = errors.New("invalid input")
	ErrInsufficientInventory = errors.New("insufficient inventory")
	ErrIdempotencyConflict   = errors.New("idempotency conflict")
)

const (
	ReasonDamaged                  = "damaged"
	ReasonReceivedStock            = "received_stock"
	ReasonCycleCountReconciliation = "cycle_count_reconciliation"
	ReasonTheftLoss                = "theft_loss"
	ReasonCustomerReturnManual     = "customer_return_manual"
	ReasonCorrection               = "correction"
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

type AdjustParams struct {
	StoreID        string
	LocationID     string
	SKUID          string
	QtyDelta       *int64
	TargetQty      *int64
	ReasonCode     string
	Note           string
	Subject        string
	CorrelationID  string
	IdempotencyKey string
}

type AdjustmentResult struct {
	SnapshotID            string    `json:"snapshot_id"`
	FulfillmentLocationID string    `json:"fulfillment_location_id"`
	SKUID                 string    `json:"sku_id"`
	OnHandQty             int64     `json:"on_hand_qty"`
	ReservedQty           int64     `json:"reserved_qty"`
	AvailableQty          int64     `json:"available_qty"`
	QuantityDelta         int64     `json:"quantity_delta"`
	MovementID            string    `json:"movement_id"`
	ReasonCode            string    `json:"reason_code"`
	UpdatedAt             time.Time `json:"updated_at"`
}
