package coreapi

import (
	"errors"
	"net/http"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/httpx"
)

// Internal error contract (ADR-017).
//
// The vocabulary below is the complete set of error codes the Core internal API
// can emit. Each code maps to exactly one HTTP status, and actor clients map
// each code onto their own public error contract. The vocabulary is deliberately
// closed: adding a code is a contract change, and no code may ever carry SQL
// text, a stack trace, an internal table name, or a secret value.
const (
	CodeNotFound               = "not_found"
	CodeInvalidArgument        = "invalid_argument"
	CodeValidationError        = "validation_error"
	CodeUnauthorized           = "unauthorized"
	CodeForbidden              = "forbidden"
	CodeConflict               = "conflict"
	CodeMarketMismatch         = "market_mismatch"
	CodeInsufficientInventory  = "insufficient_inventory"
	CodeSchemaMismatch         = "schema_mismatch"
	CodeUnsafeContent          = "unsafe_content"
	CodePreviewUnavailable     = "preview_unavailable"
	CodeStorefrontUnavailable  = "storefront_unavailable"
	CodeUnavailable            = "unavailable"
	CodeCheckoutExpired        = "checkout_expired"
	CodeIdempotencyConflict    = "idempotency_conflict"
	CodeInvalidOrderTransition = "invalid_order_transition"
	CodePriceChanged           = "price_changed"
	CodeListingUnavailable     = "listing_unavailable"
	CodeInternalError          = "internal_error"
)

// statusFor maps an internal error code onto its canonical HTTP status.
func statusFor(code string) int {
	switch code {
	case CodeNotFound, CodeStorefrontUnavailable:
		return http.StatusNotFound
	case CodeInvalidArgument, CodeValidationError, CodeSchemaMismatch, CodeUnsafeContent:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeConflict, CodeMarketMismatch, CodeInsufficientInventory, CodeCheckoutExpired, CodeIdempotencyConflict, CodeInvalidOrderTransition, CodePriceChanged, CodeListingUnavailable:
		return http.StatusConflict
	case CodeUnavailable, CodePreviewUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// ErrorResponse is the internal error envelope. It intentionally reuses the
// platform error shape so actor clients can decode it with the same struct they
// already use for their own errors.
type ErrorResponse = httpx.ErrorResponse

// writeError emits an internal error envelope for a code.
func writeError(w http.ResponseWriter, code string) {
	httpx.WriteError(w, statusFor(code), code, messageFor(code))
}

// messageFor returns a public-safe, non-revealing message for a code. Messages
// never include the underlying cause: a caller that is not entitled to a
// resource learns nothing more than the coarse category.
func messageFor(code string) string {
	switch code {
	case CodeNotFound:
		return "resource not found"
	case CodeInvalidArgument, CodeValidationError:
		return "invalid input"
	case CodeUnauthorized:
		return "unauthorized"
	case CodeForbidden:
		return "forbidden"
	case CodeConflict:
		return "conflict"
	case CodeMarketMismatch:
		return "market mismatch"
	case CodeInsufficientInventory:
		return "insufficient inventory"
	case CodeCheckoutExpired:
		return "checkout session expired"
	case CodeIdempotencyConflict:
		return "checkout request conflicts with the finalized session"
	case CodeInvalidOrderTransition:
		return "invalid order transition"
	case CodePriceChanged:
		return "price changed"
	case CodeListingUnavailable:
		return "listing unavailable"
	case CodeSchemaMismatch:

		return "configuration does not match the theme schema"
	case CodeUnsafeContent:
		return "configuration contains prohibited executable content"
	case CodePreviewUnavailable:
		return "theme preview is not configured"
	case CodeStorefrontUnavailable:
		return "storefront not available"
	case CodeUnavailable:
		return "service unavailable"
	default:
		return "internal error"
	}
}

// writeDomainError maps a Core domain error onto the internal error contract.
//
// The mapping preserves every distinction the actor APIs currently make in their
// public responses, so migrating an actor to the runtime API cannot change its
// public error behaviour. Unknown errors collapse to internal_error rather than
// leaking a cause.
func writeDomainError(w http.ResponseWriter, err error) {
	writeError(w, codeFor(err))
}

func codeFor(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, commerce.ErrNotFound),
		errors.Is(err, markets.ErrNotFound),
		errors.Is(err, themes.ErrNotFound),
		errors.Is(err, storefront.ErrCatalogNotFound):
		return CodeNotFound
	case errors.Is(err, storefront.ErrStoreNotFound),
		errors.Is(err, storefront.ErrDomainInactive),
		errors.Is(err, storefront.ErrStoreInactive):
		// Unknown host, inactive domain and inactive store are indistinguishable
		// to the caller: a customer must not be able to tell an unregistered
		// domain from a suspended store.
		return CodeStorefrontUnavailable
	case errors.Is(err, storefront.ErrInvalidQuery):
		return CodeValidationError
	case errors.Is(err, commerce.ErrInvalidInput),
		errors.Is(err, themes.ErrInvalidInput):
		return CodeValidationError
	case errors.Is(err, commerce.ErrUnauthorized):
		return CodeUnauthorized
	case errors.Is(err, commerce.ErrMarketMismatch):

		return CodeMarketMismatch
	case errors.Is(err, commerce.ErrInsufficientInventory):
		return CodeInsufficientInventory
	case errors.Is(err, commerce.ErrConflict),
		errors.Is(err, themes.ErrConflict),
		errors.Is(err, commerce.ErrCartExpired):
		return CodeConflict
	case errors.Is(err, commerce.ErrCheckoutExpired):
		return CodeCheckoutExpired
	case errors.Is(err, commerce.ErrIdempotencyConflict):
		return CodeIdempotencyConflict
	case errors.Is(err, commerce.ErrInvalidTransition):
		return CodeInvalidOrderTransition
	case errors.Is(err, commerce.ErrPriceChanged):
		return CodePriceChanged
	case errors.Is(err, commerce.ErrListingUnavailable):
		return CodeListingUnavailable
	case errors.Is(err, commerce.ErrUnavailable):
		return CodeUnavailable
	case errors.Is(err, themes.ErrSchemaMismatch):
		return CodeSchemaMismatch
	case errors.Is(err, themes.ErrUnsafeContent):
		return CodeUnsafeContent
	case errors.Is(err, themes.ErrPreviewNotConfigured):
		return CodePreviewUnavailable
	default:
		return CodeInternalError
	}
}
