package coreapi

import (
	"net/http"

	"core/packages/httpx"
)

// StorefrontHostResponse is the minimal internal response containing the
// authoritative Storefront host for a store. Internal lifecycle metadata
// (verification tokens, IDs, status, domain_type) is deliberately excluded.
type StorefrontHostResponse struct {
	Host string `json:"host"`
}

func (s *server) handleGetStorefrontHost(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}

	domain, err := s.deps.Repo.GetActivePrimaryStoreDomain(r.Context(), store.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, StorefrontHostResponse{Host: domain.Domain})
}
