package storefront

import (
	"errors"
	"fmt"

	"github.com/matjeroapps/core/packages/i18n"
	"github.com/matjeroapps/core/packages/money"
)

// Public catalog read errors. They are deliberately coarse so actor APIs can
// classify failures without string-matching PostgreSQL messages, and so public
// responses never describe why a record is unavailable.
var (
	// ErrCatalogNotFound means the requested public record does not exist within
	// the resolved tenant scope. A record that exists for another store is
	// reported as not found, never as forbidden.
	ErrCatalogNotFound = errors.New("catalog record not found")
	// ErrInvalidQuery means the caller supplied an unusable filter, sort, or page.
	ErrInvalidQuery = errors.New("invalid catalog query")
)

// Public availability states. Quantities and fulfillment locations are internal
// and never surface in a customer-facing payload.
const (
	AvailabilityInStock    = "in_stock"
	AvailabilityOutOfStock = "out_of_stock"
)

// Public sort options. These are domain-neutral names, not SQL fragments, so a
// future search read model can honor the same contract.
const (
	SortNewest    = "newest"
	SortPriceAsc  = "price_asc"
	SortPriceDesc = "price_desc"
	SortNameAsc   = "name_asc"
)

// Page bounds for public browse. Public collections are always bounded: an
// unbounded storefront query is a denial-of-service vector and a payload-size
// problem.
const (
	DefaultPageLimit = 24
	MaxPageLimit     = 60
)

// Page is an offset page request for public collections.
type Page struct {
	Limit  int
	Offset int
}

func (p Page) normalize() (Page, error) {
	if p.Limit < 0 || p.Offset < 0 {
		return Page{}, fmt.Errorf("%w: page limit and offset must not be negative", ErrInvalidQuery)
	}
	if p.Limit > MaxPageLimit {
		return Page{}, fmt.Errorf("%w: page limit must not exceed %d", ErrInvalidQuery, MaxPageLimit)
	}
	if p.Limit == 0 {
		p.Limit = DefaultPageLimit
	}
	return p, nil
}

// CatalogScope is the tenant boundary applied to every public catalog read. It
// can only be built from a ResolvedStore, so a request can never select a tenant
// by passing a store or seller identifier: tenant identity always originates
// from the trusted request host.
type CatalogScope struct {
	storeID    string
	marketCode string
	domain     string
	locale     i18n.Locale
}

// NewCatalogScope binds a host-resolved store to a validated locale.
func NewCatalogScope(store ResolvedStore, locale i18n.Locale) (CatalogScope, error) {
	if store.Store.ID == "" || store.Store.MarketCode == "" {
		return CatalogScope{}, fmt.Errorf("%w: resolved store is incomplete", ErrInvalidQuery)
	}
	validated, err := validateLocale(locale)
	if err != nil {
		return CatalogScope{}, err
	}
	return CatalogScope{
		storeID:    store.Store.ID,
		marketCode: store.Store.MarketCode,
		domain:     store.StoreDomain.Domain,
		locale:     validated,
	}, nil
}

// Locale reports the validated locale the scope reads translations for.
func (s CatalogScope) Locale() i18n.Locale { return s.locale }

// StoreID reports the host-resolved store bound to this scope. It is read-only
// tenant context for Core-internal integrations and is never serialized into a
// public storefront payload.
func (s CatalogScope) StoreID() string { return s.storeID }

// MarketCode reports the market bound to the host-resolved Store.
func (s CatalogScope) MarketCode() string { return s.marketCode }

func validateLocale(locale i18n.Locale) (i18n.Locale, error) {
	if locale == "" {
		return i18n.Default(), nil
	}
	for _, supported := range i18n.SupportedLocales {
		if locale == supported {
			return locale, nil
		}
	}
	return "", fmt.Errorf("%w: unsupported locale %q", ErrInvalidQuery, string(locale))
}

// fallbackLocale mirrors the market reference-data fallback policy: a missing
// translation falls back to the other supported locale, and only then to a
// non-localized value such as the slug. No new fallback policy is introduced.
func fallbackLocale(locale i18n.Locale) i18n.Locale {
	if locale == i18n.LocaleArabic {
		return i18n.LocaleEnglish
	}
	return i18n.LocaleArabic
}

// PublicCurrency is the customer-facing currency of a store's market.
type PublicCurrency struct {
	Code      string `json:"code"`
	Symbol    string `json:"symbol"`
	MinorUnit int    `json:"minor_unit"`
}

// StoreTheme is the published presentation contract for a storefront. Only
// published theme configuration is exposed here; draft configuration stays
// behind the signed preview-token mechanism.
type StoreTheme struct {
	Key                   string         `json:"key"`
	Version               string         `json:"version"`
	Configuration         map[string]any `json:"configuration"`
	ConfigurationRevision int            `json:"configuration_revision"`
}

