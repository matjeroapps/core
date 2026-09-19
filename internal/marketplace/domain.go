package marketplace

import (
	"time"

	"core/packages/i18n"
)

type CollectionType string

const (
	CollectionBestSellers    CollectionType = "best_sellers"
	CollectionNewProducts    CollectionType = "new_products"
	CollectionUniqueProducts CollectionType = "unique_products"
	CollectionOffers         CollectionType = "offers"
	CollectionFastDelivery   CollectionType = "fast_delivery"
	CollectionTrending       CollectionType = "trending"
)

func (c CollectionType) Valid() bool {
	switch c {
	case CollectionBestSellers, CollectionNewProducts, CollectionUniqueProducts,
		CollectionOffers, CollectionFastDelivery, CollectionTrending:
		return true
	default:
		return false
	}
}

func (c CollectionType) Implemented() bool {
	switch c {
	case CollectionBestSellers, CollectionNewProducts, CollectionUniqueProducts, CollectionFastDelivery:
		return true
	default:
		return false
	}
}

type PageRequest struct {
	MarketCode string
	Locale     i18n.Locale
	Limit      int
	Cursor     string
}

type Cursor struct {
	CreatedAt       time.Time `json:"created_at,omitempty"`
	DeliveredQty    int64     `json:"delivered_qty,omitempty"`
	SellerListingID string    `json:"seller_listing_id"`
}

type Price struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type Item struct {
	ListingID       string    `json:"listing_id"`
	ProductID       string    `json:"product_id"`
	StoreID         string    `json:"store_id"`
	StoreName       string    `json:"store_name"`
	Title           string    `json:"title"`
	Slug            string    `json:"slug"`
	Price           Price     `json:"price"`
	PrimaryImageURI string    `json:"primary_image_uri,omitempty"`
	Badges          []string  `json:"badges"`
	Availability    string    `json:"availability"`
	DeliveredQty    int64     `json:"delivered_quantity,omitempty"`
	CreatedAt       time.Time `json:"-"`
}

type Collection struct {
	CollectionType CollectionType `json:"collection_type"`
	MarketCode     string         `json:"market_code"`
	Items          []Item         `json:"items"`
	NextCursor     string         `json:"next_cursor,omitempty"`
}

const (
	AvailabilityInStock  = "IN_STOCK"
	AvailabilityOutStock = "OUT_OF_STOCK"
	BadgeBestSeller      = "BEST_SELLER"
	BadgeNew             = "NEW"
	BadgeUnique          = "UNIQUE"
	BadgeFastDelivery    = "FAST_DELIVERY"
)
