package coreapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/httpx"
	"github.com/matjeroapps/core/packages/i18n"
)

// Storefront handlers expose the P4.3 public catalog read model over the
// internal API. The business implementation stays here in Core: eligibility,
// price-source rules, tenant isolation, market isolation, availability, privacy
// rules, search, filters and pagination are never reimplemented by an actor.

// HeaderStorefrontRevision labels a successful public read with the cache
// generation its payload is guaranteed to be at least as new as.
//
// A downstream cache stores the response under this value, never under a
// revision it probed earlier. The revision is read before the catalog query, so
// the payload can only be newer than the label, never older. That ordering is
// what makes the label safe: caching fresher data under an older generation is
// harmless because that generation is abandoned on the next probe, whereas
// caching older data under a newer generation would serve stale content for the
// whole entry lifetime.
const HeaderStorefrontRevision = "X-Matjero-Storefront-Revision"

// HeaderStorefrontPreview carries the signed, short-lived draft theme preview
// token from the seller-owned storefront API to Core. It is an internal runtime
// contract and never replaces service authentication.
const HeaderStorefrontPreview = "X-Matjero-Storefront-Preview"

// HeaderGuestOrderToken carries the raw guest order capability token from the
// seller-owned storefront API to Core for guest order access.
const HeaderGuestOrderToken = "X-Matjero-Guest-Order-Token"

// scopeFor resolves the tenant from the trusted storefront host forwarded by the
// actor and binds it to the negotiated locale.
//
// The request Host and any X-Forwarded-Host are ignored entirely. Tenant
// authority comes only from X-Matjero-Storefront-Host, which the actor computes
// using its own trusted-proxy policy after stripping any client-supplied copy.
// Core never accepts a client-selectable store or seller identifier as tenant
// authority.
func (s *server) scopeFor(w http.ResponseWriter, r *http.Request) (storefront.CatalogScope, bool) {
	host := serviceauth.StorefrontHostFrom(r)
	if host == "" {
		writeError(w, CodeStorefrontUnavailable)
		return storefront.CatalogScope{}, false
	}

	resolved, err := s.deps.Stores.Resolve(r.Context(), host)
	if err != nil {
		writeDomainError(w, err)
		return storefront.CatalogScope{}, false
	}

	scope, err := storefront.NewCatalogScope(resolved, i18n.FromContext(r.Context()))
	if err != nil {
		writeDomainError(w, err)
		return storefront.CatalogScope{}, false
	}
	return scope, true
}

// storefrontRead performs one public catalog read for the resolved tenant and
// labels the successful response with its cache generation.
//
// The generation is read before the payload so the label is a lower bound on the
// payload's freshness, and it is only emitted on success: an unavailable or
// invalid request discloses no generation at all.
func (s *server) storefrontRead(w http.ResponseWriter, r *http.Request, read func(storefront.CatalogScope) (any, error)) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	revision, err := s.deps.Revisions.RevisionFor(r.Context(), scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	payload, err := read(scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set(HeaderStorefrontRevision, strconv.FormatInt(revision, 10))
	httpx.WriteJSON(w, http.StatusOK, payload)
}

// handleStorefrontRevision answers the authoritative cache generation for a
// trusted host.
//
// It is the probe a downstream cache calls before trusting a cached payload, so
// it deliberately fails the same way every other public read does: an unknown
// host, an inactive domain and an inactive store are indistinguishable, and none
// of them yields a revision. That is what stops a cache from continuing to serve
// a store that stopped resolving publicly.
func (s *server) handleStorefrontRevision(w http.ResponseWriter, r *http.Request) {
	host := serviceauth.StorefrontHostFrom(r)
	if host == "" {
		writeError(w, CodeStorefrontUnavailable)
		return
	}
	revision, err := s.deps.Revisions.Revision(r.Context(), host)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StorefrontRevisionResponse{Revision: revision})
}

func (s *server) handleStorefrontStore(w http.ResponseWriter, r *http.Request) {
	previewToken := strings.TrimSpace(r.Header.Get(HeaderStorefrontPreview))
	if previewToken != "" {
		s.handleStorefrontStorePreview(w, r, previewToken)
		return
	}

	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		bootstrap, err := s.deps.Catalog.Bootstrap(r.Context(), scope)
		return storefrontStoreResponse{Store: bootstrap}, err
	})
}

func (s *server) handleStorefrontStorePreview(w http.ResponseWriter, r *http.Request, previewToken string) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	bootstrap, err := s.deps.Catalog.Bootstrap(r.Context(), scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	previewTheme, err := s.deps.Themes.ResolveStorefrontPreview(r.Context(), previewToken, scope.StoreID())
	if err != nil {
		writePreviewError(w, err)
		return
	}
	bootstrap.Theme = &storefront.StoreTheme{
		Key:                   previewTheme.Key,
		Version:               previewTheme.Version,
		Configuration:         previewTheme.Configuration,
		ConfigurationRevision: previewTheme.DraftRevision,
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.WriteJSON(w, http.StatusOK, storefrontStoreResponse{Store: bootstrap})
}

func writePreviewError(w http.ResponseWriter, err error) {
	if errors.Is(err, themes.ErrPreviewNotConfigured) {
		writeError(w, CodePreviewUnavailable)
		return
	}
	if errors.Is(err, themes.ErrSchemaMismatch) {
		writeError(w, CodeSchemaMismatch)
		return
	}
	if errors.Is(err, themes.ErrUnsafeContent) {
		writeError(w, CodeUnsafeContent)
		return
	}
	// Invalid, stale, cross-store and missing preview state all collapse into
	// the same storefront-unavailable outcome to avoid a token validity oracle.
	writeError(w, CodeStorefrontUnavailable)
}

func (s *server) handleStorefrontCategories(w http.ResponseWriter, r *http.Request) {
	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		items, err := s.deps.Catalog.Categories(r.Context(), scope)
		return CollectionResponse[storefront.CategoryNode]{Items: items}, err
	})
}

