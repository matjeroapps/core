package marketplace

import "errors"

var (
	ErrInvalidInput          = errors.New("invalid marketplace input")
	ErrInvalidCollectionType = errors.New("invalid marketplace collection type")
	ErrCollectionUnavailable = errors.New("marketplace collection unavailable")
	ErrListingNotFound       = errors.New("marketplace listing not found")
	ErrListingNotPublished   = errors.New("marketplace listing not published")
	ErrProductUnavailable    = errors.New("marketplace product unavailable")
	ErrPriceUnavailable      = errors.New("marketplace price unavailable")
	ErrQuantityInvalid       = errors.New("marketplace quantity invalid")
	ErrInventoryUnavailable  = errors.New("marketplace inventory unavailable")
	ErrNoEligibleLocation    = errors.New("marketplace no eligible fulfillment location")
	ErrCrossMarketAccess     = errors.New("marketplace cross-market access")
	ErrStoreMismatch         = errors.New("marketplace store mismatch")
)
