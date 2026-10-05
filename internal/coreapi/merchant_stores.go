package coreapi

import (
	"errors"
	"net/http"

	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/packages/httpx"
)

func (s *server) handleCreateMerchantStore(w http.ResponseWriter, r *http.Request) {
	if s.deps.MerchantAuthorizer == nil {
		writeError(w, CodeUnavailable)
		return
	}
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	if err := s.deps.MerchantAuthorizer.Authorize(r.Context(), merchantID, subject, merchants.CapabilityTypeRetail, merchants.PermissionRetailStoresManage); err != nil {
		writeMerchantAuthError(w, err)
		return
	}

	var body StoreCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	store, err := s.deps.Commerce.CreateStoreForMerchantSubject(r.Context(), subject, merchantID.String(), body.MarketCode, body.Code, body.Name, body.Status, body.Settings)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, store)
}

func writeMerchantAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, merchants.ErrMerchantNotFound):
		writeError(w, CodeNotFound)
	case errors.Is(err, merchants.ErrMembershipNotFound), errors.Is(err, merchants.ErrPermissionDenied):
		writeError(w, CodeForbidden)
	case errors.Is(err, merchants.ErrInvalidMerchantStatus), errors.Is(err, merchants.ErrCapabilitySuspended), errors.Is(err, merchants.ErrCapabilityNotFound):
		writeError(w, CodeForbidden)
	default:
		writeError(w, CodeForbidden)
	}
}
