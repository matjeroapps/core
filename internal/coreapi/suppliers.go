package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/httpx"
	"github.com/matjeroapps/core/packages/money"
)

// Supplier handlers.
//
// As with sellers, business identity is resolved by Core from the forwarded
// subject. Every supplier-scoped write goes through a *ForSubject service
// method so ownership is enforced in the domain layer, not by the transport.

func (s *server) handleResolveSupplier(w http.ResponseWriter, r *http.Request) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	supplierID, err := s.deps.Commerce.ResolveSupplierIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SupplierResolveResponse{SupplierID: supplierID})
}

func (s *server) handleGetSupplier(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	supplier, err := s.deps.Repo.GetSupplierByID(r.Context(), supplierID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	settings, _ := s.deps.Repo.GetSupplierSettings(r.Context(), supplierID)
	httpx.WriteJSON(w, http.StatusOK, SupplierProfileResponse{Supplier: supplier, Settings: settings})
}

func (s *server) handleUpdateSupplierProfile(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	var body ProfileUpdateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Repo.UpdateSupplierProfile(r.Context(), supplierID, body.Name, body.Status, body.Settings); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}

func (s *server) handleUpdateSupplierStatus(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Repo.UpdateSupplierStatus(r.Context(), chi.URLParam(r, "supplierID"), body.Status); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}

func (s *server) handleListSuppliers(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	items, err := s.deps.Repo.ListSuppliers(r.Context(), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Supplier]{Items: items})
}

func (s *server) handleListSupplierMarkets(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListSupplierMarkets(r.Context(), supplierID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SupplierMarket]{Items: items})
}

func (s *server) handleListSupplierLocations(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListFulfillmentLocations(r.Context(), supplierID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.FulfillmentLocation]{Items: items})
}

func (s *server) handleCreateSupplierLocation(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body FulfillmentLocationCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	location, err := s.deps.Commerce.CreateFulfillmentLocationForSubject(r.Context(), subject, supplierID, body.SupplierMarketID, body.MarketCode, body.Code, body.Name, body.LocationType, body.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, location)
}

func (s *server) handleListSupplierProducts(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListSupplierProducts(r.Context(), supplierID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SupplierProduct]{Items: items})
}

// handleCreateSupplierProduct creates the global product, its translations, the
// supplier binding, and the category assignments.
//
// The whole sequence is one atomic Core operation: a failure at any step leaves
// no rows behind, so a caller can never observe a product without its supplier
// binding or without its categories.
func (s *server) handleCreateSupplierProduct(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body ProductCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	translations := make([]commerce.ProductTranslation, 0, len(body.Translations))
	for _, translation := range body.Translations {
		translations = append(translations, commerce.ProductTranslation{
			Locale:      translation.Locale,
			Name:        translation.Name,
			Description: translation.Description,
		})
	}

	product, supplierProduct, err := s.deps.Commerce.CreateSupplierProductWithDetailsForSubject(r.Context(), subject, supplierID, commerce.ProductDraft{
		Slug:         body.Slug,
		Status:       body.Status,
		SupplierCode: body.SupplierCode,
		Translations: translations,
		CategoryIDs:  body.CategoryIDs,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ProductCreateResponse{Product: product, SupplierProduct: supplierProduct})
}

func (s *server) handleSetProductCategories(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body ProductCategoriesRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Commerce.SetProductCategoriesForSubject(r.Context(), subject, supplierID, chi.URLParam(r, "productID"), body.CategoryIDs); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProductCategoriesResponse{CategoryIDs: body.CategoryIDs})
}

func (s *server) handleListSupplierOffers(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListSupplierOffers(r.Context(), supplierID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SupplierOffer]{Items: items})
}

// handleCreateSupplierOffer creates an offer together with its optional price and
// availability.
//
// The three writes are one atomic Core operation, so an offer is never left
// unpriced or without availability because a later step failed.
func (s *server) handleCreateSupplierOffer(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body SupplierOfferCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	draft := commerce.OfferDraft{
		SupplierProductID: body.SupplierProductID,
		SupplierMarketID:  body.SupplierMarketID,
		MarketCode:        body.MarketCode,
		Status:            body.Status,
		IsAvailable:       body.IsAvailable,
		AvailableQty:      body.AvailableQty,
	}
	if body.Price != nil {
		price, err := money.New(body.Price.AmountMinor, body.Price.Currency)
		if err != nil {
			writeError(w, CodeValidationError)
			return
		}
		draft.Price = &price
	}

	offer, err := s.deps.Commerce.CreateSupplierOfferWithDetailsForSubject(r.Context(), subject, supplierID, draft)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, offer)
}

