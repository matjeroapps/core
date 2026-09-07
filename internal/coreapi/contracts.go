package coreapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/contracts"
	"github.com/matjeroapps/core/modules/themes"
)

// Internal request and response contracts.
//
// These DTOs are the wire shape of the Core internal API only. They are not the
// actor APIs' public contracts: each actor owns its own public DTOs and maps
// these responses onto them. Core domain structs are serialized directly where
// the actor already exposes them verbatim, so no translation layer can drift.

// CollectionResponse is the standard list envelope.
type CollectionResponse[T any] = contracts.CollectionResponse[T]

// StatusResponse is the standard response for status mutations.
type StatusResponse = contracts.StatusResponse

// maxPageLimit bounds every internal collection so a misbehaving caller cannot
// ask Core to materialize an unbounded result set.
const maxPageLimit = 100

// parsePage reads limit/offset query parameters using the same defaults and
// clamps the actor APIs already apply, so pagination behaviour is unchanged by
// the move from a Go call to an HTTP call.
func parsePage(r *http.Request) commerce.Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > maxPageLimit {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	return commerce.Page{Limit: limit, Offset: offset}
}

// --- Sellers ---

// SellerResolveResponse returns the seller identity Core resolved from a
// forwarded subject. Core performs the resolution itself so a caller can never
// assert its own business identity.
type SellerResolveResponse struct {
	SellerID string `json:"seller_id"`
}

// SellerProfileResponse is the seller profile and its settings.
type SellerProfileResponse struct {
	Seller   commerce.Seller `json:"seller"`
	Settings map[string]any  `json:"settings"`
}

// ProfileUpdateRequest is the shared seller/supplier profile mutation payload.
type ProfileUpdateRequest struct {
	Name     string         `json:"name"`
	Status   string         `json:"status"`
	Settings map[string]any `json:"settings"`
}

// StoreCreateRequest creates a store for the authenticated seller.
type StoreCreateRequest struct {
	MarketCode string         `json:"market_code"`
	Code       string         `json:"code"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Settings   map[string]any `json:"settings"`
}

// CustomDomainRequest registers a custom domain for a store.
type CustomDomainRequest struct {
	Domain string `json:"domain"`
}

// --- Suppliers ---

// SupplierResolveResponse returns the supplier identity Core resolved from a
// forwarded subject.
type SupplierResolveResponse struct {
	SupplierID string `json:"supplier_id"`
}

// SupplierProfileResponse is the supplier profile and its settings.
type SupplierProfileResponse struct {
	Supplier commerce.Supplier `json:"supplier"`
	Settings map[string]any    `json:"settings"`
}

// FulfillmentLocationCreateRequest registers a fulfillment location.
type FulfillmentLocationCreateRequest struct {
	SupplierMarketID string `json:"supplier_market_id"`
	MarketCode       string `json:"market_code"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	LocationType     string `json:"location_type"`
	Status           string `json:"status"`
}

// StoreFulfillmentLocationCreateRequest creates Store-owned inventory space;
// source ownership is derived from the authorized Store path.
type StoreFulfillmentLocationCreateRequest struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	LocationType string `json:"location_type"`
	Status       string `json:"status"`
}

type CartCreateRequest struct{}

type CartAddItemRequest struct {
	SKUID    string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
}

// CartLineResponse intentionally omits seller/source identity. The resolved
// Listing is Core-owned persistence state, not public/browser authority.
type CartLineResponse struct {
	ID                     string `json:"id"`
	SKUID                  string `json:"sku_id"`
	Quantity               int64  `json:"quantity"`
	ExpectedUnitPriceMinor int64  `json:"unit_price_minor"`
	ExpectedCurrencyCode   string `json:"currency_code"`
}

type CartResponse struct {
	ID         string             `json:"id"`
	Status     string             `json:"status"`
	MarketCode string             `json:"market_code"`
	CartToken  string             `json:"cart_token,omitempty"`
	Items      []CartLineResponse `json:"items"`
}

