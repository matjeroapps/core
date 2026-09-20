package commerce

import (
	"encoding/json"
	"time"
)

const (
	OrderStatusPending          = "pending"
	OrderStatusConfirmed        = "confirmed"
	OrderStatusProcessing       = "processing"
	OrderStatusReadyForShipping = "ready_for_shipping"
	OrderStatusShipped          = "shipped"
	OrderStatusOutForDelivery   = "out_for_delivery"
	OrderStatusDelivered        = "delivered"
	OrderStatusCancelled        = "cancelled"
	OrderStatusReturned         = "returned"
)

const DefaultConfirmationDuration = 15 * time.Minute

type TransitionAuthority string

const (
	AuthorityCheckout  TransitionAuthority = "checkout"
	AuthorityCustomer  TransitionAuthority = "customer"
	AuthoritySeller    TransitionAuthority = "seller"
	AuthorityScheduler TransitionAuthority = "scheduler"
	AuthorityAdmin     TransitionAuthority = "admin"
	AuthoritySystem    TransitionAuthority = "system"
)

const (
	AddressTypeShipping = "shipping"
)

const (
	NoteVisibilityInternal = "internal"
)

type MarketplaceAttribution struct {
	ID                string    `json:"id,omitempty"`
	OrderID           string    `json:"order_id,omitempty"`
	CheckoutSessionID string    `json:"checkout_session_id,omitempty"`
	StoreID           string    `json:"store_id"`
	MarketCode        string    `json:"market_code"`
	SellerListingID   string    `json:"seller_listing_id"`
	SourceCollection  *string   `json:"source_collection,omitempty"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
}

type Order struct {
	ID                          string                  `json:"id"`
	OrderNumber                 string                  `json:"order_number"`
	StoreID                     string                  `json:"store_id"`
	MarketCode                  string                  `json:"market_code"`
	CustomerID                  *string                 `json:"customer_id,omitempty"`
	CheckoutSessionID           string                  `json:"checkout_session_id"`
	Status                      string                  `json:"status"`
	CurrencyCode                string                  `json:"currency_code"`
	GuestOrderAccessTokenDigest []byte                  `json:"-"`
	SubtotalMinor               int64                   `json:"subtotal_minor"`
	TotalMinor                  int64                   `json:"total_minor"`
	ConfirmationDeadlineAt      time.Time               `json:"confirmation_deadline_at"`
	CancellationReason          *string                 `json:"cancellation_reason,omitempty"`
	AggregateVersion            int64                   `json:"aggregate_version"`
	CreatedAt                   time.Time               `json:"created_at"`
	UpdatedAt                   time.Time               `json:"updated_at"`
	Items                       []OrderItem             `json:"items,omitempty"`
	Address                     *OrderAddress           `json:"address,omitempty"`
	Attribution                 *MarketplaceAttribution `json:"attribution,omitempty"`
}

type OrderItem struct {
	ID                       string    `json:"id"`
	OrderID                  string    `json:"order_id"`
	SellerListingID          *string   `json:"seller_listing_id,omitempty"`
	ProductID                *string   `json:"product_id,omitempty"`
	VariantID                *string   `json:"variant_id,omitempty"`
	SKUID                    *string   `json:"sku_id,omitempty"`
	SupplierOfferID          *string   `json:"supplier_offer_id,omitempty"`
	SourceSupplierID         *string   `json:"source_supplier_id,omitempty"`
	FulfillmentLocationID    string    `json:"fulfillment_location_id"`
	InventoryReservationID   string    `json:"inventory_reservation_id"`
	ProductTitleSnapshot     string    `json:"product_title_snapshot"`
	SKUCodeSnapshot          string    `json:"sku_code_snapshot"`
	UnitPriceMinor           int64     `json:"unit_price_minor"`
	CurrencyCode             string    `json:"currency_code"`
	Quantity                 int64     `json:"quantity"`
	LineTotalMinor           int64     `json:"line_total_minor"`
	SupplierCostMinor        *int64    `json:"supplier_cost_minor,omitempty"`
	SupplierCostCurrencyCode *string   `json:"supplier_cost_currency_code,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
}

type OrderAddress struct {
	ID            string    `json:"id"`
	OrderID       string    `json:"order_id"`
	AddressType   string    `json:"address_type"`
	RecipientName string    `json:"recipient_name"`
	Phone         *string   `json:"phone,omitempty"`
	AddressLine1  string    `json:"address_line_1"`
	AddressLine2  *string   `json:"address_line_2,omitempty"`
	City          string    `json:"city"`
	Region        *string   `json:"region,omitempty"`
	PostalCode    *string   `json:"postal_code,omitempty"`
	CountryCode   string    `json:"country_code"`
	CreatedAt     time.Time `json:"created_at"`
}

