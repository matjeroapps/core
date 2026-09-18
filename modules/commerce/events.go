package commerce

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"core/packages/events"
	"core/packages/money"
)

const (
	EventTypeSupplierUpserted      = "commerce.supplier.upserted.v1"
	EventTypeSellerUpserted        = "commerce.seller.upserted.v1"
	EventTypeStoreUpserted         = "commerce.store.upserted.v1"
	EventTypeProductUpserted       = "commerce.product.upserted.v1"
	EventTypeCategoryUpserted      = "commerce.category.upserted.v1"
	EventTypeAttributeUpserted     = "commerce.attribute.upserted.v1"
	EventTypeVariantUpserted       = "commerce.variant.upserted.v1"
	EventTypeSKUUpserted           = "commerce.sku.upserted.v1"
	EventTypeSupplierOfferUpserted = "commerce.supplier_offer.upserted.v1"
	EventTypeSellerListingUpserted = "commerce.seller_listing.upserted.v1"
	EventTypeOrderStatusChanged    = "commerce.order.status_changed.v1"
	EventTypeOrderCreated          = "commerce.order.created.v1"
)

type SearchTranslation struct {
	Locale      string `json:"locale"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type SupplierSearchPayload struct {
	ID           string              `json:"id"`
	Code         string              `json:"code"`
	Name         string              `json:"name"`
	Status       string              `json:"status"`
	MarketCodes  []string            `json:"market_codes,omitempty"`
	Translations []SearchTranslation `json:"translations,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type SellerSearchPayload struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StoreSearchPayload struct {
	ID           string              `json:"id"`
	SellerID     string              `json:"seller_id"`
	MarketCode   string              `json:"market_code"`
	Code         string              `json:"code"`
	Name         string              `json:"name"`
	Status       string              `json:"status"`
	Domains      []string            `json:"domains,omitempty"`
	Translations []SearchTranslation `json:"translations,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type ProductSearchPayload struct {
	ID           string              `json:"id"`
	Slug         string              `json:"slug"`
	Status       string              `json:"status"`
	CategoryIDs  []string            `json:"category_ids,omitempty"`
	AttributeIDs []string            `json:"attribute_ids,omitempty"`
	Translations []SearchTranslation `json:"translations,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type CategorySearchPayload struct {
	ID               string              `json:"id"`
	ParentCategoryID *string             `json:"parent_category_id,omitempty"`
	Slug             string              `json:"slug"`
	Status           string              `json:"status"`
	Translations     []SearchTranslation `json:"translations,omitempty"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type AttributeSearchPayload struct {
	ID           string              `json:"id"`
	Code         string              `json:"code"`
	Status       string              `json:"status"`
	Translations []SearchTranslation `json:"translations,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type VariantSearchPayload struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	SKUIDs    []string  `json:"sku_ids,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SKUSearchPayload struct {
	ID        string    `json:"id"`
	VariantID string    `json:"variant_id"`
	Code      string    `json:"code"`
	Barcode   string    `json:"barcode,omitempty"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SupplierOfferSearchPayload struct {
	ID                string       `json:"id"`
	SupplierID        string       `json:"supplier_id"`
	SupplierProductID string       `json:"supplier_product_id"`
	SupplierMarketID  string       `json:"supplier_market_id"`
	MarketCode        string       `json:"market_code"`
	ProductID         string       `json:"product_id"`
	SupplierCode      string       `json:"supplier_code,omitempty"`
	Status            string       `json:"status"`
	Price             *money.Money `json:"price,omitempty"`
	IsAvailable       *bool        `json:"is_available,omitempty"`
	AvailableQty      *int64       `json:"available_qty,omitempty"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

type SellerListingSearchPayload struct {
	ID              string       `json:"id"`
	StoreID         string       `json:"store_id"`
	ProductID       string       `json:"product_id"`
	SupplierOfferID *string      `json:"supplier_offer_id,omitempty"`
	MarketCode      string       `json:"market_code"`
	Status          string       `json:"status"`
	Price           *money.Money `json:"price,omitempty"`
	IsAvailable     *bool        `json:"is_available,omitempty"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

func NewSupplierUpsertedEvent(payload SupplierSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeSupplierUpserted, "supplier", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewSellerUpsertedEvent(payload SellerSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeSellerUpserted, "seller", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewStoreUpsertedEvent(payload StoreSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeStoreUpserted, "store", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewProductUpsertedEvent(payload ProductSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeProductUpserted, "product", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewCategoryUpsertedEvent(payload CategorySearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeCategoryUpserted, "category", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewAttributeUpsertedEvent(payload AttributeSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeAttributeUpserted, "attribute", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewVariantUpsertedEvent(payload VariantSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeVariantUpserted, "variant", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewSKUUpsertedEvent(payload SKUSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeSKUUpserted, "sku", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func NewSupplierOfferUpsertedEvent(payload SupplierOfferSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeSupplierOfferUpserted, "supplier_offer", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

type OrderStatusChangedPayload struct {
	OrderID            string    `json:"order_id"`
	OrderNumber        string    `json:"order_number"`
	StoreID            string    `json:"store_id"`
	MarketCode         string    `json:"market_code"`
	CustomerID         *string   `json:"customer_id,omitempty"`
	Status             string    `json:"status"`
	FromStatus         *string   `json:"from_status,omitempty"`
	CurrencyCode       string    `json:"currency_code"`
	SubtotalMinor      int64     `json:"subtotal_minor"`
	TotalMinor         int64     `json:"total_minor"`
	CancellationReason *string   `json:"cancellation_reason,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func NewOrderStatusChangedEvent(order Order, fromStatus string, correlationID, causationID string, decisionNow time.Time) (events.EventEnvelope, error) {
	if order.ID == "" {
		return events.EventEnvelope{}, ErrInvalidInput
	}
	var fromStatusPtr *string
	if fromStatus != "" {
		fromStatusPtr = &fromStatus
	}
	payload := OrderStatusChangedPayload{
		OrderID:            order.ID,
		OrderNumber:        order.OrderNumber,
		StoreID:            order.StoreID,
		MarketCode:         order.MarketCode,
		CustomerID:         order.CustomerID,
		Status:             order.Status,
		FromStatus:         fromStatusPtr,
		CurrencyCode:       order.CurrencyCode,
		SubtotalMinor:      order.SubtotalMinor,
		TotalMinor:         order.TotalMinor,
		CancellationReason: order.CancellationReason,
		UpdatedAt:          decisionNow,
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return events.EventEnvelope{}, err
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeOrderStatusChanged,
		SchemaVersion:    1,
		AggregateType:    "order",
		AggregateID:      order.ID,
		AggregateVersion: order.AggregateVersion,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       decisionNow,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return events.EventEnvelope{}, fmt.Errorf("validate order status changed event: %w", err)
	}

	return envelope, nil
}

type OrderCreatedItemPayload struct {
	ID                   string    `json:"id"`
	OrderID              string    `json:"order_id"`
	SellerListingID      *string   `json:"seller_listing_id,omitempty"`
	ProductID            *string   `json:"product_id,omitempty"`
	VariantID            *string   `json:"variant_id,omitempty"`
	SKUID                *string   `json:"sku_id,omitempty"`
	ProductTitleSnapshot string    `json:"product_title_snapshot"`
	SKUCodeSnapshot      string    `json:"sku_code_snapshot"`
	UnitPriceMinor       int64     `json:"unit_price_minor"`
	CurrencyCode         string    `json:"currency_code"`
	Quantity             int64     `json:"quantity"`
	LineTotalMinor       int64     `json:"line_total_minor"`
	CreatedAt            time.Time `json:"created_at"`
}

type OrderCreatedPayload struct {
	OrderID                string                    `json:"order_id"`
	OrderNumber            string                    `json:"order_number"`
	StoreID                string                    `json:"store_id"`
	MarketCode             string                    `json:"market_code"`
	CustomerID             *string                   `json:"customer_id,omitempty"`
	CheckoutSessionID      string                    `json:"checkout_session_id"`
	Status                 string                    `json:"status"`
	CurrencyCode           string                    `json:"currency_code"`
	SubtotalMinor          int64                     `json:"subtotal_minor"`
	TotalMinor             int64                     `json:"total_minor"`
	ConfirmationDeadlineAt time.Time                 `json:"confirmation_deadline_at"`
	CreatedAt              time.Time                 `json:"created_at"`
	Items                  []OrderCreatedItemPayload `json:"items"`
}

func NewOrderCreatedEvent(order Order, correlationID, causationID string, occurredAt time.Time) (events.EventEnvelope, error) {
	if order.ID == "" || occurredAt.IsZero() {
		return events.EventEnvelope{}, ErrInvalidInput
	}
	items := make([]OrderCreatedItemPayload, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, OrderCreatedItemPayload{
			ID:                   item.ID,
			OrderID:              item.OrderID,
			SellerListingID:      item.SellerListingID,
			ProductID:            item.ProductID,
			VariantID:            item.VariantID,
			SKUID:                item.SKUID,
			ProductTitleSnapshot: item.ProductTitleSnapshot,
			SKUCodeSnapshot:      item.SKUCodeSnapshot,
			UnitPriceMinor:       item.UnitPriceMinor,
			CurrencyCode:         item.CurrencyCode,
			Quantity:             item.Quantity,
			LineTotalMinor:       item.LineTotalMinor,
			CreatedAt:            item.CreatedAt,
		})
	}
	payload := OrderCreatedPayload{
		OrderID:                order.ID,
		OrderNumber:            order.OrderNumber,
		StoreID:                order.StoreID,
		MarketCode:             order.MarketCode,
		CustomerID:             order.CustomerID,
		CheckoutSessionID:      order.CheckoutSessionID,
		Status:                 order.Status,
		CurrencyCode:           order.CurrencyCode,
		SubtotalMinor:          order.SubtotalMinor,
		TotalMinor:             order.TotalMinor,
		ConfirmationDeadlineAt: order.ConfirmationDeadlineAt,
		CreatedAt:              order.CreatedAt,
		Items:                  items,
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return events.EventEnvelope{}, err
	}

	aggregateVersion := order.AggregateVersion
	if aggregateVersion == 0 {
		aggregateVersion = 1
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeOrderCreated,
		SchemaVersion:    1,
		AggregateType:    "order",
		AggregateID:      order.ID,
		AggregateVersion: aggregateVersion,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       occurredAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return events.EventEnvelope{}, fmt.Errorf("validate order created event: %w", err)
	}

	return envelope, nil
}

func NewSellerListingUpsertedEvent(payload SellerListingSearchPayload, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return newCommerceEvent(EventTypeSellerListingUpserted, "seller_listing", payload.ID, aggregateVersion, correlationID, causationID, payload)
}

func newCommerceEvent(eventType, aggregateType, aggregateID string, aggregateVersion int64, correlationID, causationID string, payload any) (events.EventEnvelope, error) {
	if aggregateID == "" {
		return events.EventEnvelope{}, ErrInvalidInput
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return events.EventEnvelope{}, err
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        eventType,
		SchemaVersion:    1,
		AggregateType:    aggregateType,
		AggregateID:      aggregateID,
		AggregateVersion: aggregateVersion,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return events.EventEnvelope{}, fmt.Errorf("validate commerce event: %w", err)
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
