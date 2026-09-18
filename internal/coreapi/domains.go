package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"
)

// handleListStoreDomains lists domains for an authorized store.
func (s *server) handleListStoreDomains(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	subject := serviceauth.SubjectFrom(r)

	domains, err := s.deps.Commerce.ListStoreDomainsForSubject(r.Context(), subject, store.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	resp := make([]commerce.StoreDomainResponse, len(domains))
	for i, d := range domains {
		resp[i] = d.ToResponse()
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.StoreDomainResponse]{Items: resp})
}

// handleRequestCustomDomain registers a custom domain for an authorized store.
func (s *server) handleRequestCustomDomain(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	subject := serviceauth.SubjectFrom(r)

	var body CustomDomainRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	domain, err := s.deps.Commerce.RequestCustomStoreDomain(r.Context(), subject, store.ID, body.Domain)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, domain.ToResponse())
}

// handleVerifyCustomDomain performs DNS verification for a custom domain.
func (s *server) handleVerifyCustomDomain(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	subject := serviceauth.SubjectFrom(r)
	domainID := chi.URLParam(r, "domainID")

	domain, err := s.deps.Commerce.VerifyCustomStoreDomainForSubject(r.Context(), subject, store.ID, domainID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, domain.ToResponse())
}

// handleActivateCustomDomain activates a verified custom domain.
func (s *server) handleActivateCustomDomain(w http.ResponseWriter, r *http.Request) {
	store, ok := s.authorizeStore(w, r)
	if !ok {
		return
	}
	subject := serviceauth.SubjectFrom(r)
	domainID := chi.URLParam(r, "domainID")

	domain, err := s.deps.Commerce.ActivateCustomStoreDomainForSubject(r.Context(), subject, store.ID, domainID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, domain.ToResponse())
}

// handleAdminListDomains lists domains across stores for platform moderation.
func (s *server) handleAdminListDomains(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	filter := commerce.AdminDomainFilter{
		StoreID:    r.URL.Query().Get("store_id"),
		SellerID:   r.URL.Query().Get("seller_id"),
		Status:     r.URL.Query().Get("status"),
		DomainType: r.URL.Query().Get("domain_type"),
		Search:     r.URL.Query().Get("search"),
		Page:       parsePage(r),
	}

	domains, err := s.deps.Commerce.AdminListDomains(r.Context(), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	resp := make([]commerce.StoreDomainAdminResponse, len(domains))
	for i, d := range domains {
		resp[i] = d.ToAdminResponse()
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.StoreDomainAdminResponse]{Items: resp})
}

// handleAdminDisableDomain disables a domain for moderation.
func (s *server) handleAdminDisableDomain(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	domainID := chi.URLParam(r, "domainID")

	domain, err := s.deps.Commerce.AdminDisableDomain(r.Context(), domainID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, domain.ToAdminResponse())
}

// handleAdminEnableDomain re-enables a domain for moderation.
func (s *server) handleAdminEnableDomain(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	domainID := chi.URLParam(r, "domainID")

	domain, err := s.deps.Commerce.AdminEnableDomain(r.Context(), domainID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, domain.ToAdminResponse())
}
