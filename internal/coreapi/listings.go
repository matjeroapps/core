package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"
	"core/packages/money"
)

// Seller listing handlers.

// handleListSellerListings is the admin moderation listing. It accepts an
// optional store_id filter; an empty filter lists across stores.
func (s *server) handleListSellerListings(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	items, err := s.deps.Repo.ListSellerListings(r.Context(), r.URL.Query().Get("store_id"), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SellerListing]{Items: items})
}

// handleSetSellerListingPrice sets a listing price. The listing's owning store
// must belong to the caller's seller identity, so a seller cannot reprice
// another seller's listing by guessing an identifier.
func (s *server) handleSetSellerListingPrice(w http.ResponseWriter, r *http.Request) {
	listingID := chi.URLParam(r, "listingID")
	if !s.authorizeListing(w, r, listingID) {
		return
	}
	var body PriceUpdateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	price, err := money.New(body.AmountMinor, body.Currency)
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}
	if _, err := s.deps.Repo.SetSellerListingPrice(r.Context(), listingID, price); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "updated"})
}

func (s *server) handleUpdateSellerListingStatus(w http.ResponseWriter, r *http.Request) {
	listingID := chi.URLParam(r, "listingID")
	if !s.authorizeListing(w, r, listingID) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Repo.UpdateSellerListingStatus(r.Context(), listingID, body.Status); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}

// authorizeListing verifies the caller may mutate a listing.
//
// Admin may mutate any listing. A seller caller must own the store the listing
// belongs to. The listing is loaded through the repository and its store is
// resolved, so ownership is checked against persisted data rather than any
// caller-supplied field.
func (s *server) authorizeListing(w http.ResponseWriter, r *http.Request, listingID string) bool {
	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return false
	}
	if caller == serviceauth.CallerAdmin {
		return true
	}

	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return false
	}
	sellerID, err := s.deps.Commerce.ResolveSellerIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return false
	}

	listing, err := s.deps.Repo.GetSellerListingByID(r.Context(), listingID)
	if err != nil {
		writeDomainError(w, err)
		return false
	}
	store, err := s.deps.Repo.GetStore(r.Context(), listing.StoreID)
	if err != nil {
		writeDomainError(w, err)
		return false
	}
	if store.SellerID != sellerID {
		writeError(w, CodeNotFound)
		return false
	}
	return true
}
