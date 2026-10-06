package coreapi

// Store-scoped category endpoints (seller-managed, per-store).
//
// These endpoints never expose the platform-global categories tables: all data
// comes from commerce.Service store-category methods scoped by (store, seller
// membership). Foreign and unknown ids are both not_found so a caller cannot
// probe another store's catalog. See specs/030-store-scoped-categories.

import (
	"encoding/json"
	"net/http"
	"time"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"
	"github.com/go-chi/chi/v5"
)

type storeCategoryTranslationDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type storeCategoryResponse struct {
	ID               string                                 `json:"id"`
	StoreID          string                                 `json:"store_id"`
	ParentCategoryID *string                                `json:"parent_category_id"`
	Slug             string                                 `json:"slug"`
	Status           string                                 `json:"status"`
	SortOrder        int                                    `json:"sort_order"`
	Translations     map[string]storeCategoryTranslationDTO `json:"translations"`
	ProductCount     int                                    `json:"product_count"`
	ChildCount       int                                    `json:"child_count"`
	CreatedAt        time.Time                              `json:"created_at"`
	UpdatedAt        time.Time                              `json:"updated_at"`
}

type storeCategoryListResponse struct {
	Items  []storeCategoryResponse `json:"items"`
	Total  int                     `json:"total"`
	Limit  int                     `json:"limit"`
	Offset int                     `json:"offset"`
}

func toStoreCategoryResponse(category commerce.StoreCategoryWithMeta) storeCategoryResponse {
	translations := make(map[string]storeCategoryTranslationDTO, len(category.Translations))
	for _, tr := range category.Translations {
		translations[tr.Locale] = storeCategoryTranslationDTO{Name: tr.Name, Description: tr.Description}
	}
	return storeCategoryResponse{
		ID:               category.ID,
		StoreID:          category.StoreID,
		ParentCategoryID: category.ParentCategoryID,
		Slug:             category.Slug,
		Status:           category.Status,
		SortOrder:        category.SortOrder,
		Translations:     translations,
		ProductCount:     category.ProductCount,
		ChildCount:       category.ChildCount,
		CreatedAt:        category.CreatedAt,
		UpdatedAt:        category.UpdatedAt,
	}
}

// storeCategoryTranslationsRequest accepts the wire map form
// {"en": {"name": ..., "description": ...}} and flattens it for the domain.
func storeCategoryTranslationsRequest(raw map[string]storeCategoryTranslationDTO) []commerce.StoreCategoryTranslation {
	if raw == nil {
		return nil
	}
	out := make([]commerce.StoreCategoryTranslation, 0, len(raw))
	for locale, tr := range raw {
		out = append(out, commerce.StoreCategoryTranslation{
			Locale:      locale,
			Name:        tr.Name,
			Description: tr.Description,
		})
	}
	return out
}

func (s *server) handleListStoreCategories(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	page := parsePage(r)
	items, total, err := s.deps.Commerce.ListStoreCategoriesForSubject(r.Context(), subject, storeID, r.URL.Query().Get("status"), page.Limit, page.Offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	response := make([]storeCategoryResponse, len(items))
	for i, item := range items {
		response[i] = toStoreCategoryResponse(item)
	}
	httpx.WriteJSON(w, http.StatusOK, storeCategoryListResponse{
		Items:  response,
		Total:  total,
		Limit:  page.Limit,
		Offset: page.Offset,
	})
}

type createStoreCategoryRequest struct {
	Slug             string                                 `json:"slug"`
	ParentCategoryID *string                                `json:"parent_category_id"`
	SortOrder        *int                                   `json:"sort_order"`
	Translations     map[string]storeCategoryTranslationDTO `json:"translations"`
}

func (s *server) handleCreateStoreCategory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req createStoreCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	category, err := s.deps.Commerce.CreateStoreCategoryForSubject(r.Context(), subject, storeID, req.Slug, req.ParentCategoryID, req.SortOrder, storeCategoryTranslationsRequest(req.Translations))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toStoreCategoryResponse(category))
}

func (s *server) handleGetStoreCategory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	categoryID := chi.URLParam(r, "categoryID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	category, err := s.deps.Commerce.GetStoreCategoryForSubject(r.Context(), subject, storeID, categoryID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toStoreCategoryResponse(category))
}

type updateStoreCategoryRequest struct {
	Slug             *string                                `json:"slug"`
	ParentCategoryID *string                                `json:"parent_category_id"`
	ClearParent      bool                                   `json:"clear_parent"`
	SortOrder        *int                                   `json:"sort_order"`
	Translations     map[string]storeCategoryTranslationDTO `json:"translations"`
}

func (s *server) handleUpdateStoreCategory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	categoryID := chi.URLParam(r, "categoryID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req updateStoreCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	// A nil Translations slice means "leave translations unchanged"; an empty
	// map means "replace with nothing", which validation rejects (en required).
	var translations []commerce.StoreCategoryTranslation
	if req.Translations != nil {
		translations = storeCategoryTranslationsRequest(req.Translations)
	}

	category, err := s.deps.Commerce.UpdateStoreCategoryForSubject(r.Context(), subject, storeID, categoryID, commerce.StoreCategoryPatch{
		Slug:             req.Slug,
		ParentCategoryID: req.ParentCategoryID,
		ClearParent:      req.ClearParent,
		SortOrder:        req.SortOrder,
		Translations:     translations,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toStoreCategoryResponse(category))
}

type storeCategoryStatusRequest struct {
	Status string `json:"status"`
}

func (s *server) handleTransitionStoreCategoryStatus(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	categoryID := chi.URLParam(r, "categoryID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req storeCategoryStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	category, err := s.deps.Commerce.TransitionStoreCategoryForSubject(r.Context(), subject, storeID, categoryID, req.Status)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toStoreCategoryResponse(category))
}

func (s *server) handleDeleteStoreCategory(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	categoryID := chi.URLParam(r, "categoryID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	if err := s.deps.Commerce.DeleteStoreCategoryForSubject(r.Context(), subject, storeID, categoryID); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}

type reorderStoreCategoriesRequest struct {
	Order []commerce.StoreCategoryOrder `json:"order"`
}

func (s *server) handleReorderStoreCategories(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req reorderStoreCategoriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	if err := s.deps.Commerce.ReorderStoreCategoriesForSubject(r.Context(), subject, storeID, req.Order); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "ok"})
}
