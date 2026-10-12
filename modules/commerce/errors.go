package commerce

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound                   = errors.New("commerce entity not found")
	ErrConflict                   = errors.New("commerce conflict")
	ErrMarketMismatch             = errors.New("market mismatch")
	ErrInsufficientInventory      = errors.New("insufficient inventory")
	ErrInvalidInput               = errors.New("invalid input")
	ErrUnavailable                = errors.New("service unavailable")
	ErrCheckoutExpired            = errors.New("checkout expired")
	ErrCheckoutPaused             = errors.New("checkout paused")
	ErrIdempotencyConflict        = errors.New("idempotency conflict")
	ErrCheckoutCartInvariant      = errors.New("checkout cart status invariant")
	ErrInvalidTransition          = errors.New("invalid order transition")
	ErrPriceChanged               = errors.New("price changed")
	ErrListingUnavailable         = errors.New("listing unavailable")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrForbidden                  = errors.New("forbidden")
	ErrStoreEntitlementExceeded   = errors.New("active-store entitlement limit exceeded")
	ErrMerchantStoreAlreadyExists = errors.New("merchant store already exists")
	ErrPublishNotReady            = errors.New("publish readiness failed")
	ErrOfferUnavailable           = errors.New("supplier offer unavailable")
	ErrResourceInUse              = errors.New("resource in use")
	ErrUploadInProgress           = errors.New("upload in progress")
	ErrChecksumMismatch           = errors.New("checksum mismatch")
	ErrMediaInUse                 = errors.New("media in use")
	ErrStoreMismatch              = errors.New("store mismatch")
	ErrInternalError              = errors.New("internal error")
	ErrOfferAlreadyImported       = errors.New("offer_already_imported")
	ErrUnsafeMargin               = errors.New("unsafe_margin")
	ErrAILimitExceeded            = errors.New("ai authoring image limit exceeded")
	ErrAIJobConflict              = errors.New("ai authoring job conflict")
	ErrAIJobNotRetryable          = errors.New("ai authoring job not retryable")
	ErrUnsupportedAuthoringField  = errors.New("unsupported authoring field")

	// Per-item variant creation conflicts. Both wrap ErrConflict so existing
	// coarse conflict behavior keeps working, while the conflicting code can be
	// reported as an actionable per-item batch error detail.
	ErrVariantCodeConflict = fmt.Errorf("%w: variant code already used in this product", ErrConflict)
	ErrSKUCodeConflict     = fmt.Errorf("%w: SKU code already used in this store catalog", ErrConflict)
)
