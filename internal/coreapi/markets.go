package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/modules/contracts"
	"core/packages/httpx"
	"core/packages/i18n"
)

// handleListMarkets serves the market list every actor needs for its
// /v1/bootstrap and /v1/markets routes.
func (s *server) handleListMarkets(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Markets.List(r.Context(), i18n.FromContext(r.Context()))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, contracts.MarketsResponse{Markets: items})
}

// handleGetMarket resolves a single market by code.
func (s *server) handleGetMarket(w http.ResponseWriter, r *http.Request) {
	market, err := s.deps.Markets.GetByCode(r.Context(), chi.URLParam(r, "code"), i18n.FromContext(r.Context()))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, market)
}