type OrderTimeline struct {
	ID           string          `json:"id"`
	OrderID      string          `json:"order_id"`
	FromStatus   *string         `json:"from_status,omitempty"`
	ToStatus     string          `json:"to_status"`
	ActorType    string          `json:"actor_type"`
	ActorSubject *string         `json:"actor_subject,omitempty"`
	Reason       *string         `json:"reason,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type OrderNote struct {
	ID            string    `json:"id"`
	OrderID       string    `json:"order_id"`
	AuthorSubject string    `json:"author_subject"`
	Visibility    string    `json:"visibility"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"created_at"`
}

type PublicOrderItem struct {
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

type PublicOrder struct {
	ID                     string                  `json:"id"`
	OrderNumber            string                  `json:"order_number"`
	StoreID                string                  `json:"store_id"`
	MarketCode             string                  `json:"market_code"`
	CustomerID             *string                 `json:"customer_id,omitempty"`
	CheckoutSessionID      string                  `json:"checkout_session_id"`
	Status                 string                  `json:"status"`
	CurrencyCode           string                  `json:"currency_code"`
	SubtotalMinor          int64                   `json:"subtotal_minor"`
	TotalMinor             int64                   `json:"total_minor"`
	ConfirmationDeadlineAt time.Time               `json:"confirmation_deadline_at"`
	CancellationReason     *string                 `json:"cancellation_reason,omitempty"`
	AggregateVersion       int64                   `json:"aggregate_version"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
	Items                  []PublicOrderItem       `json:"items,omitempty"`
	Address                *OrderAddress           `json:"address,omitempty"`
	Attribution            *MarketplaceAttribution `json:"attribution,omitempty"`
}

func (o Order) ToPublic() PublicOrder {
	items := make([]PublicOrderItem, 0, len(o.Items))
	for _, item := range o.Items {
		items = append(items, PublicOrderItem{
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
	return PublicOrder{
		ID:                     o.ID,
		OrderNumber:            o.OrderNumber,
		StoreID:                o.StoreID,
		MarketCode:             o.MarketCode,
		CustomerID:             o.CustomerID,
		CheckoutSessionID:      o.CheckoutSessionID,
		Status:                 o.Status,
		CurrencyCode:           o.CurrencyCode,
		SubtotalMinor:          o.SubtotalMinor,
		TotalMinor:             o.TotalMinor,
		ConfirmationDeadlineAt: o.ConfirmationDeadlineAt,
		CancellationReason:     o.CancellationReason,
		AggregateVersion:       o.AggregateVersion,
		CreatedAt:              o.CreatedAt,
		UpdatedAt:              o.UpdatedAt,
		Items:                  items,
		Address:                o.Address,
		Attribution:            o.Attribution,
	}
}

// ValidateOrderTransition validates whether an order can transition from currentStatus to targetStatus
// under the specified authority and at decisionNow wall-clock time.
func ValidateOrderTransition(currentStatus string, authority TransitionAuthority, targetStatus string, confirmationDeadlineAt time.Time, decisionNow time.Time) error {
	if decisionNow.IsZero() {
		return ErrInvalidInput
	}

	switch currentStatus {
	case "":
		if targetStatus == OrderStatusPending && authority == AuthorityCheckout {
			return nil
		}
		return ErrInvalidTransition

	case OrderStatusPending:
		switch targetStatus {
		case OrderStatusConfirmed:
			if authority != AuthoritySeller {
				return ErrInvalidTransition
			}
			// Precondition: confirmation_deadline_at > decision_now
			// At equality or after, confirmation is invalid.
			if !decisionNow.Before(confirmationDeadlineAt) {
				return ErrInvalidTransition
			}
			return nil

		case OrderStatusCancelled:
			if authority == AuthorityCustomer || authority == AuthoritySeller {
				return nil
			}
			if authority == AuthorityScheduler {
				if !decisionNow.Before(confirmationDeadlineAt) {
					return nil
				}
				return ErrInvalidTransition
			}
			return ErrInvalidTransition

		default:
			return ErrInvalidTransition
		}

	case OrderStatusConfirmed:
		switch targetStatus {
		case OrderStatusProcessing:
			if authority == AuthoritySeller {
				return nil
			}
			return ErrInvalidTransition

		case OrderStatusCancelled:
			if authority == AuthoritySeller {
				return nil
			}
			return ErrInvalidTransition

		default:
			return ErrInvalidTransition
		}

	case OrderStatusProcessing:
		switch targetStatus {
		case OrderStatusReadyForShipping:
			if authority == AuthoritySeller {
				return nil
			}
			return ErrInvalidTransition

		case OrderStatusCancelled:
			if authority == AuthoritySeller {
				return nil
			}
			return ErrInvalidTransition

		default:
			return ErrInvalidTransition
		}

	default:
		// All future inactive states (shipped, out_for_delivery, delivered, cancelled, returned) cannot transition in Phase 5.
		return ErrInvalidTransition
	}
}
