package coreapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"core/internal/marketplace"
	"core/packages/httpx"
	"core/packages/i18n"
)

func (s *server) handleMarketplaceCollection(w http.ResponseWriter, r *http.Request) {
	if s.deps.Marketplace == nil {
		writeError(w, CodeUnavailable)
		return
	}

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, CodeInvalidArgument)
			return
		}
	}

	collection, err := s.deps.Marketplace.GetCollection(r.Context(), marketplace.PageRequest{
		MarketCode: chi.URLParam(r, "market_code"),
		Locale:     i18n.FromContext(r.Context()),
		Limit:      limit,
		Cursor:     r.URL.Query().Get("cursor"),
	}, marketplace.CollectionType(chi.URLParam(r, "collection_type")))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, collection)
}

func (s *server) handleResolveMarketplaceListing(w http.ResponseWriter, r *http.Request) {
	if s.deps.Marketplace == nil {
		writeError(w, CodeUnavailable)
		return
	}

	var req ResolveMarketplaceListingRequest
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}

	locale := i18n.FromContext(r.Context())
	if strings.TrimSpace(req.Locale) != "" {
		locale = i18n.Locale(strings.TrimSpace(req.Locale))
	}

	resolved, err := s.deps.Marketplace.ResolveListing(r.Context(), marketplace.ResolveListingParams{
		MarketCode:       chi.URLParam(r, "market_code"),
		SellerListingID:  chi.URLParam(r, "listing_id"),
		Quantity:         req.Quantity,
		SKUID:            req.SKUID,
		SourceCollection: marketplace.CollectionType(req.SourceCollection),
		Locale:           locale,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	resp := ResolveMarketplaceListingResponse{
		SellerListingID:       resolved.SellerListingID,
		ProductID:             resolved.ProductID,
		StoreID:               resolved.StoreID,
		MarketCode:            resolved.MarketCode,
		SKUID:                 resolved.SKUID,
		Quantity:              resolved.Quantity,
		UnitPriceMinor:        resolved.UnitPriceMinor,
		CurrencyCode:          resolved.CurrencyCode,
		FulfillmentLocationID: resolved.FulfillmentLocationID,
		ProductTitle:          resolved.ProductTitle,
		SKUCode:               resolved.SKUCode,
		Attribution: MarketplaceAttributionResponse{
			SellerListingID:  resolved.Attribution.SellerListingID,
			StoreID:          resolved.Attribution.StoreID,
			MarketCode:       resolved.Attribution.MarketCode,
			SourceCollection: resolved.Attribution.SourceCollection,
		},
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}
