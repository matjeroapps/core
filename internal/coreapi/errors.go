package coreapi

import (
	"errors"
	"net/http"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/payments"
	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/internal/shipping"
	"github.com/matjeroapps/core/internal/suppliers"
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
	CodeNotFound                  = "not_found"
	CodeInvalidArgument           = "invalid_argument"
	CodeValidationError           = "validation_error"
	CodeUnauthorized              = "unauthorized"
	CodeForbidden                 = "forbidden"
	CodeConflict                  = "conflict"
	CodeMarketMismatch            = "market_mismatch"
	CodeInsufficientInventory     = "insufficient_inventory"
	CodeSchemaMismatch            = "schema_mismatch"
	CodeUnsafeContent             = "unsafe_content"
	CodePreviewUnavailable        = "preview_unavailable"
	CodeStorefrontUnavailable     = "storefront_unavailable"
	CodeUnavailable               = "unavailable"
	CodeCheckoutExpired           = "checkout_expired"
	CodeIdempotencyConflict       = "idempotency_conflict"
	CodeInvalidOrderTransition    = "invalid_order_transition"
	CodePriceChanged              = "price_changed"
	CodeListingUnavailable        = "listing_unavailable"
	CodeInvalidShipmentTransition = "invalid_shipment_transition"
	CodeInvalidPaymentTransition  = "invalid_payment_transition"
	CodeInternalError             = "internal_error"
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
	case CodeConflict, CodeMarketMismatch, CodeInsufficientInventory, CodeCheckoutExpired, CodeIdempotencyConflict, CodeInvalidOrderTransition, CodeInvalidShipmentTransition, CodeInvalidPaymentTransition, CodePriceChanged, CodeListingUnavailable:
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
	case CodeInvalidShipmentTransition:
		return "invalid shipment transition"
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
		errors.Is(err, storefront.ErrCatalogNotFound),
		errors.Is(err, shipping.ErrShipmentNotFound),
		errors.Is(err, shipping.ErrOrderNotFound),
		errors.Is(err, payments.ErrPaymentNotFound),
		errors.Is(err, payments.ErrOrderNotFound),
		errors.Is(err, finance.ErrAccountNotFound),
		errors.Is(err, finance.ErrJournalEntryNotFound),
		errors.Is(err, balance.ErrAccountNotFound),
		errors.Is(err, balance.ErrBalanceNotFound),
		errors.Is(err, balance.ErrSettlementPeriodNotFound),
		errors.Is(err, settlement.ErrSettlementNotFound),
		errors.Is(err, settlement.ErrSettlementPeriodNotFound),
		errors.Is(err, suppliers.ErrNotFound):
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
		errors.Is(err, themes.ErrInvalidInput),
		errors.Is(err, shipping.ErrInvalidInput),
		errors.Is(err, shipping.ErrInvalidStatus),
		errors.Is(err, payments.ErrInvalidInput),
		errors.Is(err, payments.ErrInvalidStatus),
		errors.Is(err, finance.ErrInvalidAccountType),
		errors.Is(err, finance.ErrInvalidAccountStatus),
		errors.Is(err, finance.ErrUnbalancedEntry),
		errors.Is(err, finance.ErrInvalidLineAmounts),
		errors.Is(err, finance.ErrCurrencyMismatch),
		errors.Is(err, finance.ErrMissingLines),
		errors.Is(err, finance.ErrInvalidReference),
		errors.Is(err, balance.ErrInvalidAccountID),
		errors.Is(err, balance.ErrInvalidCurrency),
		errors.Is(err, balance.ErrInvalidSettlementPeriodDates),
		errors.Is(err, balance.ErrInvalidEventPayload),
		errors.Is(err, balance.ErrNilEventPayload),
		errors.Is(err, settlement.ErrInvalidAccountID),
		errors.Is(err, settlement.ErrInvalidSettlementPeriodID),
		errors.Is(err, suppliers.ErrInvalidInput):
		return CodeValidationError
	case errors.Is(err, commerce.ErrUnauthorized):
		return CodeUnauthorized
	case errors.Is(err, suppliers.ErrForbidden):
		return CodeForbidden
	case errors.Is(err, commerce.ErrMarketMismatch):

		return CodeMarketMismatch
	case errors.Is(err, commerce.ErrInsufficientInventory):
		return CodeInsufficientInventory
	case errors.Is(err, commerce.ErrConflict),
		errors.Is(err, themes.ErrConflict),
		errors.Is(err, commerce.ErrCartExpired),
		errors.Is(err, finance.ErrDuplicateAccountCode),
		errors.Is(err, finance.ErrDuplicatePosting),
		errors.Is(err, finance.ErrInactiveAccount),
		errors.Is(err, balance.ErrSettlementPeriodAlreadyClosed),
		errors.Is(err, settlement.ErrInvalidPeriodState),
		errors.Is(err, settlement.ErrInvalidSettlementState),
		errors.Is(err, settlement.ErrFinalizedSettlementImmutable),
		errors.Is(err, settlement.ErrSettlementAlreadyFinalized),
		errors.Is(err, suppliers.ErrAlreadyExists):
		return CodeConflict
	case errors.Is(err, commerce.ErrCheckoutExpired):
		return CodeCheckoutExpired
	case errors.Is(err, commerce.ErrIdempotencyConflict):
		return CodeIdempotencyConflict
	case errors.Is(err, commerce.ErrInvalidTransition):
		return CodeInvalidOrderTransition
	case errors.Is(err, shipping.ErrInvalidTransition):
		return CodeInvalidShipmentTransition
	case errors.Is(err, payments.ErrInvalidTransition):
		return CodeInvalidPaymentTransition
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