// StoreBootstrap is the purpose-built payload a storefront needs before it can
// render anything: public store identity, market/locale/currency context, public
// settings, and the published theme.
type StoreBootstrap struct {
	StoreCode        string         `json:"store_code"`
	StoreName        string         `json:"store_name"`
	Domain           string         `json:"domain,omitempty"`
	Market           string         `json:"market"`
	Currency         PublicCurrency `json:"currency"`
	Timezone         string         `json:"timezone"`
	DefaultLocale    string         `json:"default_locale"`
	SupportedLocales []string       `json:"supported_locales"`
	Settings         map[string]any `json:"settings"`
	Theme            *StoreTheme    `json:"theme,omitempty"`
}

// CategoryNode is a publicly visible category of a store. Categories are global
// commerce records, so a category is only public for a store when that store
// actually sells publicly eligible products in it (or in a descendant).
// ParentSlug lets a client rebuild the tree without extra requests.
type CategoryNode struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ParentSlug   string `json:"parent_slug,omitempty"`
	ProductCount int64  `json:"product_count"`
}

// ProductImage is a customer-facing media reference.
type ProductImage struct {
	URI     string `json:"uri"`
	AltText string `json:"alt_text,omitempty"`
}

// CategoryRef is a minimal category reference embedded in product payloads.
type CategoryRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ProductListItem is a browse-page row. It carries only what a grid renders, so
// collection responses stay small; full detail lives in ProductDetail. Summary is
// a truncated localized description, not the whole document.
type ProductListItem struct {
	Slug         string        `json:"slug"`
	Name         string        `json:"name"`
	Summary      string        `json:"summary,omitempty"`
	Price        money.Money   `json:"price"`
	Image        *ProductImage `json:"image,omitempty"`
	Category     *CategoryRef  `json:"category,omitempty"`
	Availability string        `json:"availability"`
	VariantCount int64         `json:"variant_count"`
}

// ProductPage is a bounded page of browse results.
type ProductPage struct {
	Items  []ProductListItem `json:"items"`
	Total  int64             `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

// PublicSKU is the selectable unit for a future cart. Its ID is an opaque
// identifier deliberately exposed for selection; SKU codes and barcodes stay
// internal because they can encode supplier vocabulary.
type PublicSKU struct {
	ID           string `json:"id"`
	Availability string `json:"availability"`
}

// PublicVariant is a customer-selectable variant of a product.
type PublicVariant struct {
	Code         string      `json:"code"`
	Availability string      `json:"availability"`
	SKUs         []PublicSKU `json:"skus"`
}

// ProductDetail is the product page payload.
type ProductDetail struct {
	Slug             string          `json:"slug"`
	Name             string          `json:"name"`
	Description      string          `json:"description,omitempty"`
	Price            money.Money     `json:"price"`
	Availability     string          `json:"availability"`
	Images           []ProductImage  `json:"images"`
	Categories       []CategoryRef   `json:"categories"`
	Variants         []PublicVariant `json:"variants"`
	PurchaseBehavior string          `json:"purchase_behavior"`
	Sections         []any           `json:"sections"`
}

// ProductQuery is the domain-neutral browse/search request. It intentionally
// exposes no storage-specific concepts (no tsquery, no ILIKE pattern, no SQL
// sort syntax) so the implementation can move to a dedicated search read model
// without changing the customer-facing contract.
//
// Prices are compared in minor units against the store market's currency, so a
// public caller never has to send (or be trusted with) a currency code.
type ProductQuery struct {
	CategorySlug  string
	Keyword       string
	MinPriceMinor *int64
	MaxPriceMinor *int64
	Availability  string
	Sort          string
	Page          Page
}

func (q ProductQuery) normalize() (ProductQuery, error) {
	page, err := q.Page.normalize()
	if err != nil {
		return ProductQuery{}, err
	}
	q.Page = page

	switch q.Sort {
	case "":
		q.Sort = SortNewest
	case SortNewest, SortPriceAsc, SortPriceDesc, SortNameAsc:
	default:
		return ProductQuery{}, fmt.Errorf("%w: unknown sort %q", ErrInvalidQuery, q.Sort)
	}

	switch q.Availability {
	case "", AvailabilityInStock, AvailabilityOutOfStock:
	default:
		return ProductQuery{}, fmt.Errorf("%w: unknown availability %q", ErrInvalidQuery, q.Availability)
	}

	if q.MinPriceMinor != nil && *q.MinPriceMinor < 0 {
		return ProductQuery{}, fmt.Errorf("%w: min price must not be negative", ErrInvalidQuery)
	}
	if q.MaxPriceMinor != nil && *q.MaxPriceMinor < 0 {
		return ProductQuery{}, fmt.Errorf("%w: max price must not be negative", ErrInvalidQuery)
	}
	if q.MinPriceMinor != nil && q.MaxPriceMinor != nil && *q.MinPriceMinor > *q.MaxPriceMinor {
		return ProductQuery{}, fmt.Errorf("%w: min price must not exceed max price", ErrInvalidQuery)
	}

	return q, nil
}
