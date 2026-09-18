package coreapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/modules/commerce"
	"core/packages/httpx"
)

// Platform moderation handlers. Every handler in this file is admin-only; the
// route group already enforces that, and requireAdmin is repeated where a
// handler is also reachable from a shared group.

// overviewTables are the Core-owned tables counted by the admin dashboard. The
// SQL lives here, next to the schema it counts, so no actor needs direct
// database access to render the platform overview.
var overviewTables = []struct {
	key   string
	query string
}{
	{"suppliers", "SELECT count(*) FROM suppliers"},
	{"sellers", "SELECT count(*) FROM sellers"},
	{"stores", "SELECT count(*) FROM stores"},
	{"products", "SELECT count(*) FROM products"},
	{"categories", "SELECT count(*) FROM categories"},
	{"offers", "SELECT count(*) FROM supplier_offers"},
	{"listings", "SELECT count(*) FROM seller_listings"},
}

func (s *server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	counts := make(map[string]int, len(overviewTables))
	for _, table := range overviewTables {
		var count int
		if err := s.deps.Repo.Pool().QueryRow(r.Context(), table.query).Scan(&count); err != nil {
			writeError(w, CodeUnavailable)
			return
		}
		counts[table.key] = count
	}
	httpx.WriteJSON(w, http.StatusOK, OverviewResponse{Counts: counts})
}

func (s *server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Repo.ListProducts(r.Context(), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Product]{Items: items})
}

func (s *server) handleUpdateProductStatus(w http.ResponseWriter, r *http.Request) {
	s.updateStatus(w, r, s.deps.Repo.UpdateProductStatus, chi.URLParam(r, "productID"))
}

func (s *server) handleListCategories(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Repo.ListCategories(r.Context(), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.Category]{Items: items})
}

func (s *server) handleUpdateCategoryStatus(w http.ResponseWriter, r *http.Request) {
	s.updateStatus(w, r, s.deps.Repo.UpdateCategoryStatus, chi.URLParam(r, "categoryID"))
}

// handleListOffers lists supplier offers across the platform, optionally
// filtered by market.
func (s *server) handleListOffers(w http.ResponseWriter, r *http.Request) {
	filter := commerce.SupplierCatalogFilter{Page: parsePage(r)}
	if market := r.URL.Query().Get("market_code"); market != "" {
		filter.MarketCode = market
	}
	items, err := s.deps.Repo.ListSupplierCatalog(r.Context(), filter)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.SupplierCatalogItem]{Items: items})
}

func (s *server) handleUpdateSupplierOfferStatus(w http.ResponseWriter, r *http.Request) {
	s.updateStatus(w, r, s.deps.Repo.UpdateSupplierOfferStatus, chi.URLParam(r, "offerID"))
}

func (s *server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Repo.ListFulfillmentLocations(r.Context(), r.URL.Query().Get("supplier_id"), parsePage(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[commerce.FulfillmentLocation]{Items: items})
}

func (s *server) handleUpdateLocationStatus(w http.ResponseWriter, r *http.Request) {
	s.updateStatus(w, r, s.deps.Repo.UpdateFulfillmentLocationStatus, chi.URLParam(r, "locationID"))
}

// updateStatus applies a status mutation and echoes the applied value.
func (s *server) updateStatus(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string) error, id string) {
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := fn(r.Context(), id, body.Status); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: body.Status})
}
