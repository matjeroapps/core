package coreapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"core/internal/marketplace"
	"core/modules/commerce"
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

func (s *server) handleAddMarketplaceCartItem(w http.ResponseWriter, r *http.Request) {
	if s.deps.Marketplace == nil {
		writeError(w, CodeUnavailable)
		return
	}

	var req MarketplaceCartAddItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	listingID := strings.TrimSpace(req.SellerListingID)
	if listingID == "" {
		listingID = strings.TrimSpace(req.ListingID)
	}

	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}

	locale := i18n.FromContext(r.Context())
	if strings.TrimSpace(req.Locale) != "" {
		locale = i18n.Locale(strings.TrimSpace(req.Locale))
	}

	var sourceCollection marketplace.CollectionType
	if req.SourceCollection != nil {
		sourceCollection = marketplace.CollectionType(strings.TrimSpace(*req.SourceCollection))
	}

	marketCode := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "market_code")))

	resolved, err := s.deps.Marketplace.ResolveListing(r.Context(), marketplace.ResolveListingParams{
		MarketCode:       marketCode,
		SellerListingID:  listingID,
		Quantity:         quantity,
		SKUID:            strings.TrimSpace(req.SKUID),
		SourceCollection: sourceCollection,
		Locale:           locale,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	cartToken := strings.TrimSpace(req.CartToken)
	if cartToken == "" {
		cartToken = cartTokenFromRequest(r)
	}

	var activeCart commerce.Cart
	var effectiveToken string

	if cartToken == "" {
		_, rawToken, err := s.deps.Repo.CreateCart(r.Context(), resolved.StoreID, resolved.MarketCode, nil)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		updatedCart, err := s.deps.Repo.AddCartItem(r.Context(), resolved.StoreID, rawToken, resolved.SKUID, resolved.Quantity)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		activeCart = updatedCart
		effectiveToken = rawToken
	} else {
		existingCart, err := s.deps.Repo.GetCartByTokenAnyStore(r.Context(), cartToken)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		if existingCart.MarketCode != marketCode || existingCart.MarketCode != resolved.MarketCode {
			writeDomainError(w, commerce.ErrMarketMismatch)
			return
		}
		if existingCart.StoreID != resolved.StoreID {
			writeDomainError(w, commerce.ErrStoreMismatch)
			return
		}
		if existingCart.Status != commerce.CartStatusActive {
			writeDomainError(w, commerce.ErrCartExpired)
			return
		}
		updatedCart, err := s.deps.Repo.AddCartItem(r.Context(), resolved.StoreID, cartToken, resolved.SKUID, resolved.Quantity)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		activeCart = updatedCart
		effectiveToken = cartToken
	}

	items := make([]MarketplaceCartLineResponse, 0, len(activeCart.Items))
	for _, item := range activeCart.Items {
		items = append(items, MarketplaceCartLineResponse{
			ID:              item.ID,
			SellerListingID: item.SellerListingID,
			SKUID:           item.SKUID,
			Quantity:        item.Quantity,
			UnitPriceMinor:  item.ExpectedUnitPriceMinor,
			CurrencyCode:    item.ExpectedCurrencyCode,
		})
	}

	resp := MarketplaceCartHandoffResponse{
		CartID:     activeCart.ID,
		CartToken:  effectiveToken,
		StoreID:    activeCart.StoreID,
		MarketCode: activeCart.MarketCode,
		Status:     activeCart.Status,
		Items:      items,
		Attribution: &MarketplaceAttributionResponse{
			SellerListingID:  resolved.Attribution.SellerListingID,
			StoreID:          resolved.Attribution.StoreID,
			MarketCode:       resolved.Attribution.MarketCode,
			SourceCollection: resolved.Attribution.SourceCollection,
		},
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}
