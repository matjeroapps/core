package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/httpx"
	"github.com/matjeroapps/core/packages/i18n"
)

// Store handlers.
//
// Store ownership is enforced by resolving the caller's seller identity from its
// forwarded subject and comparing it against the store's owner. A seller that
// names another seller's store receives a safe not-found, never a forbidden, so
// store existence is not disclosed across tenants.

func (s *server) handleListStores(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	items, err := s.deps.Repo.ListStores(r.Context(), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Store]{Items: items})
}

func (s *server) handleGetStore(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, store)
}

func (s *server) handleUpdateStoreStatus(w http.ResponseWriter, r *http.Request) {
	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return
	}

	var body StoreStatusUpdateRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	storeID := chi.URLParam(r, "storeID")
	var updated commerce.Store
	var err error

	if caller == serviceauth.CallerAdmin {
		updated, err = s.deps.Commerce.UpdateStoreStatusByAdmin(r.Context(), storeID, body.Status)
	} else {
		subject := serviceauth.SubjectFrom(r)
		if subject == "" {
			writeError(w, CodeInvalidArgument)
			return
		}
		updated, err = s.deps.Commerce.UpdateStoreStatusForSubject(r.Context(), subject, storeID, body.Status)
	}

	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: updated.Status})
}

// handleListSupplierCatalog browses the supplier offers available to a store's
// market. The market code is taken from the store record, never from the query
// string, so a seller cannot widen the browse scope into another market.
func (s *server) handleListSupplierCatalog(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	filter := commerce.SupplierCatalogFilter{
		MarketCode: store.MarketCode,
		Locale:     string(i18n.FromContext(r.Context())),
		Page:       parsePage(r),
	}
	if supplier := r.URL.Query().Get("supplier_id"); supplier != "" {
		filter.SupplierID = supplier
	}
	if category := r.URL.Query().Get("category_id"); category != "" {
		filter.CategoryID = category
	}
	items, err := s.deps.Repo.ListSupplierCatalog(r.Context(), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SupplierCatalogItem]{Items: items})
}

func (s *server) handleListStoreListings(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListSellerListings(r.Context(), store.ID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SellerListing]{Items: items})
}

// handleImportSellerListing imports a supplier offer into a store. Ownership is
// enforced by the service layer through the forwarded subject.
func (s *server) handleImportSellerListing(w http.ResponseWriter, r *http.Request) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	if _, ok := s.authorizeStore(w, r); !ok {
		return
	}

	var body SellerListingImportRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	// The store is taken from the authorized path parameter, not the body, so a
	// caller cannot import into a different store than the one it was authorized
	// against.
	body.StoreID = chi.URLParam(r, "storeID")

	listing, err := s.deps.Commerce.CreateSellerListingForSubject(r.Context(), subject, body.StoreID, body.ProductID, body.SupplierOfferID, body.MarketCode, body.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, listing)
}

func (s *server) handleCreateStoreLocation(w http.ResponseWriter, r *http.Request) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	var body StoreFulfillmentLocationCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	location, err := s.deps.Commerce.CreateStoreFulfillmentLocationForSubject(
		r.Context(), subject, chi.URLParam(r, "storeID"), body.Code, body.Name, body.LocationType, body.Status,
	)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, location)
}

// authorizeStore loads a store and verifies the caller may act on it.
func (s *server) authorizeStore(w http.ResponseWriter, r *http.Request) (commerce.Store, bool) {
	store, err := s.deps.Repo.GetStore(r.Context(), chi.URLParam(r, "storeID"))
	if err != nil {
		writeDomainError(w, err)
		return commerce.Store{}, false
	}

	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return commerce.Store{}, false
	}
	if caller == serviceauth.CallerAdmin {
		return store, true
	}

	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return commerce.Store{}, false
	}
	if _, err := s.deps.Commerce.AuthorizeSellerAccess(r.Context(), subject, store.SellerID, commerce.RoleStaff); err != nil {
		writeDomainError(w, err)
		return commerce.Store{}, false
	}
	return store, true
}
