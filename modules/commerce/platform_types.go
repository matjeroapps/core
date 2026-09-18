package commerce

import (
	"time"

	"core/packages/money"
)

type Page struct {
	Limit  int
	Offset int
}

type SupplierCatalogFilter struct {
	MarketCode string
	CategoryID string
	SupplierID string
	Status     string
	Locale     string
	Search     string
	Available  *bool
	MinPrice   *money.Money
	MaxPrice   *money.Money
	Page       Page
}

type SupplierCatalogItem struct {
	OfferID          string       `json:"offer_id"`
	OfferStatus      string       `json:"offer_status"`
	MarketCode       string       `json:"market_code"`
	ProductID        string       `json:"product_id"`
	ProductSlug      string       `json:"product_slug"`
	ProductName      string       `json:"product_name"`
	ProductStatus    string       `json:"product_status"`
	SupplierID       string       `json:"supplier_id"`
	SupplierCode     string       `json:"supplier_code"`
	SupplierName     string       `json:"supplier_name"`
	CategoryID       string       `json:"category_id,omitempty"`
	CategoryName     string       `json:"category_name,omitempty"`
	Price            *money.Money `json:"price,omitempty"`
	IsAvailable      *bool        `json:"is_available,omitempty"`
	AvailableQty     *int64       `json:"available_qty,omitempty"`
	FulfillmentCount int64        `json:"fulfillment_count,omitempty"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

type SupplierLocationSummary struct {
	ID           string    `json:"id"`
	MarketCode   string    `json:"market_code"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	LocationType string    `json:"location_type"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type InventoryMovement struct {
	ID                  string    `json:"id"`
	InventorySnapshotID string    `json:"inventory_snapshot_id"`
	MovementType        string    `json:"movement_type"`
	QuantityDelta       int64     `json:"quantity_delta"`
	OnHandQty           int64     `json:"on_hand_qty"`
	ReservedQty         int64     `json:"reserved_qty"`
	Reason              string    `json:"reason"`
	PrincipalSubject    string    `json:"principal_subject"`
	CorrelationID       string    `json:"correlation_id"`
	CausationID         string    `json:"causation_id"`
	IdempotencyKey      string    `json:"idempotency_key,omitempty"`
	RequestFingerprint  string    `json:"request_fingerprint,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}