func (s *server) handleListInventorySnapshots(w http.ResponseWriter, r *http.Request) {
	supplierID, ok := s.authorizeSupplier(w, r)
	if !ok {
		return
	}
	items, err := s.deps.Repo.ListInventorySnapshots(r.Context(), supplierID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.InventorySnapshot]{Items: items})
}

// handleCreateInventorySnapshot opens a snapshot after verifying the target
// fulfillment location belongs to the authenticated supplier.
func (s *server) handleCreateInventorySnapshot(w http.ResponseWriter, r *http.Request) {
	_, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body InventorySnapshotCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	location, err := s.deps.Repo.GetFulfillmentLocationByID(r.Context(), body.FulfillmentLocationID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if location.SupplierID != supplierID {
		writeError(w, CodeNotFound)
		return
	}
	snapshot, err := s.deps.Repo.CreateInventorySnapshot(r.Context(), body.FulfillmentLocationID, body.SKUID, body.OnHandQty)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, snapshot)
}

// handleAdjustInventory applies a stock movement. Correlation and causation
// identifiers are taken from the request context so the movement stays traceable
// back to the originating actor request.
func (s *server) handleAdjustInventory(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body InventoryAdjustmentRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	snapshot, movement, err := s.deps.Commerce.AdjustInventoryForSubject(
		r.Context(), subject, supplierID, chi.URLParam(r, "snapshotID"),
		body.QuantityDelta, body.MovementType, body.Reason,
		httpx.CorrelationID(r.Context()), httpx.RequestID(r.Context()),
	)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, InventoryAdjustmentResponse{Snapshot: snapshot, Movement: movement})
}

func (s *server) handleListInventoryMovements(w http.ResponseWriter, r *http.Request) {
	// The caller must be the owning supplier (or admin); the snapshot is then
	// addressed directly by its own identifier.
	if _, ok := s.authorizeSupplier(w, r); !ok {
		return
	}
	items, err := s.deps.Repo.ListInventoryMovements(r.Context(), chi.URLParam(r, "snapshotID"), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.InventoryMovement]{Items: items})
}

// authorizeSupplier resolves the supplier a request may act on. Admin may name
// any supplier; a supplier caller is always scoped to its own identity.
func (s *server) authorizeSupplier(w http.ResponseWriter, r *http.Request) (string, bool) {
	_, supplierID, ok := s.authorizeSupplierSubject(w, r)
	return supplierID, ok
}

// authorizeSupplierSubject additionally returns the forwarded subject, which the
// ownership-enforcing service methods require.
func (s *server) authorizeSupplierSubject(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	caller, ok := serviceauth.CallerFrom(r.Context())
	if !ok {
		writeError(w, CodeUnauthorized)
		return "", "", false
	}

	if caller == serviceauth.CallerAdmin {
		return serviceauth.SubjectFrom(r), chi.URLParam(r, "supplierID"), true
	}

	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return "", "", false
	}
	supplierID, err := s.deps.Commerce.ResolveSupplierIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return "", "", false
	}
	if supplierID != chi.URLParam(r, "supplierID") {
		writeError(w, CodeNotFound)
		return "", "", false
	}
	return subject, supplierID, true
}

func (s *server) handleListSupplierStores(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	seller, err := s.deps.Commerce.RequireSupplierRetailAccess(r.Context(), subject, supplierID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items, err := s.deps.Repo.ListStoresBySellerID(r.Context(), seller.ID, parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Store]{Items: items})
}

func (s *server) handleCreateSupplierStore(w http.ResponseWriter, r *http.Request) {
	subject, supplierID, ok := s.authorizeSupplierSubject(w, r)
	if !ok {
		return
	}
	var body StoreCreateRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	store, err := s.deps.Commerce.CreateSupplierStoreForSubject(r.Context(), subject, supplierID, body.MarketCode, body.Code, body.Name, body.Status, body.Settings)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, store)
}
