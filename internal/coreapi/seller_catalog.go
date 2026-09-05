package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/httpx"
)

type StoreProductCreateRequest = commerce.SellerProductDraft

type StoreProductUpdateRequest struct {
	Slug         string                        `json:"slug"`
	Translations []commerce.ProductTranslation `json:"translations"`
	CategoryIDs  []string                      `json:"category_ids"`
}

type VariantCreateRequest struct {
	Code   string `json:"code"`
	Status string `json:"status"`
}

type SKUCreateRequest struct {
	Code    string `json:"code"`
	Barcode string `json:"barcode"`
	Status  string `json:"status"`
}

type StoreInventorySnapshotCreateRequest struct {
	LocationID string `json:"location_id"`
	SKUID      string `json:"sku_id"`
	OnHandQty  int64  `json:"on_hand_qty"`
}

type StoreInventoryAdjustmentRequest struct {
	QuantityDelta int64  `json:"quantity_delta"`
	Reason        string `json:"reason"`
}

type OrderTransitionRequest struct {
	TargetStatus string  `json:"target_status"`
	Reason       *string `json:"reason,omitempty"`
}

func (s *server) handleListStoreProducts(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	sourceFilter := r.URL.Query().Get("source")
	queryFilter := r.URL.Query().Get("query")
	page := parsePage(r)

	items, _, err := s.deps.Commerce.ListSellerProductsForSubject(r.Context(), subject, storeID, statusFilter, sourceFilter, queryFilter, page.Limit, page.Offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SellerProductListItem]{
		Items: items,
	})
}

func (s *server) handleCreateStoreProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req StoreProductCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	detail, err := s.deps.Commerce.CreateSellerProductForSubject(r.Context(), subject, storeID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, detail)
}

func (s *server) handleGetStoreProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	detail, err := s.deps.Commerce.GetSellerProductDetailForSubject(r.Context(), subject, storeID, productID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (s *server) handleUpdateStoreProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req StoreProductUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	detail, err := s.deps.Commerce.UpdateSellerProductForSubject(r.Context(), subject, storeID, productID, req.Slug, req.Translations, req.CategoryIDs)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (s *server) handleCreateProductVariant(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req VariantCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	v, err := s.deps.Commerce.CreateVariantForSubject(r.Context(), subject, storeID, productID, req.Code, req.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (s *server) handleUpdateProductVariant(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	variantID := chi.URLParam(r, "variantID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req VariantCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	v, err := s.deps.Commerce.UpdateVariantForSubject(r.Context(), subject, storeID, productID, variantID, req.Code, req.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, v)
}

func (s *server) handleCreateVariantSKU(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	variantID := chi.URLParam(r, "variantID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req SKUCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	sku, err := s.deps.Commerce.CreateSKUForSubject(r.Context(), subject, storeID, productID, variantID, req.Code, req.Barcode, req.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, sku)
}

func (s *server) handleUpdateVariantSKU(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	variantID := chi.URLParam(r, "variantID")
	skuID := chi.URLParam(r, "skuID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req SKUCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	sku, err := s.deps.Commerce.UpdateSKUForSubject(r.Context(), subject, storeID, productID, variantID, skuID, req.Code, req.Barcode, req.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sku)
}

func (s *server) handlePresignMediaUpload(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req commerce.MediaUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	res, err := s.deps.Commerce.GenerateMediaUploadPresignedURLForSubject(r.Context(), subject, storeID, productID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *server) handleCompleteMediaUpload(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req commerce.CompleteMediaUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	m, err := s.deps.Commerce.CompleteMediaUploadForSubject(r.Context(), subject, storeID, productID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, m)
}

func (s *server) handleUpdateProductMedia(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	mediaID := chi.URLParam(r, "mediaID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req struct {
		AltText   string `json:"alt_text"`
		SortOrder int    `json:"sort_order"`
		IsPrimary bool   `json:"is_primary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	m, err := s.deps.Commerce.UpdateMediaMetadataForSubject(r.Context(), subject, storeID, productID, mediaID, req.AltText, req.SortOrder, req.IsPrimary)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, m)
}

func (s *server) handleDeleteProductMedia(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	mediaID := chi.URLParam(r, "mediaID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	if err := s.deps.Commerce.DeleteMediaMetadataForSubject(r.Context(), subject, storeID, productID, mediaID); err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}

func (s *server) handleListStoreLocations(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	locations, err := s.deps.Commerce.ListStoreFulfillmentLocationsForSubject(r.Context(), subject, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.FulfillmentLocation]{
		Items: locations,
	})
}

func (s *server) handleListStoreInventory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	summaries, err := s.deps.Commerce.ListStoreInventoryForSubject(r.Context(), subject, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SellerInventorySummary]{
		Items: summaries,
	})
}

func (s *server) handleCreateStoreInventorySnapshot(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req StoreInventorySnapshotCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	snap, err := s.deps.Commerce.CreateInventorySnapshotForSubject(r.Context(), subject, storeID, req.LocationID, req.SKUID, req.OnHandQty)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, snap)
}

func (s *server) handleAdjustStoreInventory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	snapshotID := chi.URLParam(r, "snapshotID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req StoreInventoryAdjustmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	correlationID := httpx.CorrelationID(r.Context())
	snap, _, err := s.deps.Commerce.AdjustStoreInventoryForSubject(r.Context(), subject, storeID, snapshotID, req.QuantityDelta, req.Reason, correlationID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, snap)
}

func (s *server) handleGetListingPresentation(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	listingID := chi.URLParam(r, "listingID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	pres, err := s.deps.Commerce.GetListingPresentationForSubject(r.Context(), subject, storeID, listingID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, pres)
}

func (s *server) handleUpdateListingPresentation(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	listingID := chi.URLParam(r, "listingID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var pres commerce.SellerListingPresentation
	if err := json.NewDecoder(r.Body).Decode(&pres); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	updated, err := s.deps.Commerce.UpdateListingPresentationForSubject(r.Context(), subject, storeID, listingID, pres)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (s *server) handlePublishStoreProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	if err := s.deps.Commerce.PublishSellerProductForSubject(r.Context(), subject, storeID, productID); err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "active"})
}

func (s *server) handleUnpublishStoreProduct(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	productID := chi.URLParam(r, "productID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	if err := s.deps.Commerce.UnpublishSellerProductForSubject(r.Context(), subject, storeID, productID); err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "inactive"})
}

func (s *server) handleListStoreOrders(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	page := parsePage(r)

	orders, _, err := s.deps.Commerce.ListStoreOrdersForSubject(r.Context(), subject, storeID, statusFilter, page.Limit, page.Offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Order]{
		Items: orders,
	})
}

func (s *server) handleGetStoreOrder(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	orderID := chi.URLParam(r, "orderID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	order, err := s.deps.Commerce.GetStoreOrderForSubject(r.Context(), subject, storeID, orderID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, order)
}

func (s *server) handleTransitionStoreOrder(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	orderID := chi.URLParam(r, "orderID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req OrderTransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	correlationID := httpx.CorrelationID(r.Context())
	order, err := s.deps.Commerce.TransitionStoreOrderForSubject(r.Context(), subject, storeID, orderID, req.TargetStatus, req.Reason, correlationID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, order)
}
