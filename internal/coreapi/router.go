// Package coreapi hosts the Core internal HTTP API.
//
// This is the runtime business capability boundary required by ADR-017. Actor
// repositories (admin, seller, supplier) reach every Core-owned business
// capability through this API instead of importing Core Go packages.
//
// The API is internal: it is not a public customer API, it is not exposed
// through the public storefront domain, and it has no browser CORS. Requests
// must be authenticated by the service-auth middleware mounted by the
// application, which is the only reason handlers here may trust the forwarded
// actor context and storefront host headers.
package coreapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/i18n"
)

// CatalogReader is the public catalog read model. storefront.CatalogRepository
// satisfies it.
type CatalogReader interface {
	Bootstrap(ctx context.Context, scope storefront.CatalogScope) (storefront.StoreBootstrap, error)
	Categories(ctx context.Context, scope storefront.CatalogScope) ([]storefront.CategoryNode, error)
	CategoryBySlug(ctx context.Context, scope storefront.CatalogScope, slug string) (storefront.CategoryNode, error)
	Products(ctx context.Context, scope storefront.CatalogScope, query storefront.ProductQuery) (storefront.ProductPage, error)
	Search(ctx context.Context, scope storefront.CatalogScope, keyword string, query storefront.ProductQuery) (storefront.ProductPage, error)
	ProductBySlug(ctx context.Context, scope storefront.CatalogScope, slug string) (storefront.ProductDetail, error)
}

// StoreLocator maps a trusted domain to a tenant store. storefront.StoreResolver
// satisfies it.
type StoreLocator interface {
	Resolve(ctx context.Context, domain string) (storefront.ResolvedStore, error)
}

// RevisionReader reports the authoritative public cache generation of a store.
// storefront.RevisionReader satisfies it.
//
// It is read on every public storefront route, so a downstream cache can neither
// serve a payload from an abandoned generation nor keep serving a store that
// stopped resolving publicly.
type RevisionReader interface {
	Revision(ctx context.Context, host string) (int64, error)
	RevisionFor(ctx context.Context, scope storefront.CatalogScope) (int64, error)
}

// MarketService lists and resolves markets. markets.Service satisfies it.
type MarketService interface {
	List(ctx context.Context, locale i18n.Locale) ([]markets.Market, error)
	GetByCode(ctx context.Context, code string, locale i18n.Locale) (markets.Market, error)
}

// Dependencies wires the internal API. Every field is a Core-owned capability;
// no actor ever constructs these directly.
type Dependencies struct {
	Commerce  commerce.Service
	Repo      commerce.Repository
	Markets   MarketService
	Catalog   CatalogReader
	Stores    StoreLocator
	Revisions RevisionReader
	Themes    themes.Service
}

