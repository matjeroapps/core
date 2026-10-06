package commerce

import (
	"time"

	"core/packages/money"
)

type Supplier struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SupplierSettings struct {
	SupplierID string         `json:"supplier_id"`
	Settings   map[string]any `json:"settings"`
}

type SupplierMember struct {
	ID               string    `json:"id"`
	SupplierID       string    `json:"supplier_id"`
	PrincipalSubject string    `json:"principal_subject"`
	Role             string    `json:"role"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SupplierMarket struct {
	ID         string         `json:"id"`
	SupplierID string         `json:"supplier_id"`
	MarketCode string         `json:"market_code"`
	Status     string         `json:"status"`
	Settings   map[string]any `json:"settings"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type Seller struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SellerSettings struct {
	SellerID string         `json:"seller_id"`
	Settings map[string]any `json:"settings"`
}

type SellerMember struct {
	ID               string    `json:"id"`
	SellerID         string    `json:"seller_id"`
	PrincipalSubject string    `json:"principal_subject"`
	Role             string    `json:"role"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SupplierSellerAffiliation struct {
	SupplierID string    `json:"supplier_id"`
	SellerID   string    `json:"seller_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type RetailCapabilityDraft struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings"`
}

type Store struct {
	ID         string    `json:"id"`
	SellerID   string    `json:"seller_id"`
	MarketCode string    `json:"market_code"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type StoreDomain struct {
	ID                string     `json:"id"`
	StoreID           string     `json:"store_id"`
	Domain            string     `json:"domain"`
	IsPrimary         bool       `json:"is_primary"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	Status            string     `json:"status"`
	DomainType        string     `json:"domain_type,omitempty"`
	VerificationToken *string    `json:"verification_token,omitempty"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type StoreDomainResponse struct {
	ID                string                   `json:"id"`
	StoreID           string                   `json:"store_id"`
	Domain            string                   `json:"domain"`
	IsPrimary         bool                     `json:"is_primary"`
	VerifiedAt        *time.Time               `json:"verified_at,omitempty"`
	Status            string                   `json:"status"`
	DomainType        string                   `json:"domain_type,omitempty"`
	VerificationToken *string                  `json:"verification_token,omitempty"`
	LastCheckedAt     *time.Time               `json:"last_checked_at,omitempty"`
	Verification      *StoreDomainVerification `json:"verification,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

func (d StoreDomain) ToResponse() StoreDomainResponse {
	resp := StoreDomainResponse{
		ID:                d.ID,
		StoreID:           d.StoreID,
		Domain:            d.Domain,
		IsPrimary:         d.IsPrimary,
		VerifiedAt:        d.VerifiedAt,
		Status:            d.Status,
		DomainType:        d.DomainType,
		VerificationToken: d.VerificationToken,
		LastCheckedAt:     d.LastCheckedAt,
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
	}
	if d.VerificationToken != nil && *d.VerificationToken != "" {
		v := BuildVerificationDetails(d.Domain, *d.VerificationToken)
		resp.Verification = &v
	}
	return resp
}

type StoreDomainAdminResponse struct {
	ID            string     `json:"id"`
	StoreID       string     `json:"store_id"`
	Domain        string     `json:"domain"`
	IsPrimary     bool       `json:"is_primary"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	Status        string     `json:"status"`
	DomainType    string     `json:"domain_type,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (d StoreDomain) ToAdminResponse() StoreDomainAdminResponse {
	return StoreDomainAdminResponse{
		ID:            d.ID,
		StoreID:       d.StoreID,
		Domain:        d.Domain,
		IsPrimary:     d.IsPrimary,
		VerifiedAt:    d.VerifiedAt,
		Status:        d.Status,
		DomainType:    d.DomainType,
		LastCheckedAt: d.LastCheckedAt,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
}

type AdminDomainFilter struct {
	StoreID    string
	SellerID   string
	Status     string
	DomainType string
	Search     string
	Page       Page
}

type StoreSettings struct {
	StoreID  string         `json:"store_id"`
	Settings map[string]any `json:"settings"`
}

const (
	StoreCheckoutStatusAccepting = "accepting"
	StoreCheckoutStatusPaused    = "paused"
)

const DefaultCheckoutPausedMessage = "Checkout is temporarily unavailable. Please try again later."

type StoreOperationalState struct {
	StoreID            string     `json:"store_id"`
	CheckoutStatus     string     `json:"checkout_status"`
	MaintenanceMessage string     `json:"maintenance_message"`
	UpdatedBy          string     `json:"updated_by,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
	CheckoutAccepting  bool       `json:"checkout_accepting"`
}

type Product struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProductTranslation struct {
	ProductID       string `json:"product_id"`
	Locale          string `json:"locale"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	MetaTitle       string `json:"meta_title,omitempty"`
	MetaDescription string `json:"meta_description,omitempty"`
}

type Category struct {
	ID               string    `json:"id"`
	ParentCategoryID *string   `json:"parent_category_id,omitempty"`
	Slug             string    `json:"slug"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CategoryTranslation struct {
	CategoryID  string `json:"category_id"`
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Store-scoped categories (seller-managed). Structurally separate from the
// platform-global Category: isolation is enforced by table separation, not by
// query discipline.
const (
	StoreCategoryStatusActive   = "active"
	StoreCategoryStatusInactive = "inactive"
	StoreCategoryStatusArchived = "archived"
)

func IsValidStoreCategoryStatus(status string) bool {
	switch status {
	case StoreCategoryStatusActive, StoreCategoryStatusInactive, StoreCategoryStatusArchived:
		return true
	default:
		return false
	}
}

type StoreCategory struct {
	ID               string    `json:"id"`
	StoreID          string    `json:"store_id"`
	ParentCategoryID *string   `json:"parent_category_id,omitempty"`
	Slug             string    `json:"slug"`
	Status           string    `json:"status"`
	SortOrder        int       `json:"sort_order"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type StoreCategoryTranslation struct {
	CategoryID  string `json:"category_id"`
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// StoreCategoryWithMeta is a list/detail node: the category plus its
// translations and the counts the UI needs for confirmation dialogs
// (descendants and products referencing the node).
type StoreCategoryWithMeta struct {
	StoreCategory
	Translations []StoreCategoryTranslation `json:"translations"`
	ProductCount int                        `json:"product_count"`
	ChildCount   int                        `json:"child_count"`
}

// StoreCategoryRef is a readable product-assignment reference: enough for a
// picker label, never a bare ID. The English name is always present (required
// at creation); NameAr is empty when no Arabic translation exists.
type StoreCategoryRef struct {
	ID     string `json:"id"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
	Name   string `json:"name"`
	NameAr string `json:"name_ar,omitempty"`
}

// StoreCategoryPatch is the update payload. Nil pointers mean "unchanged";
// ClearParent distinguishes "remove parent" from "leave parent alone".
// A non-nil Translations slice replaces all translations.
type StoreCategoryPatch struct {
	Slug             *string
	ParentCategoryID *string
	ClearParent      bool
	SortOrder        *int
	Translations     []StoreCategoryTranslation
}

type StoreCategoryOrder struct {
	ID        string `json:"id"`
	SortOrder int    `json:"sort_order"`
}

type Variant struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type VariantAttributeValue struct {
	VariantID        string    `json:"variant_id"`
	AttributeID      string    `json:"attribute_id"`
	AttributeValueID string    `json:"attribute_value_id"`
	CreatedAt        time.Time `json:"created_at"`
}

type VariantAttributeValueDetail struct {
	VariantID          string `json:"variant_id,omitempty"`
	AttributeID        string `json:"attribute_id"`
	AttributeName      string `json:"attribute_name"`
	AttributeValueID   string `json:"attribute_value_id"`
	AttributeValueName string `json:"attribute_value_name"`
}

type VariantAttributeMapping struct {
	AttributeID      string `json:"attribute_id"`
	AttributeValueID string `json:"attribute_value_id"`
}

type VariantWithDetails struct {
	ID              string                        `json:"id"`
	ProductID       string                        `json:"product_id"`
	Code            string                        `json:"code"`
	Status          string                        `json:"status"`
	AttributeValues []VariantAttributeValueDetail `json:"attribute_values"`
	SKU             *SKU                          `json:"sku,omitempty"`
}

type SKU struct {
	ID              string    `json:"id"`
	VariantID       string    `json:"variant_id"`
	Code            string    `json:"code"`
	Barcode         string    `json:"barcode,omitempty"`
	Status          string    `json:"status"`
	WeightGrams     *int      `json:"weight_grams,omitempty"`
	LengthMM        *int      `json:"length_mm,omitempty"`
	WidthMM         *int      `json:"width_mm,omitempty"`
	HeightMM        *int      `json:"height_mm,omitempty"`
	PriceMinorUnits *int64    `json:"price_minor_units,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Attribute struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AttributeTranslation struct {
	AttributeID string `json:"attribute_id"`
	Locale      string `json:"locale"`
	Name        string `json:"name"`
}

type AttributeValue struct {
	ID          string    `json:"id"`
	AttributeID string    `json:"attribute_id"`
	Code        string    `json:"code"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AttributeValueTranslation struct {
	AttributeValueID string `json:"attribute_value_id"`
	Locale           string `json:"locale"`
	Name             string `json:"name"`
}

type MediaMetadata struct {
	ID         string         `json:"id"`
	ProductID  string         `json:"product_id"`
	MediaType  string         `json:"media_type"`
	URI        string         `json:"uri"`
	AltText    string         `json:"alt_text"`
	SortOrder  int            `json:"sort_order"`
	Metadata   map[string]any `json:"metadata"`
	StorageKey *string        `json:"storage_key,omitempty"`
	IsPrimary  bool           `json:"is_primary"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type SupplierProduct struct {
	ID           string    `json:"id"`
	SupplierID   string    `json:"supplier_id"`
	ProductID    string    `json:"product_id"`
	SupplierCode string    `json:"supplier_code"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SupplierOffer struct {
	ID                string    `json:"id"`
	SupplierID        string    `json:"supplier_id"`
	SupplierProductID string    `json:"supplier_product_id"`
	SupplierMarketID  string    `json:"supplier_market_id"`
	MarketCode        string    `json:"market_code"`
	Status            string    `json:"status"`
	MinimumOrderQty   int64     `json:"minimum_order_quantity"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SupplierOfferPrice struct {
	ID              string      `json:"id"`
	SupplierOfferID string      `json:"supplier_offer_id"`
	Price           money.Money `json:"price"`
	IsCurrent       bool        `json:"is_current"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type SupplierOfferAvailability struct {
	ID              string    `json:"id"`
	SupplierOfferID string    `json:"supplier_offer_id"`
	IsAvailable     bool      `json:"is_available"`
	AvailableQty    *int64    `json:"available_qty,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SellerListing struct {
	ID              string    `json:"id"`
	StoreID         string    `json:"store_id"`
	ProductID       string    `json:"product_id"`
	SupplierOfferID *string   `json:"supplier_offer_id,omitempty"`
	MarketCode      string    `json:"market_code"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SellerListingPrice struct {
	ID              string      `json:"id"`
	SellerListingID string      `json:"seller_listing_id"`
	Price           money.Money `json:"price"`
	IsCurrent       bool        `json:"is_current"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type ListingLifecycleStatus struct {
	ListingID              string       `json:"listing_id"`
	StoreID                string       `json:"store_id"`
	Status                 string       `json:"status"`
	EffectiveAvailability  string       `json:"effective_availability"`
	IsUpstreamAvailable    bool         `json:"is_upstream_available"`
	SupplierOfferID        *string      `json:"supplier_offer_id,omitempty"`
	SupplierOfferStatus    *string      `json:"supplier_offer_status,omitempty"`
	HasMarginWarning       bool         `json:"has_margin_warning"`
	CurrentRetailPrice     *money.Money `json:"current_retail_price,omitempty"`
	UpstreamWholesalePrice *money.Money `json:"upstream_wholesale_price,omitempty"`
	LastSyncedAt           time.Time    `json:"last_synced_at"`
}

type FulfillmentLocation struct {
	SupplierID       string    `json:"supplier_id"`
	StoreID          string    `json:"store_id,omitempty"`
	ID               string    `json:"id"`
	SupplierMarketID string    `json:"supplier_market_id"`
	MarketCode       string    `json:"market_code"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	LocationType     string    `json:"location_type"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type InventorySnapshot struct {
	ID                    string    `json:"id"`
	FulfillmentLocationID string    `json:"fulfillment_location_id"`
	SKUID                 string    `json:"sku_id"`
	OnHandQty             int64     `json:"on_hand_qty"`
	ReservedQty           int64     `json:"reserved_qty"`
	Version               int64     `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type InventoryReservation struct {
	ID                  string     `json:"id"`
	InventorySnapshotID string     `json:"inventory_snapshot_id"`
	Quantity            int64      `json:"quantity"`
	Status              string     `json:"status"`
	ReservationToken    string     `json:"reservation_token"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SellerProduct struct {
	ID         string    `json:"id"`
	SellerID   string    `json:"seller_id"`
	ProductID  string    `json:"product_id"`
	SellerCode *string   `json:"seller_code,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SellerListingPresentation struct {
	SellerListingID  string               `json:"seller_listing_id"`
	SchemaVersion    int                  `json:"schema_version"`
	PurchaseBehavior string               `json:"purchase_behavior"` // inherit, add_to_cart, buy_now
	Sections         []ProductPageSection `json:"sections"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type ProductPageSection struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"` // description, highlights, image_text, specifications, faq, final_cta
	Enabled   bool           `json:"enabled"`
	SortOrder int            `json:"sort_order"`
	Content   map[string]any `json:"content"`
}

type SellerProductDetail struct {
	Product          Product                    `json:"product"`
	Source           string                     `json:"source"` // seller_owned, supplier_backed
	Translations     []ProductTranslation       `json:"translations"`
	Categories       []Category                 `json:"categories"`
	StoreCategories  []StoreCategoryRef         `json:"store_categories"`
	Variants         []Variant                  `json:"variants"`
	SKUs             []SKU                      `json:"skus"`
	Media            []MediaMetadata            `json:"media"`
	Listing          SellerListing              `json:"listing"`
	Price            *SellerListingPrice        `json:"price,omitempty"`
	InventorySummary []SellerInventorySummary   `json:"inventory_summary"`
	Presentation     *SellerListingPresentation `json:"presentation,omitempty"`
	PurchaseBehavior string                     `json:"purchase_behavior"` // effective purchase behavior: add_to_cart, buy_now
	PublishReadiness PublishReadiness           `json:"publish_readiness"`
}

type SellerInventorySummary struct {
	ID                    string `json:"id"`
	FulfillmentLocationID string `json:"fulfillment_location_id"`
	LocationName          string `json:"location_name"`
	SKUID                 string `json:"sku_id"`
	OnHandQty             int64  `json:"on_hand_qty"`
	ReservedQty           int64  `json:"reserved_qty"`
	AvailableQty          int64  `json:"available_qty"`
	Version               int64  `json:"version"`
}

// SellerInventoryAggregate is the product-level inventory projection exposed
// at the internal Seller API boundary: totals plus one entry per location.
type SellerInventoryAggregate struct {
	TotalOnHand    int64                     `json:"total_on_hand"`
	TotalReserved  int64                     `json:"total_reserved"`
	TotalAvailable int64                     `json:"total_available"`
	Locations      []SellerInventoryLocation `json:"locations"`
}

type SellerInventoryLocation struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	SKUID        string `json:"sku_id"`
	OnHandQty    int64  `json:"on_hand_qty"`
	ReservedQty  int64  `json:"reserved_qty"`
	AvailableQty int64  `json:"available_qty"`
}

// SellerProductListView is the product list projection exposed at the
// internal Seller API boundary, carrying everything a Seller dashboard row
// needs without re-fetching the full product detail.
type SellerProductListView struct {
	Product          Product                  `json:"product"`
	Source           string                   `json:"source"`
	Name             string                   `json:"name"`
	ListingID        string                   `json:"listing_id"`
	ListingStatus    string                   `json:"listing_status"`
	CurrentPrice     *SellerListingPrice      `json:"current_price,omitempty"`
	InventorySummary SellerInventoryAggregate `json:"inventory_summary"`
	PublishReadiness PublishReadiness         `json:"publish_readiness"`
}

type PublishReadiness struct {
	IsReady bool     `json:"is_ready"`
	Reasons []string `json:"reasons,omitempty"`
}

type SupplierPublication struct {
	ProductID       string           `json:"product_id"`
	SupplierProduct SupplierProduct  `json:"supplier_product"`
	Readiness       PublishReadiness `json:"readiness"`
	Status          string           `json:"status"`
	PublishedOffers []SupplierOffer  `json:"published_offers,omitempty"`
}

type MediaUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type MediaUploadResponse struct {
	UploadURL   string            `json:"upload_url"`
	StorageKey  string            `json:"storage_key"`
	UploadToken string            `json:"upload_token"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type CompleteMediaUploadRequest struct {
	StorageKey  string `json:"storage_key"`
	UploadToken string `json:"upload_token"`
	AltText     string `json:"alt_text"`
	SortOrder   int    `json:"sort_order"`
	IsPrimary   bool   `json:"is_primary"`
}

type SellerProductDraft struct {
	Slug             string               `json:"slug"`
	Translations     []ProductTranslation `json:"translations"`
	CategoryIDs      []string             `json:"category_ids"`
	StoreCategoryIDs []string             `json:"store_category_ids"`
}

// MediaUploadIntent tracks a server-side scoped presign request.
// TokenDigest and seller/store fields are never serialized.
type MediaUploadIntent struct {
	ID                 string     `json:"id"`
	SellerID           string     `json:"-"`
	StoreID            string     `json:"-"`
	ProductID          *string    `json:"product_id,omitempty"`
	ClientUploadID     *string    `json:"client_upload_id,omitempty"`
	RequestFingerprint *string    `json:"request_fingerprint,omitempty"`
	ChecksumSHA256     string     `json:"checksum_sha256,omitempty"`
	ByteSize           int64      `json:"byte_size,omitempty"`
	OriginalFilename   string     `json:"original_filename,omitempty"`
	StorageKey         string     `json:"storage_key"`
	ContentType        string     `json:"content_type"`
	MaxBytes           int64      `json:"max_bytes"`
	TokenDigest        string     `json:"-"`
	ExpiresAt          time.Time  `json:"expires_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type SupplierOfferImportParams struct {
	MarkupPercentage      *float64 `json:"markup_percentage,omitempty"`
	RetailPriceMinorUnits *int64   `json:"retail_price_minor_units,omitempty"`
	ShippingSubsidyPolicy string   `json:"shipping_subsidy_policy,omitempty"`
}

type ImportedOfferResult struct {
	ID                       string    `json:"id"`
	ListingID                string    `json:"listing_id"`
	StoreID                  string    `json:"store_id"`
	ProductID                string    `json:"product_id"`
	SupplierOfferID          string    `json:"supplier_offer_id"`
	RetailPriceMinorUnits    int64     `json:"retail_price_minor_units"`
	WholesalePriceMinorUnits int64     `json:"wholesale_price_minor_units"`
	Currency                 string    `json:"currency"`
	MarginPercentage         float64   `json:"margin_percentage"`
	Status                   string    `json:"status"`
	CreatedAt                time.Time `json:"created_at"`
}
