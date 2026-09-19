package coreapi

import (
	"net/http"
	"strconv"

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