func (s *server) handleStorefrontCategory(w http.ResponseWriter, r *http.Request) {
	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		category, err := s.deps.Catalog.CategoryBySlug(r.Context(), scope, chi.URLParam(r, "slug"))
		return storefrontCategoryResponse{Category: category}, err
	})
}

func (s *server) handleStorefrontProducts(w http.ResponseWriter, r *http.Request) {
	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		query, err := parseProductQuery(r)
		if err != nil {
			return nil, err
		}
		page, err := s.deps.Catalog.Products(r.Context(), scope, query)
		return newStorefrontProductPage(page), err
	})
}

func (s *server) handleStorefrontProduct(w http.ResponseWriter, r *http.Request) {
	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		product, err := s.deps.Catalog.ProductBySlug(r.Context(), scope, chi.URLParam(r, "slug"))
		return storefrontProductResponse{Product: product}, err
	})
}

func (s *server) handleStorefrontSearch(w http.ResponseWriter, r *http.Request) {
	s.storefrontRead(w, r, func(scope storefront.CatalogScope) (any, error) {
		query, err := parseProductQuery(r)
		if err != nil {
			return nil, err
		}
		page, err := s.deps.Catalog.Search(r.Context(), scope, query.Keyword, query)
		return newStorefrontProductPage(page), err
	})
}

// --- Storefront response contracts ---
//
// These mirror the shapes the seller storefront API already publishes so the
// actor's public contract is unchanged by the transport migration.

// StorefrontRevisionResponse carries a store's public cache generation. The
// number is opaque: a consumer compares it for equality and must not infer any
// business meaning from its value.
type StorefrontRevisionResponse struct {
	Revision int64 `json:"revision"`
}

type storefrontStoreResponse struct {
	Store storefront.StoreBootstrap `json:"store"`
}

type storefrontCategoryResponse struct {
	Category storefront.CategoryNode `json:"category"`
}

type storefrontProductResponse struct {
	Product storefront.ProductDetail `json:"product"`
}

type storefrontPagination struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type storefrontProductPageResponse struct {
	Items      []storefront.ProductListItem `json:"items"`
	Pagination storefrontPagination         `json:"pagination"`
}

func newStorefrontProductPage(page storefront.ProductPage) storefrontProductPageResponse {
	items := page.Items
	if items == nil {
		items = []storefront.ProductListItem{}
	}
	return storefrontProductPageResponse{
		Items: items,
		Pagination: storefrontPagination{
			Total:  page.Total,
			Limit:  page.Limit,
			Offset: page.Offset,
		},
	}
}

// parseProductQuery validates public browse parameters before they reach the
// read model. Malformed values are rejected rather than silently defaulted, so a
// customer never receives a page they did not ask for. Bounds, sort names and
// availability values are enforced by the read model itself.
func parseProductQuery(r *http.Request) (storefront.ProductQuery, error) {
	params := r.URL.Query()
	query := storefront.ProductQuery{
		CategorySlug: strings.TrimSpace(params.Get("category")),
		Keyword:      strings.TrimSpace(params.Get("q")),
		Availability: strings.TrimSpace(params.Get("availability")),
		Sort:         strings.TrimSpace(params.Get("sort")),
	}

	limit, err := intParam(params.Get("limit"), "limit")
	if err != nil {
		return storefront.ProductQuery{}, err
	}
	offset, err := intParam(params.Get("offset"), "offset")
	if err != nil {
		return storefront.ProductQuery{}, err
	}
	if limit != nil {
		query.Page.Limit = int(*limit)
	}
	if offset != nil {
		query.Page.Offset = int(*offset)
	}

	if query.MinPriceMinor, err = intParam(params.Get("min_price"), "min_price"); err != nil {
		return storefront.ProductQuery{}, err
	}
	if query.MaxPriceMinor, err = intParam(params.Get("max_price"), "max_price"); err != nil {
		return storefront.ProductQuery{}, err
	}

	return query, nil
}

func intParam(raw, name string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an integer", storefront.ErrInvalidQuery, name)
	}
	return &value, nil
}

func (s *server) handleGetGuestOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	rawCapability := strings.TrimSpace(r.Header.Get(HeaderGuestOrderToken))
	orderID := chi.URLParam(r, "orderID")
	order, err := s.deps.Repo.GetGuestOrder(r.Context(), nil, scope.StoreID(), orderID, rawCapability)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.WriteJSON(w, http.StatusOK, order.ToPublic())
}

func (s *server) handleCancelGuestOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	rawCapability := strings.TrimSpace(r.Header.Get(HeaderGuestOrderToken))
	orderID := chi.URLParam(r, "orderID")
	correlationID := httpx.CorrelationID(r.Context())
	order, err := s.deps.Repo.CancelGuestOrder(r.Context(), nil, scope.StoreID(), orderID, rawCapability, correlationID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.WriteJSON(w, http.StatusOK, order.ToPublic())
}
