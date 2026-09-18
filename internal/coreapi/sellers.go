package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"
)

// Seller handlers.
//
// Business identity is always resolved by Core from the forwarded subject via
// the *ForSubject service methods. A caller can never assert its own seller
// identifier: the sellerID path parameter is only ever used for admin-scoped
// operations, and seller-scoped operations derive it from the subject.

// handleResolveSeller maps an authenticated subject to its seller identity.
func (s *server) handleResolveSeller(w http.ResponseWriter, r *http.Request) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	sellerID, err := s.deps.Commerce.ResolveSellerIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SellerResolveResponse{SellerID: sellerID})
}

// handleGetSeller returns a seller profile and settings.
//
// Admin may read any seller. A seller caller may only read its own profile: the
// identifier is resolved from the subject, so a seller cannot read another
// seller by guessing an ID.
func (s *server) handleGetSeller(w http.ResponseWriter, r *http.Request) {
	sellerID, ok := s.authorizeSeller(w, r)
	if !ok {
		return
	}
	seller, err := s.deps.Repo.GetSellerByID(r.Context(), sellerID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	settings, _ := s.deps.Repo.GetSellerSettings(r.Context(), sellerID)
	httpx.WriteJSON(w, http.StatusOK, SellerProfileResponse{Seller: seller, Settings: settings})
}

func (s *server) handleUpdateSellerProfile(w http.ResponseWriter, r *http.Request) {
	sellerID, ok := s.authorizeSeller(w, r)
	if !ok {
		return
	}
	var body ProfileUpdateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Repo.UpdateSellerProfile(r.Context(), sellerID, body.Name, body.Status, body.Settings); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}

// handleUpdateSellerStatus is an admin moderation operation.
func (s *server) handleUpdateSellerStatus(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Repo.UpdateSellerStatus(r.Context(), chi.URLParam(r, "sellerID"), body.Status); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}

func (s *server) handleListSellers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	items, err := s.deps.Repo.ListSellers(r.Context(), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Seller]{Items: items})
}

// handleListSellerStores lists the stores owned by a seller. A seller caller
// only ever sees its own stores; admin may pass any sellerID.
func (s *server) handleListSellerStores(w http.ResponseWriter, r *http.Request) {
	sellerID, ok := s.authorizeSeller(w, r)
	if !ok {
		return
	}
	stores, err := s.deps.Repo.ListStoresBySellerID(r.Context(), sellerID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	activeCount, err := s.deps.Repo.CountActiveStoresBySellerID(r.Context(), sellerID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	limit := s.deps.Commerce.GetActiveStoreLimit()
	httpx.WriteJSON(w, http.StatusOK, SellerStoresResponse{
		Items:            stores,
		ActiveStoreLimit: limit,
		ActiveStoreCount: activeCount,
	})
}

// handleCreateSellerStore creates a store for the authenticated seller. The
// subject is required so Core can enforce ownership through the service layer
// rather than trusting the path parameter.
func (s *server) handleCreateSellerStore(w http.ResponseWriter, r *http.Request) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	sellerID, err := s.deps.Commerce.ResolveSellerIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if chi.URLParam(r, "sellerID") != sellerID {
		writeError(w, CodeForbidden)
		return
	}

	var body StoreCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	store, err := s.deps.Commerce.CreateStoreForSubject(r.Context(), subject, sellerID, body.MarketCode, body.Code, body.Name, body.Status, body.Settings)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, store)
}

// authorizeSeller resolves the seller a request may act on.
//
// Admin callers may act on the seller named in the path. Seller callers are
// always scoped to the seller Core resolved from their forwarded subject, so a
// seller cannot act on another seller by supplying a different path segment.
func (s *server) authorizeSeller(w http.ResponseWriter, r *http.Request) (string, bool) {
	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return "", false
	}

	if caller == serviceauth.CallerAdmin {
		return chi.URLParam(r, "sellerID"), true
	}

	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return "", false
	}
	sellerID, err := s.deps.Commerce.ResolveSellerIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return "", false
	}
	if sellerID != chi.URLParam(r, "sellerID") {
		// Safe not-found: a seller must not be able to distinguish "another
		// seller exists" from "no such seller".
		writeError(w, CodeNotFound)
		return "", false
	}
	return sellerID, true
}
