package listings

import (
	"errors"
	"time"
)

var (
	ErrNotFound              = errors.New("listing not found")
	ErrInvalidInput          = errors.New("invalid input")
	ErrUnsafeMargin          = errors.New("unsafe_margin")
	ErrOfferAlreadyImported  = errors.New("offer_already_imported")
	ErrOwnerOverrideRequired = errors.New("only the store owner can approve sub-wholesale pricing")
)

type ListingPricingRequest struct {
	RetailPriceMinorUnits int64  `json:"retail_price_minor_units"`
	AllowSubWholesale     bool   `json:"allow_sub_wholesale"`
	AuditReason           string `json:"audit_reason"`
}

type ListingPricingResponse struct {
	ListingID                  string    `json:"listing_id"`
	StoreID                    string    `json:"store_id"`
	RetailPriceMinorUnits      int64     `json:"retail_price_minor_units"`
	Currency                   string    `json:"currency"`
	WholesalePriceMinorUnits   int64     `json:"wholesale_price_minor_units"`
	MarginPercentage           float64   `json:"margin_percentage"`
	SubWholesaleOverrideActive bool      `json:"sub_wholesale_override_active"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type ListingWholesaleInfo struct {
	ListingID                string
	StoreID                  string
	ProductID                string
	SupplierOfferID          *string
	MarketCode               string
	WholesalePriceMinorUnits int64
	WholesaleCurrency        string
}
