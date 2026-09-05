package coreapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/contracts"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/openapi"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
)

// Internal OpenAPI document.
//
// This spec is generated from the route declarations below and committed to
// docs/api/internal/openapi.json. It exists for documentation, review and
// compatibility governance. Actor repositories must never generate a client from
// it during their build: that would reintroduce the compile-time coupling
// ADR-017 removes.

// InternalSpecTitle is the title of the internal OpenAPI document.
const InternalSpecTitle = "Matjero Core Internal API"

// InternalSpecDescription documents the audience and security posture of the
// internal API. It is part of the published contract, so it must stay accurate.
const InternalSpecDescription = `Internal service-to-service API exposing Core-owned business capabilities.

This API is NOT public. It is intended for a private service network with TLS
outside trusted local development. It is never exposed through the public
storefront domain and has no browser CORS.

Every request must present a per-caller service credential:

  Authorization: Bearer <service-token>
  X-Matjero-Service: seller|admin|supplier

The token is bound to the caller named in X-Matjero-Service; a valid token
presented under a different caller name is rejected.

Actor APIs forward a minimal verified actor context:

  X-Matjero-Subject: <authenticated end-user subject>
  X-Matjero-Storefront-Host: <trusted normalized storefront host>
  X-Matjero-Storefront-Preview: <signed theme preview token, storefront bootstrap only>

Core resolves business identity from the subject itself and never trusts a
caller-supplied seller, supplier or store identifier as an authorization
decision. Actors must strip any client-supplied copy of these headers before
setting trusted values.

Successful public storefront reads carry the authoritative cache generation of
the resolved store:

  X-Matjero-Storefront-Revision: <opaque generation>

The generation is read before the payload, so it is a lower bound on the
payload's freshness. A caching actor stores each response under the generation
returned with it, never under one it probed earlier, and treats the value as
opaque.

A valid X-Matjero-Storefront-Preview header on GET /internal/v1/storefront/store
returns the exact draft theme for the host-resolved store, active installation
and current draft revision named by the token. Preview bootstrap responses are
private, no-store responses and do not carry X-Matjero-Storefront-Revision.

Errors use a closed vocabulary (not_found, invalid_argument, validation_error,
unauthorized, forbidden, conflict, market_mismatch, insufficient_inventory,
schema_mismatch, unsafe_content, preview_unavailable, storefront_unavailable,
unavailable, internal_error). Error responses never carry SQL text, stack
traces, internal table names or secret values.`

// BuildInternalSpec builds the internal OpenAPI document from the route
// declarations below.
func BuildInternalSpec() (*openapi3.T, error) {
	doc, err := openapi.BuildDocument(openapi.DocumentSpec{
		Title:         InternalSpecTitle,
		Description:   InternalSpecDescription,
		Authenticated: true,
		Tags:          internalTags(),
		Routes:        internalRoutes(),
	})
	if err != nil {
		return nil, err
	}

	// The shared builder describes the bearer scheme as an OIDC token. The
	// internal API uses a service token instead, so correct the description
	// rather than forking the builder.
	if scheme, ok := doc.Components.SecuritySchemes["bearerAuth"]; ok && scheme.Value != nil {
		scheme.Value.Description = "Core internal per-caller service token"
	}
	return doc, nil
}

func internalTags() []openapi3.Tag {
	return []openapi3.Tag{
		{Name: "Markets", Description: "Market reference data shared by every actor bootstrap"},
		{Name: "Storefront", Description: "Host-scoped public catalog read model"},
		{Name: "Sellers", Description: "Seller identity, profile and stores"},
		{Name: "Suppliers", Description: "Supplier identity, catalog, offers and inventory"},
		{Name: "Stores", Description: "Store-scoped catalog and listings"},
		{Name: "Seller Listings", Description: "Seller listing price and status controls"},
		{Name: "Inventory", Description: "Inventory snapshots and movements"},
		{Name: "Themes", Description: "Theme Engine installation and configuration"},
		{Name: "Platform Administration", Description: "Platform moderation and operational overview"},
	}
}