// NewRouter registers the internal API under /internal/v1.
//
// Service authentication is intentionally not mounted here: the application
// mounts it so that operational health endpoints can stay exempt. Every route
// below therefore runs behind an authenticated caller.
func NewRouter(deps Dependencies) chi.Router {
	r := chi.NewRouter()
	r.Use(i18n.Middleware(i18n.Default()))

	server := &server{deps: deps}

	r.Route("/internal/v1", func(r chi.Router) {
		// Market discovery is shared by every actor's bootstrap route.
		r.Group(func(r chi.Router) {
			r.Get("/markets", server.handleListMarkets)
			r.Get("/markets/{code}", server.handleGetMarket)
		})

		// Public storefront catalog. Seller-owned: only the seller service
		// serves customer storefront traffic.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller))
			r.Get("/storefront/revision", server.handleStorefrontRevision)
			r.Get("/storefront/store", server.handleStorefrontStore)
			r.Get("/storefront/categories", server.handleStorefrontCategories)
			r.Get("/storefront/categories/{slug}", server.handleStorefrontCategory)
			r.Get("/storefront/products", server.handleStorefrontProducts)
			r.Get("/storefront/products/{slug}", server.handleStorefrontProduct)
			r.Get("/storefront/search", server.handleStorefrontSearch)
			r.Post("/storefront/carts", server.handleCreateCart)
			r.Get("/storefront/carts", server.handleGetCart)
			r.Post("/storefront/carts/items", server.handleAddCartItem)
			r.Patch("/storefront/carts/items/{itemID}", server.handleUpdateCartItem)
			r.Delete("/storefront/carts/items/{itemID}", server.handleRemoveCartItem)
			r.Post("/storefront/checkout-sessions", server.handleCreateCheckoutSession)
			r.Post("/storefront/checkout-sessions/{sessionID}/finalize", server.handleEvaluateCheckoutSession)
			r.Get("/storefront/orders/{orderID}", server.handleGetGuestOrder)
			r.Post("/storefront/orders/{orderID}/cancel", server.handleCancelGuestOrder)
		})

		// Store-owned fulfillment locations. Seller identity is resolved from the
		// forwarded subject and the Store path; no body field can choose ownership.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller))
			r.Post("/stores/{storeID}/locations", server.handleCreateStoreLocation)
		})

		// Seller capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller, serviceauth.CallerAdmin))
			r.Get("/sellers/resolve", server.handleResolveSeller)
			r.Get("/sellers", server.handleListSellers)
			r.Get("/sellers/{sellerID}", server.handleGetSeller)
			r.Put("/sellers/{sellerID}/profile", server.handleUpdateSellerProfile)
			r.Post("/sellers/{sellerID}/status", server.handleUpdateSellerStatus)
			r.Get("/sellers/{sellerID}/stores", server.handleListSellerStores)
			r.Post("/sellers/{sellerID}/stores", server.handleCreateSellerStore)
		})

		// Supplier capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSupplier, serviceauth.CallerAdmin))
			r.Get("/suppliers/resolve", server.handleResolveSupplier)
			r.Get("/suppliers", server.handleListSuppliers)
			r.Get("/suppliers/{supplierID}", server.handleGetSupplier)
			r.Put("/suppliers/{supplierID}/profile", server.handleUpdateSupplierProfile)
			r.Post("/suppliers/{supplierID}/status", server.handleUpdateSupplierStatus)
			r.Get("/suppliers/{supplierID}/markets", server.handleListSupplierMarkets)
			r.Get("/suppliers/{supplierID}/locations", server.handleListSupplierLocations)
			r.Post("/suppliers/{supplierID}/locations", server.handleCreateSupplierLocation)
			r.Get("/suppliers/{supplierID}/products", server.handleListSupplierProducts)
			r.Post("/suppliers/{supplierID}/products", server.handleCreateSupplierProduct)
			r.Put("/suppliers/{supplierID}/products/{productID}/categories", server.handleSetProductCategories)
			r.Get("/suppliers/{supplierID}/offers", server.handleListSupplierOffers)
			r.Post("/suppliers/{supplierID}/offers", server.handleCreateSupplierOffer)
			r.Get("/suppliers/{supplierID}/inventory", server.handleListInventorySnapshots)
			r.Post("/suppliers/{supplierID}/inventory/snapshots", server.handleCreateInventorySnapshot)
			r.Post("/suppliers/{supplierID}/inventory/{snapshotID}/adjustments", server.handleAdjustInventory)
			r.Get("/suppliers/{supplierID}/inventory/{snapshotID}/movements", server.handleListInventoryMovements)
		})

		// Supplier Retail capabilities (Supplier-service only self-service operations).
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSupplier))
			r.Get("/suppliers/{supplierID}/retail-capability", server.handleGetSupplierRetailCapability)
			r.Post("/suppliers/{supplierID}/retail-capability", server.handleCreateSupplierRetailCapability)
			r.Get("/suppliers/{supplierID}/stores", server.handleListSupplierStores)
			r.Post("/suppliers/{supplierID}/stores", server.handleCreateSupplierStore)
		})

		// Store capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller, serviceauth.CallerAdmin))
			r.Get("/stores", server.handleListStores)
			r.Get("/stores/{storeID}", server.handleGetStore)
			r.Get("/stores/{storeID}/storefront-host", server.handleGetStorefrontHost)
			r.Get("/stores/{storeID}/domains", server.handleListStoreDomains)
			r.Post("/stores/{storeID}/domains", server.handleRequestCustomDomain)
			r.Post("/stores/{storeID}/domains/{domainID}/verify", server.handleVerifyCustomDomain)
			r.Post("/stores/{storeID}/domains/{domainID}/activate", server.handleActivateCustomDomain)
			r.Post("/stores/{storeID}/status", server.handleUpdateStoreStatus)
			r.Get("/stores/{storeID}/supplier-catalog", server.handleListSupplierCatalog)
			r.Get("/stores/{storeID}/listings", server.handleListStoreListings)
			r.Post("/stores/{storeID}/listings", server.handleImportSellerListing)
		})

		// Seller listing capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller, serviceauth.CallerAdmin))
			r.Get("/listings", server.handleListSellerListings)
			r.Post("/listings/{listingID}/price", server.handleSetSellerListingPrice)
			r.Post("/listings/{listingID}/status", server.handleUpdateSellerListingStatus)
		})

		// Platform moderation capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerAdmin))
			r.Get("/admin/overview", server.handleAdminOverview)
			r.Get("/domains", server.handleAdminListDomains)
			r.Post("/domains/{domainID}/disable", server.handleAdminDisableDomain)
			r.Post("/domains/{domainID}/enable", server.handleAdminEnableDomain)
			r.Get("/products", server.handleListProducts)
			r.Post("/products/{productID}/status", server.handleUpdateProductStatus)
			r.Get("/categories", server.handleListCategories)
			r.Post("/categories/{categoryID}/status", server.handleUpdateCategoryStatus)
			r.Get("/offers", server.handleListOffers)
			r.Post("/offers/{offerID}/status", server.handleUpdateSupplierOfferStatus)
			r.Get("/locations", server.handleListLocations)
			r.Post("/locations/{locationID}/status", server.handleUpdateLocationStatus)
		})

		// Theme Engine capabilities.
		r.Group(func(r chi.Router) {
			r.Use(requireCallers(serviceauth.CallerSeller))
			r.Get("/themes", server.handleListThemes)
			r.Get("/themes/{key}/versions", server.handleListThemeVersions)
			r.Get("/stores/{storeID}/theme", server.handleGetThemeInstallation)
			r.Post("/stores/{storeID}/theme/install", server.handleInstallTheme)
			r.Get("/stores/{storeID}/theme/draft", server.handleGetThemeDraft)
			r.Put("/stores/{storeID}/theme/draft", server.handleUpdateThemeDraft)
			r.Post("/stores/{storeID}/theme/publish", server.handlePublishTheme)
			r.Post("/stores/{storeID}/theme/discard", server.handleDiscardThemeDraft)
			r.Post("/stores/{storeID}/theme/upgrade", server.handleUpgradeTheme)
			r.Post("/stores/{storeID}/theme/preview", server.handleCreateThemePreview)
		})
	})

	return r
}

type server struct {
	deps Dependencies
}

// requireCallers narrows a route group to specific authenticated actor services.
func requireCallers(allowed ...serviceauth.Caller) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return serviceauth.RequireCaller(next, allowed...)
	}
}