type ShippingAddressRequest struct {
	RecipientName string  `json:"recipient_name"`
	AddressLine1  string  `json:"address_line_1"`
	AddressLine2  *string `json:"address_line_2,omitempty"`
	City          string  `json:"city"`
	Region        *string `json:"region,omitempty"`
	PostalCode    *string `json:"postal_code,omitempty"`
	CountryCode   string  `json:"country_code"`
}

type CheckoutFinalizeRequest struct {
	ShippingAddress ShippingAddressRequest `json:"shipping_address"`
	ContactEmail    string                 `json:"contact_email"`
}

type CheckoutSessionResponse struct {
	ID                    string    `json:"id"`
	CartID                string    `json:"cart_id"`
	Status                string    `json:"status"`
	ExpiresAt             time.Time `json:"expires_at"`
	CustomerID            *string   `json:"customer_id,omitempty"`
	GuestOrderAccessToken string    `json:"guest_order_access_token"`
}

type CheckoutDecisionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Replay    bool   `json:"replay"`
}

// TranslationInput is a localized product name/description.
type TranslationInput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProductCreateRequest creates a product and binds it to the supplier.
type ProductCreateRequest struct {
	Slug         string             `json:"slug"`
	Status       string             `json:"status"`
	SupplierCode string             `json:"supplier_code"`
	Translations []TranslationInput `json:"translations"`
	CategoryIDs  []string           `json:"category_ids"`
}

// ProductCreateResponse returns both the global product and the supplier binding.
type ProductCreateResponse struct {
	Product         commerce.Product         `json:"product"`
	SupplierProduct commerce.SupplierProduct `json:"supplier_product"`
}

// ProductCategoriesRequest replaces a product's category assignments.
type ProductCategoriesRequest struct {
	CategoryIDs []string `json:"category_ids"`
}

// ProductCategoriesResponse echoes the applied category identifiers.
type ProductCategoriesResponse struct {
	CategoryIDs []string `json:"category_ids"`
}

// SupplierOfferCreateRequest creates a supplier offer and optionally seeds its
// price and availability.
type SupplierOfferCreateRequest struct {
	SupplierProductID string       `json:"supplier_product_id"`
	SupplierMarketID  string       `json:"supplier_market_id"`
	MarketCode        string       `json:"market_code"`
	Status            string       `json:"status"`
	Price             *moneyAmount `json:"price"`
	IsAvailable       *bool        `json:"is_available"`
	AvailableQty      *int64       `json:"available_qty"`
}

// moneyAmount is the minor-unit/currency pair used for price mutations.
type moneyAmount struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// InventorySnapshotCreateRequest opens an inventory snapshot.
type InventorySnapshotCreateRequest struct {
	FulfillmentLocationID string `json:"fulfillment_location_id"`
	SKUID                 string `json:"sku_id"`
	OnHandQty             int64  `json:"on_hand_qty"`
}

// InventoryAdjustmentRequest applies a stock movement to a snapshot.
type InventoryAdjustmentRequest struct {
	QuantityDelta int64  `json:"quantity_delta"`
	MovementType  string `json:"movement_type"`
	Reason        string `json:"reason"`
}

// InventoryAdjustmentResponse returns the updated snapshot and the movement.
type InventoryAdjustmentResponse struct {
	Snapshot commerce.InventorySnapshot `json:"snapshot"`
	Movement commerce.InventoryMovement `json:"movement"`
}

// SupplierRetailCapabilityRequest creates a seller profile for a supplier.
type SupplierRetailCapabilityRequest struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings"`
}

// SupplierRetailCapabilityResponse details the supplier's explicit retail link and seller profile.
type SupplierRetailCapabilityResponse struct {
	Affiliation commerce.SupplierSellerAffiliation `json:"affiliation"`
	Seller      commerce.Seller                    `json:"seller"`
}

