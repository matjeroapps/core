package commerce

import "errors"

var (
	ErrNotFound              = errors.New("commerce entity not found")
	ErrConflict              = errors.New("commerce conflict")
	ErrMarketMismatch        = errors.New("market mismatch")
	ErrInsufficientInventory = errors.New("insufficient inventory")
	ErrInvalidInput          = errors.New("invalid input")
	ErrUnavailable           = errors.New("service unavailable")
	ErrCheckoutExpired       = errors.New("checkout expired")
	ErrIdempotencyConflict   = errors.New("idempotency conflict")
	ErrCheckoutCartInvariant = errors.New("checkout cart status invariant")
	ErrInvalidTransition     = errors.New("invalid order transition")
	ErrPriceChanged          = errors.New("price changed")
	ErrListingUnavailable    = errors.New("listing unavailable")
	ErrUnauthorized          = errors.New("unauthorized")
	ErrInternalError         = errors.New("internal error")
)
