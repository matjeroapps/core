package coreapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/modules/commerce"
	"core/packages/httpx"
)

func checkoutSessionResponse(session commerce.CheckoutSession, rawCapability string) CheckoutSessionResponse {
	return CheckoutSessionResponse{
		ID: session.ID, CartID: session.CartID, Status: session.Status,
		ExpiresAt: session.ExpiresAt, CustomerID: session.CustomerID,
		GuestOrderAccessToken: rawCapability,
	}
}

func (s *server) handleCreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	session, rawCapability, err := s.deps.Repo.CreateCheckoutSession(
		r.Context(), scope.StoreID(), cartTokenFromRequest(r), nil, s.deps.Commerce.CheckoutSessionLifetime,
	)
	if err != nil {
		s.writeCheckoutError(w, r, scope.StoreID(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, checkoutSessionResponse(session, rawCapability))
}

func (s *server) handleEvaluateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	var body CheckoutFinalizeRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	var attrInput *commerce.MarketplaceAttributionInput
	if body.Attribution != nil {
		attrInput = &commerce.MarketplaceAttributionInput{
			SellerListingID:  body.Attribution.SellerListingID,
			StoreID:          body.Attribution.StoreID,
			MarketCode:       body.Attribution.MarketCode,
			SourceCollection: body.Attribution.SourceCollection,
		}
	}
	request := commerce.FinalizeRequest{
		SessionID: rPathSessionID(r),
		ShippingAddress: commerce.ShippingAddress{
			RecipientName: body.ShippingAddress.RecipientName,
			AddressLine1:  body.ShippingAddress.AddressLine1,
			AddressLine2:  body.ShippingAddress.AddressLine2,
			City:          body.ShippingAddress.City,
			Region:        body.ShippingAddress.Region,
			PostalCode:    body.ShippingAddress.PostalCode,
			CountryCode:   body.ShippingAddress.CountryCode,
		},
		ContactEmail: body.ContactEmail,
		Attribution:  attrInput,
	}
	correlationID := httpx.CorrelationID(r.Context())
	order, err := s.deps.Repo.FinalizeCheckout(r.Context(), scope.StoreID(), request, correlationID)
	if err != nil {
		s.writeCheckoutError(w, r, scope.StoreID(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, order.ToPublic())

}

func (s *server) writeCheckoutError(w http.ResponseWriter, r *http.Request, storeID string, err error) {
	if !errors.Is(err, commerce.ErrCheckoutPaused) {
		writeDomainError(w, err)
		return
	}
	message := commerce.DefaultCheckoutPausedMessage
	if state, stateErr := s.deps.Repo.GetStoreOperationalState(r.Context(), storeID); stateErr == nil && state.MaintenanceMessage != "" {
		message = state.MaintenanceMessage
	}
	httpx.WriteError(w, http.StatusServiceUnavailable, CodeCheckoutPaused, message)
}

func rPathSessionID(r *http.Request) string {
	return chi.URLParam(r, "sessionID")
}
