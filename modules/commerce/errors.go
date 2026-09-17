package commerce

import "errors"

var (
	ErrNotFound                 = errors.New("commerce entity not found")
	ErrConflict                 = errors.New("commerce conflict")
	ErrMarketMismatch           = errors.New("market mismatch")
	ErrInsufficientInventory    = errors.New("insufficient inventory")
	ErrInvalidInput             = errors.New("invalid input")
	ErrUnavailable              = errors.New("service unavailable")
	ErrCheckoutExpired          = errors.New("checkout expired")
	ErrIdempotencyConflict      = errors.New("idempotency conflict")
	ErrCheckoutCartInvariant    = errors.New("checkout cart status invariant")
	ErrInvalidTransition        = errors.New("invalid order transition")
	ErrPriceChanged             = errors.New("price changed")
	ErrListingUnavailable       = errors.New("listing unavailable")
	ErrUnauthorized             = errors.New("unauthorized")
	ErrForbidden                = errors.New("forbidden")
	ErrStoreEntitlementExceeded = errors.New("active-store entitlement limit exceeded")
	ErrPublishNotReady          = errors.New("publish readiness failed")
	ErrOfferUnavailable         = errors.New("supplier offer unavailable")
	ErrResourceInUse            = errors.New("resource in use")
	ErrUploadInProgress         = errors.New("upload in progress")
	ErrChecksumMismatch         = errors.New("checksum mismatch")
	ErrMediaInUse               = errors.New("media in use")
	ErrInternalError            = errors.New("internal error")
)