// internalRoutes declares every route the internal API serves. This list is the
// source of truth for the generated document; a route added to the router but
// not declared here is a spec drift that CI catches.
func internalRoutes() []openapi.RouteSpec {
	notFound := openapi.ErrorResponse(http.StatusNotFound, "Not found")
	unauthorized := openapi.ErrorResponse(http.StatusUnauthorized, "Unauthorized")
	forbidden := openapi.ErrorResponse(http.StatusForbidden, "Forbidden")
	badRequest := openapi.ErrorResponse(http.StatusBadRequest, "Invalid input")
	conflict := openapi.ErrorResponse(http.StatusConflict, "Conflict")
	unavailable := openapi.ErrorResponse(http.StatusServiceUnavailable, "Unavailable")
	serverError := openapi.ErrorResponse(http.StatusInternalServerError, "Internal error")

	readResponses := func(description string, body any) []openapi.ResponseSpec {
		return []openapi.ResponseSpec{
			openapi.OKResponse(description, body),
			unauthorized, forbidden, notFound, serverError,
		}
	}
	writeResponses := func(description string, body any) []openapi.ResponseSpec {
		return []openapi.ResponseSpec{
			openapi.OKResponse(description, body),
			badRequest, unauthorized, forbidden, notFound, conflict, serverError,
		}
	}
	createResponses := func(description string, body any) []openapi.ResponseSpec {
		return []openapi.ResponseSpec{
			openapi.CreatedResponse(description, body),
			badRequest, unauthorized, forbidden, notFound, conflict, serverError,
		}
	}

	pageParams := []openapi.ParameterSpec{openapi.LimitParam(), openapi.OffsetParam()}
	pathParam := openapi.PathStringParam

	// Successful public storefront reads are labelled with the cache generation
	// their payload is at least as new as, so a downstream cache stores each
	// payload under the generation that produced it instead of one it probed
	// earlier.
	revisionHeader := []openapi.HeaderSpec{{
		Name:        HeaderStorefrontRevision,
		Description: "Opaque public cache generation of the resolved store",
		Schema:      int64(0),
	}}
	storefrontReadResponses := func(description string, body any) []openapi.ResponseSpec {
		ok := openapi.OKResponse(description, body)
		ok.Headers = revisionHeader
		return []openapi.ResponseSpec{ok, unauthorized, forbidden, notFound, serverError}
	}

	return []openapi.RouteSpec{
		// --- Markets ---
		{
			Method: http.MethodGet, Path: "/internal/v1/markets", OperationID: "internalListMarkets",
			Summary: "List markets", Tags: []string{"Markets"},
			Parameters: pageParams,
			Responses:  readResponses("Market collection", contracts.MarketsResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/markets/{code}", OperationID: "internalGetMarket",
			Summary: "Get a market", Tags: []string{"Markets"},
			Parameters: []openapi.ParameterSpec{pathParam("code", "Market code")},
			Responses:  readResponses("Market", markets.Market{}),
		},

		// --- Storefront ---
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/revision", OperationID: "internalGetStorefrontRevision",
			Summary: "Read the public cache generation for a trusted host",
			Description: "Returns the authoritative, opaque cache generation of the store resolved from " +
				"X-Matjero-Storefront-Host. It changes whenever anything the public storefront renders for that " +
				"store changes, so a cache that includes it in its key is invalidated by moving to a new " +
				"namespace instead of deleting entries. An unknown host, an inactive domain and an inactive " +
				"store are indistinguishable and yield no revision, which stops a cache from serving a store " +
				"that no longer resolves publicly. The number carries no business meaning.",
			Tags:      []string{"Storefront"},
			Responses: readResponses("Storefront cache generation", StorefrontRevisionResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/store", OperationID: "internalGetStorefrontStore",
			Summary: "Resolve the storefront bootstrap for a trusted host",
			Description: "Tenant identity comes only from X-Matjero-Storefront-Host. The request Host and X-Forwarded-Host are ignored. " +
				"Without X-Matjero-Storefront-Preview the bootstrap returns the published theme and carries X-Matjero-Storefront-Revision for public cache keys. " +
				"With a valid preview token it returns the exact current draft theme bound to the resolved store, active installation and draft revision; preview responses are private, no-store and intentionally omit the revision header.",
			Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{{
				Name:        HeaderStorefrontPreview,
				In:          "header",
				Required:    false,
				Description: "Optional signed theme preview token. Valid only for storefront bootstrap; not a browser authentication mechanism.",
				Schema:      "",
			}},
			Responses: storefrontReadResponses("Storefront bootstrap", storefrontStoreResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/categories", OperationID: "internalListStorefrontCategories",
			Summary: "List public categories", Tags: []string{"Storefront"},
			Responses: storefrontReadResponses("Category collection", CollectionResponse[storefront.CategoryNode]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/categories/{slug}", OperationID: "internalGetStorefrontCategory",
			Summary: "Get a public category", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{pathParam("slug", "Category slug")},
			Responses:  storefrontReadResponses("Category", storefrontCategoryResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/products", OperationID: "internalListStorefrontProducts",
			Summary: "Browse public products", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("category", "Category slug filter", false),
				openapi.StringParam("availability", "Availability filter", false),
				openapi.StringParam("sort", "Sort order", false),
				openapi.StringParam("min_price", "Minimum price in minor units", false),
				openapi.StringParam("max_price", "Maximum price in minor units", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: storefrontReadResponses("Product page", storefrontProductPageResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/products/{slug}", OperationID: "internalGetStorefrontProduct",
			Summary: "Get a public product", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{pathParam("slug", "Product slug")},
			Responses:  storefrontReadResponses("Product detail", storefrontProductResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/search", OperationID: "internalSearchStorefrontProducts",
			Summary: "Search public products", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("q", "Search keyword", false),
				openapi.StringParam("category", "Category slug filter", false),
				openapi.StringParam("availability", "Availability filter", false),
				openapi.StringParam("sort", "Sort order", false),
				openapi.StringParam("min_price", "Minimum price in minor units", false),
				openapi.StringParam("max_price", "Maximum price in minor units", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: storefrontReadResponses("Product page", storefrontProductPageResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/storefront/carts", OperationID: "internalCreateStorefrontCart",
			Summary: "Create a host-scoped Cart capability", Tags: []string{"Storefront"},
			Responses: createResponses("Cart capability", CartResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/carts", OperationID: "internalGetStorefrontCart",
			Summary: "Read a Cart by its bearer capability", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{{Name: HeaderCartToken, In: "header", Required: true, Description: "Opaque Cart bearer capability", Schema: ""}},
			Responses:  readResponses("Cart", CartResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/storefront/carts/items", OperationID: "internalAddStorefrontCartItem",
			Summary: "Add a public SKU to a host-scoped Cart", Tags: []string{"Storefront"},
			Parameters:  []openapi.ParameterSpec{{Name: HeaderCartToken, In: "header", Required: true, Description: "Opaque Cart bearer capability", Schema: ""}},
			RequestBody: CartAddItemRequest{}, Responses: writeResponses("Cart", CartResponse{}),
		},
		{
			Method: http.MethodPatch, Path: "/internal/v1/storefront/carts/items/{itemID}", OperationID: "internalUpdateStorefrontCartItem",
			Summary: "Update Cart item quantity", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{{Name: HeaderCartToken, In: "header", Required: true, Description: "Opaque Cart bearer capability", Schema: ""}, pathParam("itemID", "Cart item identifier")},
			RequestBody: struct {
				Quantity int64 `json:"quantity"`
			}{}, Responses: writeResponses("Cart", CartResponse{}),
		},
		{
			Method: http.MethodDelete, Path: "/internal/v1/storefront/carts/items/{itemID}", OperationID: "internalRemoveStorefrontCartItem",
			Summary: "Remove a Cart item", Tags: []string{"Storefront"},
			Parameters: []openapi.ParameterSpec{{Name: HeaderCartToken, In: "header", Required: true, Description: "Opaque Cart bearer capability", Schema: ""}, pathParam("itemID", "Cart item identifier")},
			Responses:  writeResponses("Cart", CartResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/storefront/checkout-sessions", OperationID: "internalCreateCheckoutSession",
			Summary: "Create a host-scoped Checkout Session", Tags: []string{"Storefront"},
			Description: "Creates an open Guest Checkout Session and returns its one-time guest Order capability. The capability digest is never returned.",
			Parameters:  []openapi.ParameterSpec{{Name: HeaderCartToken, In: "header", Required: true, Description: "Opaque Cart bearer capability", Schema: ""}},
			Responses:   createResponses("Checkout Session and one-time capability", CheckoutSessionResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/storefront/checkout-sessions/{sessionID}/finalize", OperationID: "internalFinalizeCheckoutSession",
			Summary: "Finalize Checkout Session atomically", Tags: []string{"Storefront"},
			Description: "Executes atomic single-transaction checkout finalization turning an eligible Checkout Session and Cart into an Order.",
			Parameters:  []openapi.ParameterSpec{pathParam("sessionID", "Checkout Session identifier")},
			RequestBody: CheckoutFinalizeRequest{}, Responses: writeResponses("Order", commerce.PublicOrder{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/storefront/orders/{orderID}", OperationID: "internalGetGuestOrder",
			Summary: "Read a Guest Order by capability", Tags: []string{"Storefront"},
			Description: "Reads a Guest Order scoped to the host-resolved Store and authorized by X-Matjero-Guest-Order-Token.",
			Parameters: []openapi.ParameterSpec{
				{Name: HeaderGuestOrderToken, In: "header", Required: true, Description: "Raw guest order capability token", Schema: ""},
				pathParam("orderID", "Order identifier"),
			},
			Responses: readResponses("Guest Order", commerce.PublicOrder{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/storefront/orders/{orderID}/cancel", OperationID: "internalCancelGuestOrder",
			Summary: "Cancel a pending Guest Order by capability", Tags: []string{"Storefront"},
			Description: "Cancels a pending Guest Order scoped to the host-resolved Store and authorized by X-Matjero-Guest-Order-Token.",
			Parameters: []openapi.ParameterSpec{
				{Name: HeaderGuestOrderToken, In: "header", Required: true, Description: "Raw guest order capability token", Schema: ""},
				pathParam("orderID", "Order identifier"),
			},
			Responses: writeResponses("Guest Order", commerce.PublicOrder{}),
		},

		// --- Sellers ---
		{
			Method: http.MethodGet, Path: "/internal/v1/sellers/resolve", OperationID: "internalResolveSeller",
			Summary:     "Resolve a subject to its seller identity",
			Description: "Core performs the resolution; a caller cannot assert its own seller identifier.",
			Tags:        []string{"Sellers"},
			Responses:   readResponses("Seller identity", SellerResolveResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/sellers", OperationID: "internalListSellers",
			Summary: "List sellers (admin)", Tags: []string{"Sellers"},
			Parameters: pageParams,
			Responses:  readResponses("Seller collection", CollectionResponse[commerce.Seller]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/sellers/{sellerID}", OperationID: "internalGetSeller",
			Summary: "Get a seller profile", Tags: []string{"Sellers"},
			Parameters: []openapi.ParameterSpec{pathParam("sellerID", "Seller identifier")},
			Responses:  readResponses("Seller profile", SellerProfileResponse{}),
		},
		{
			Method: http.MethodPut, Path: "/internal/v1/sellers/{sellerID}/profile", OperationID: "internalUpdateSellerProfile",
			Summary: "Update a seller profile", Tags: []string{"Sellers"},
			Parameters:  []openapi.ParameterSpec{pathParam("sellerID", "Seller identifier")},
			RequestBody: ProfileUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/sellers/{sellerID}/status", OperationID: "internalUpdateSellerStatus",
			Summary: "Update a seller status (admin)", Tags: []string{"Sellers"},
			Parameters:  []openapi.ParameterSpec{pathParam("sellerID", "Seller identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/sellers/{sellerID}/stores", OperationID: "internalListSellerStores",
			Summary: "List stores owned by a seller", Tags: []string{"Sellers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("sellerID", "Seller identifier")}, pageParams...),
			Responses:  readResponses("Store collection", CollectionResponse[commerce.Store]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/sellers/{sellerID}/stores", OperationID: "internalCreateSellerStore",
			Summary: "Create a store for a seller", Tags: []string{"Sellers"},
			Parameters:  []openapi.ParameterSpec{pathParam("sellerID", "Seller identifier")},
			RequestBody: StoreCreateRequest{},
			Responses:   createResponses("Created store", commerce.Store{}),
		},

		// --- Suppliers ---
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/resolve", OperationID: "internalResolveSupplier",
			Summary:     "Resolve a subject to its supplier identity",
			Description: "Core performs the resolution; a caller cannot assert its own supplier identifier.",
			Tags:        []string{"Suppliers"},
			Responses:   readResponses("Supplier identity", SupplierResolveResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers", OperationID: "internalListSuppliers",
			Summary: "List suppliers (admin)", Tags: []string{"Suppliers"},
			Parameters: pageParams,
			Responses:  readResponses("Supplier collection", CollectionResponse[commerce.Supplier]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}", OperationID: "internalGetSupplier",
			Summary: "Get a supplier profile", Tags: []string{"Suppliers"},
			Parameters: []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			Responses:  readResponses("Supplier profile", SupplierProfileResponse{}),
		},
		{
			Method: http.MethodPut, Path: "/internal/v1/suppliers/{supplierID}/profile", OperationID: "internalUpdateSupplierProfile",
			Summary: "Update a supplier profile", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: ProfileUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/status", OperationID: "internalUpdateSupplierStatus",
			Summary: "Update a supplier status (admin)", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/markets", OperationID: "internalListSupplierMarkets",
			Summary: "List a supplier's markets", Tags: []string{"Suppliers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Supplier market collection", CollectionResponse[commerce.SupplierMarket]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/locations", OperationID: "internalListSupplierLocations",
			Summary: "List a supplier's fulfillment locations", Tags: []string{"Suppliers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Location collection", CollectionResponse[commerce.FulfillmentLocation]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/locations", OperationID: "internalCreateSupplierLocation",
			Summary: "Create a fulfillment location", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: FulfillmentLocationCreateRequest{},
			Responses:   createResponses("Created location", commerce.FulfillmentLocation{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/products", OperationID: "internalListSupplierProducts",
			Summary: "List a supplier's products", Tags: []string{"Suppliers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Supplier product collection", CollectionResponse[commerce.SupplierProduct]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/products", OperationID: "internalCreateSupplierProduct",
			Summary: "Create a product and bind it to a supplier", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: ProductCreateRequest{},
			Responses:   createResponses("Created product", ProductCreateResponse{}),
		},
		{
			Method: http.MethodPut, Path: "/internal/v1/suppliers/{supplierID}/products/{productID}/categories", OperationID: "internalSetProductCategories",
			Summary: "Replace a product's category assignments", Tags: []string{"Suppliers"},
			Parameters: []openapi.ParameterSpec{
				pathParam("supplierID", "Supplier identifier"),
				pathParam("productID", "Product identifier"),
			},
			RequestBody: ProductCategoriesRequest{},
			Responses:   writeResponses("Applied categories", ProductCategoriesResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/offers", OperationID: "internalListSupplierOffers",
			Summary: "List a supplier's offers", Tags: []string{"Suppliers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Offer collection", CollectionResponse[commerce.SupplierOffer]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/offers", OperationID: "internalCreateSupplierOffer",
			Summary: "Create a supplier offer", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: SupplierOfferCreateRequest{},
			Responses:   createResponses("Created offer", commerce.SupplierOffer{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/inventory", OperationID: "internalListInventorySnapshots",
			Summary: "List inventory snapshots", Tags: []string{"Inventory"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Snapshot collection", CollectionResponse[commerce.InventorySnapshot]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/inventory/snapshots", OperationID: "internalCreateInventorySnapshot",
			Summary: "Open an inventory snapshot", Tags: []string{"Inventory"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: InventorySnapshotCreateRequest{},
			Responses:   createResponses("Created snapshot", commerce.InventorySnapshot{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/inventory/{snapshotID}/adjustments", OperationID: "internalAdjustInventory",
			Summary: "Apply an inventory movement", Tags: []string{"Inventory"},
			Parameters: []openapi.ParameterSpec{
				pathParam("supplierID", "Supplier identifier"),
				pathParam("snapshotID", "Inventory snapshot identifier"),
			},
			RequestBody: InventoryAdjustmentRequest{},
			Responses:   writeResponses("Updated snapshot and movement", InventoryAdjustmentResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/inventory/{snapshotID}/movements", OperationID: "internalListInventoryMovements",
			Summary: "List inventory movements", Tags: []string{"Inventory"},
			Parameters: []openapi.ParameterSpec{
				pathParam("supplierID", "Supplier identifier"),
				pathParam("snapshotID", "Inventory snapshot identifier"),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Movement collection", CollectionResponse[commerce.InventoryMovement]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/retail-capability", OperationID: "internalGetSupplierRetailCapability",
			Summary: "Get a supplier's explicit retail link and profile", Tags: []string{"Suppliers"},
			Parameters: []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			Responses:  readResponses("Supplier retail capability", SupplierRetailCapabilityResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/retail-capability", OperationID: "internalCreateSupplierRetailCapability",
			Summary: "Provision a seller profile and explicit 1:1 retail affiliation for a supplier", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: SupplierRetailCapabilityRequest{},
			Responses:   createResponses("Provisioned supplier retail capability", SupplierRetailCapabilityResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/suppliers/{supplierID}/stores", OperationID: "internalListSupplierStores",
			Summary: "List retail stores owned by the supplier's affiliated seller", Tags: []string{"Suppliers"},
			Parameters: append([]openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")}, pageParams...),
			Responses:  readResponses("Supplier stores collection", CollectionResponse[commerce.Store]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/suppliers/{supplierID}/stores", OperationID: "internalCreateSupplierStore",
			Summary: "Create a retail store owned by the supplier's affiliated seller", Tags: []string{"Suppliers"},
			Parameters:  []openapi.ParameterSpec{pathParam("supplierID", "Supplier identifier")},
			RequestBody: StoreCreateRequest{},
			Responses:   createResponses("Created store", commerce.Store{}),
		},

		// --- Stores ---
		{
			Method: http.MethodGet, Path: "/internal/v1/stores", OperationID: "internalListStores",
			Summary: "List stores (admin)", Tags: []string{"Stores"},
			Parameters: pageParams,
			Responses:  readResponses("Store collection", CollectionResponse[commerce.Store]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}", OperationID: "internalGetStore",
			Summary: "Get a store", Tags: []string{"Stores"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  readResponses("Store", commerce.Store{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/storefront-host", OperationID: "internalGetStorefrontHost",
			Summary:     "Get authoritative storefront host for an authorized store",
			Description: "Returns the bare normalized active primary storefront host for an authorized store.",
			Tags:        []string{"Stores"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:   readResponses("Storefront host", StorefrontHostResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/domains", OperationID: "internalListStoreDomains",
			Summary: "List store domains", Tags: []string{"Stores"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  readResponses("Domain collection", CollectionResponse[commerce.StoreDomainResponse]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/domains", OperationID: "internalRequestCustomDomain",
			Summary: "Request custom store domain", Tags: []string{"Stores"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: CustomDomainRequest{},
			Responses:   createResponses("Created custom domain", commerce.StoreDomainResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/domains/{domainID}/verify", OperationID: "internalVerifyCustomDomain",
			Summary: "Verify custom store domain DNS TXT record", Tags: []string{"Stores"},
			Parameters: []openapi.ParameterSpec{
				pathParam("storeID", "Store identifier"),
				pathParam("domainID", "Domain identifier"),
			},
			Responses: []openapi.ResponseSpec{
				openapi.OKResponse("Domain verification status", commerce.StoreDomainResponse{}),
				badRequest, unauthorized, forbidden, notFound, unavailable, serverError,
			},
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/domains/{domainID}/activate", OperationID: "internalActivateCustomDomain",
			Summary: "Activate verified custom store domain", Tags: []string{"Stores"},
			Parameters: []openapi.ParameterSpec{
				pathParam("storeID", "Store identifier"),
				pathParam("domainID", "Domain identifier"),
			},
			Responses: writeResponses("Activated custom domain", commerce.StoreDomainResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/status", OperationID: "internalUpdateStoreStatus",
			Summary: "Update a store status (admin)", Tags: []string{"Stores"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/supplier-catalog", OperationID: "internalListSupplierCatalog",
			Summary:     "Browse supplier offers available to a store's market",
			Description: "The market scope is taken from the store record, never from the query string.",
			Tags:        []string{"Stores"},
			Parameters: []openapi.ParameterSpec{
				pathParam("storeID", "Store identifier"),
				openapi.StringParam("supplier_id", "Supplier filter", false),
				openapi.StringParam("category_id", "Category filter", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Catalog collection", CollectionResponse[commerce.SupplierCatalogItem]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/listings", OperationID: "internalListStoreListings",
			Summary: "List a store's seller listings", Tags: []string{"Stores"},
			Parameters: append([]openapi.ParameterSpec{pathParam("storeID", "Store identifier")}, pageParams...),
			Responses:  readResponses("Listing collection", CollectionResponse[commerce.SellerListing]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/listings", OperationID: "internalImportSellerListing",
			Summary:     "Import a supplier offer into a store",
			Description: "The target store is the authorized path parameter, not the request body.",
			Tags:        []string{"Stores"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: SellerListingImportRequest{},
			Responses:   createResponses("Created listing", commerce.SellerListing{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/locations", OperationID: "internalCreateStoreLocation",
			Summary: "Create a Store-owned fulfillment location", Tags: []string{"Stores"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: StoreFulfillmentLocationCreateRequest{},
			Responses:   createResponses("Created Store-owned location", commerce.FulfillmentLocation{}),
		},

		// --- Seller listings ---
		{
			Method: http.MethodGet, Path: "/internal/v1/listings", OperationID: "internalListSellerListings",
			Summary: "List seller listings (admin)", Tags: []string{"Seller Listings"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("store_id", "Store filter", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Listing collection", CollectionResponse[commerce.SellerListing]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/listings/{listingID}/price", OperationID: "internalSetSellerListingPrice",
			Summary: "Set a seller listing price", Tags: []string{"Seller Listings"},
			Parameters:  []openapi.ParameterSpec{pathParam("listingID", "Listing identifier")},
			RequestBody: PriceUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/listings/{listingID}/status", OperationID: "internalUpdateSellerListingStatus",
			Summary: "Update a seller listing status", Tags: []string{"Seller Listings"},
			Parameters:  []openapi.ParameterSpec{pathParam("listingID", "Listing identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},

		// --- Themes ---
		{
			Method: http.MethodGet, Path: "/internal/v1/themes", OperationID: "internalListThemes",
			Summary: "List registered themes", Tags: []string{"Themes"},
			Responses: readResponses("Theme collection", CollectionResponse[themes.Theme]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/themes/{key}/versions", OperationID: "internalListThemeVersions",
			Summary: "List versions of a theme", Tags: []string{"Themes"},
			Parameters: []openapi.ParameterSpec{pathParam("key", "Theme key")},
			Responses:  readResponses("Theme version collection", CollectionResponse[themes.ThemeVersion]{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/theme", OperationID: "internalGetThemeInstallation",
			Summary: "Get a store's theme installation", Tags: []string{"Themes"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  readResponses("Theme installation", ThemeInstallationResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/theme/install", OperationID: "internalInstallTheme",
			Summary: "Install a theme on a store", Tags: []string{"Themes"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: ThemeInstallRequest{},
			Responses:   createResponses("Theme installation", ThemeInstallationResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/stores/{storeID}/theme/draft", OperationID: "internalGetThemeDraft",
			Summary: "Get a store's draft theme configuration", Tags: []string{"Themes"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  readResponses("Draft configuration", ThemeDraftResponse{}),
		},
		{
			Method: http.MethodPut, Path: "/internal/v1/stores/{storeID}/theme/draft", OperationID: "internalUpdateThemeDraft",
			Summary: "Replace a store's draft theme configuration", Tags: []string{"Themes"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: ThemeConfigRequest{},
			Responses:   writeResponses("Draft configuration", ThemeDraftResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/theme/publish", OperationID: "internalPublishTheme",
			Summary: "Publish a store's draft theme configuration", Tags: []string{"Themes"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  writeResponses("Published revision", ThemePublishResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/theme/discard", OperationID: "internalDiscardThemeDraft",
			Summary: "Discard a store's draft theme configuration", Tags: []string{"Themes"},
			Parameters: []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses:  writeResponses("Reset draft configuration", ThemeDraftResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/theme/upgrade", OperationID: "internalUpgradeTheme",
			Summary: "Upgrade a store to a newer published theme version", Tags: []string{"Themes"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			RequestBody: ThemeUpgradeRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/stores/{storeID}/theme/preview", OperationID: "internalCreateThemePreview",
			Summary:     "Issue a signed preview token for a store's draft",
			Description: "Returns 503 preview_unavailable when THEME_PREVIEW_SECRET is not configured. It never falls back to an unsigned token.",
			Tags:        []string{"Themes"},
			Parameters:  []openapi.ParameterSpec{pathParam("storeID", "Store identifier")},
			Responses: []openapi.ResponseSpec{
				openapi.OKResponse("Preview token", ThemePreviewResponse{}),
				unauthorized, forbidden, notFound, unavailable, serverError,
			},
		},

		// --- Platform administration ---
		{
			Method: http.MethodGet, Path: "/internal/v1/admin/overview", OperationID: "internalGetAdminOverview",
			Summary: "Platform aggregate counts", Tags: []string{"Platform Administration"},
			Responses: []openapi.ResponseSpec{
				openapi.OKResponse("Overview counts", OverviewResponse{}),
				unauthorized, forbidden, unavailable, serverError,
			},
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/domains", OperationID: "internalListDomainsAdmin",
			Summary: "List domains across stores (admin)", Tags: []string{"Platform Administration"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("store_id", "Store filter", false),
				openapi.StringParam("seller_id", "Seller filter", false),
				openapi.StringParam("status", "Status filter", false),
				openapi.StringParam("domain_type", "Domain type filter", false),
				openapi.StringParam("search", "Domain search filter", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Domain collection", CollectionResponse[commerce.StoreDomainAdminResponse]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/domains/{domainID}/disable", OperationID: "internalDisableDomain",
			Summary: "Disable a store domain (admin)", Tags: []string{"Platform Administration"},
			Parameters: []openapi.ParameterSpec{pathParam("domainID", "Domain identifier")},
			Responses:  writeResponses("Disabled domain", commerce.StoreDomainAdminResponse{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/domains/{domainID}/enable", OperationID: "internalEnableDomain",
			Summary: "Re-enable a store domain (admin)", Tags: []string{"Platform Administration"},
			Parameters: []openapi.ParameterSpec{pathParam("domainID", "Domain identifier")},
			Responses:  writeResponses("Enabled domain", commerce.StoreDomainAdminResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/products", OperationID: "internalListProducts",
			Summary: "List products (admin)", Tags: []string{"Platform Administration"},
			Parameters: pageParams,
			Responses:  readResponses("Product collection", CollectionResponse[commerce.Product]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/products/{productID}/status", OperationID: "internalUpdateProductStatus",
			Summary: "Update a product status (admin)", Tags: []string{"Platform Administration"},
			Parameters:  []openapi.ParameterSpec{pathParam("productID", "Product identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/categories", OperationID: "internalListCategories",
			Summary: "List categories (admin)", Tags: []string{"Platform Administration"},
			Parameters: pageParams,
			Responses:  readResponses("Category collection", CollectionResponse[commerce.Category]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/categories/{categoryID}/status", OperationID: "internalUpdateCategoryStatus",
			Summary: "Update a category status (admin)", Tags: []string{"Platform Administration"},
			Parameters:  []openapi.ParameterSpec{pathParam("categoryID", "Category identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/offers", OperationID: "internalListOffers",
			Summary: "List supplier offers (admin)", Tags: []string{"Platform Administration"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("market_code", "Market filter", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Offer collection", CollectionResponse[commerce.SupplierCatalogItem]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/offers/{offerID}/status", OperationID: "internalUpdateSupplierOfferStatus",
			Summary: "Update a supplier offer status (admin)", Tags: []string{"Platform Administration"},
			Parameters:  []openapi.ParameterSpec{pathParam("offerID", "Offer identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
		{
			Method: http.MethodGet, Path: "/internal/v1/locations", OperationID: "internalListLocations",
			Summary: "List fulfillment locations (admin)", Tags: []string{"Platform Administration"},
			Parameters: []openapi.ParameterSpec{
				openapi.StringParam("supplier_id", "Supplier filter", false),
				openapi.LimitParam(), openapi.OffsetParam(),
			},
			Responses: readResponses("Location collection", CollectionResponse[commerce.FulfillmentLocation]{}),
		},
		{
			Method: http.MethodPost, Path: "/internal/v1/locations/{locationID}/status", OperationID: "internalUpdateLocationStatus",
			Summary: "Update a fulfillment location status (admin)", Tags: []string{"Platform Administration"},
			Parameters:  []openapi.ParameterSpec{pathParam("locationID", "Location identifier")},
			RequestBody: contracts.StatusUpdateRequest{},
			Responses:   writeResponses("Applied status", StatusResponse{}),
		},
	}
}