// --- Seller listings ---

// SellerListingImportRequest imports a supplier offer into a seller's store.
type SellerListingImportRequest struct {
	StoreID         string  `json:"store_id"`
	ProductID       string  `json:"product_id"`
	SupplierOfferID *string `json:"supplier_offer_id"`
	Status          string  `json:"status"`
	MarketCode      string  `json:"market_code"`
}

// PriceUpdateRequest sets a listing or offer price in minor units.
type PriceUpdateRequest struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// --- Admin ---

// OverviewResponse carries the platform aggregate counts.
type OverviewResponse struct {
	Counts map[string]int `json:"counts"`
}

// --- Themes ---

// ThemeInstallRequest binds a store to a theme version.
type ThemeInstallRequest struct {
	ThemeKey string `json:"theme_key"`
	Version  string `json:"version,omitempty"`
}

// ThemeInstallationResponse is the installation plus its configuration state.
type ThemeInstallationResponse struct {
	Installation      themes.ThemeInstallation `json:"installation"`
	DraftConfig       map[string]any           `json:"draft_config,omitempty"`
	PublishedConfig   map[string]any           `json:"published_config,omitempty"`
	DraftRevision     int                      `json:"draft_revision"`
	PublishedRevision int                      `json:"published_revision"`
}

// ThemeDraftResponse is the draft configuration and its revision.
type ThemeDraftResponse struct {
	Config   map[string]any `json:"config"`
	Revision int            `json:"revision"`
}

// ThemePublishResponse reports the newly published revision.
type ThemePublishResponse struct {
	PublishedRevision int `json:"published_revision"`
}

// ThemePreviewResponse carries a signed, short-lived preview token.
type ThemePreviewResponse struct {
	Token string `json:"token"`
}

// ThemeConfigRequest replaces a store's draft theme configuration.
type ThemeConfigRequest struct {
	Config map[string]any `json:"config"`
}

// ThemeUpgradeRequest points an installation at a newer published version.
type ThemeUpgradeRequest struct {
	Version string `json:"version"`
}

// --- Shipping ---

type CreateShipmentRequest struct {
	FulfillmentLocationID string                      `json:"fulfillment_location_id"`
	TrackingNumber        string                      `json:"tracking_number,omitempty"`
	ShippingCostMinor     int64                       `json:"shipping_cost_minor"`
	CodAmountMinor        int64                       `json:"cod_amount_minor"`
	Currency              string                      `json:"currency"`
	Items                 []CreateShipmentItemRequest `json:"items"`
}

type CreateShipmentItemRequest struct {
	OrderItemID string `json:"order_item_id"`
	Quantity    int64  `json:"quantity"`
}

type UpdateShipmentStatusRequest struct {
	Status         string `json:"status"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type ShipmentItemResponse struct {
	ID          string    `json:"id"`
	ShipmentID  string    `json:"shipment_id"`
	OrderItemID string    `json:"order_item_id"`
	Quantity    int64     `json:"quantity"`
	CreatedAt   time.Time `json:"created_at"`
}

type ShipmentEventResponse struct {
	ID         string    `json:"id"`
	ShipmentID string    `json:"shipment_id"`
	Status     string    `json:"status"`
	Notes      string    `json:"notes,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ShipmentResponse struct {
	ID                    string                  `json:"id"`
	OrderID               string                  `json:"order_id"`
	FulfillmentLocationID string                  `json:"fulfillment_location_id"`
	Status                string                  `json:"status"`
	TrackingNumber        string                  `json:"tracking_number,omitempty"`
	ShippingCostMinor     int64                   `json:"shipping_cost_minor"`
	CodAmountMinor        int64                   `json:"cod_amount_minor"`
	Currency              string                  `json:"currency"`
	Items                 []ShipmentItemResponse  `json:"items"`
	Events                []ShipmentEventResponse `json:"events,omitempty"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
}
