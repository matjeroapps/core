package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	"core/modules/api"
	"core/modules/contracts"
	"core/modules/markets"
	"core/packages/httpx"
)

// ActorRoutes returns the route specs every actor API serves: app bootstrap and
// market discovery. The authenticated flag toggles the bearer requirement so the
// public storefront API can reuse the same definitions.
func ActorRoutes(authenticated bool) []RouteSpec {
	return []RouteSpec{
		{
			Method:      http.MethodGet,
			Path:        "/v1/bootstrap",
			OperationID: "getBootstrap",
			Summary:     "Load app bootstrap data",
			Description: "Returns the app identity, localization context, authenticated principal when present, and market list.",
			Tags:        []string{"Identity & Access", "Markets"},
			Auth:        authenticated,
			Responses: []ResponseSpec{
				OKResponse("Bootstrap payload", api.Bootstrap{}),
				ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
				ErrorResponse(http.StatusForbidden, "Forbidden"),
				ErrorResponse(http.StatusInternalServerError, "Internal error"),
			},
		},
		{
			Method:      http.MethodGet,
			Path:        "/v1/markets",
			OperationID: "listMarkets",
			Summary:     "List markets",
			Tags:        []string{"Markets"},
			Auth:        authenticated,
			Parameters:  []ParameterSpec{LimitParam(), OffsetParam()},
			Responses: []ResponseSpec{
				OKResponse("Market collection", contracts.MarketsResponse{}),
				ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
				ErrorResponse(http.StatusForbidden, "Forbidden"),
				ErrorResponse(http.StatusInternalServerError, "Internal error"),
			},
		},
		{
			Method:      http.MethodGet,
			Path:        "/v1/markets/{code}",
			OperationID: "getMarket",
			Summary:     "Get a market",
			Tags:        []string{"Markets"},
			Auth:        authenticated,
			Parameters:  []ParameterSpec{PathStringParam("code", "Market code")},
			Responses: []ResponseSpec{
				OKResponse("Market", markets.Market{}),
				ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
				ErrorResponse(http.StatusForbidden, "Forbidden"),
				ErrorResponse(http.StatusNotFound, "Not found"),
				ErrorResponse(http.StatusInternalServerError, "Internal error"),
			},
		},
	}
}

// CommonTags returns the platform-wide OpenAPI tag catalogue shared by every
// actor specification.
func CommonTags() []openapi3.Tag {
	return []openapi3.Tag{
		{Name: "Identity & Access", Description: "Authentication-aware app bootstrap and principal context"},
		{Name: "Markets", Description: "Market bootstrap and discovery"},
		{Name: "Suppliers", Description: "Supplier profile and supplier management"},
		{Name: "Sellers", Description: "Seller profile and seller management"},
		{Name: "Stores", Description: "Seller store management"},
		{Name: "Catalog", Description: "Catalog browsing and product management"},
		{Name: "Categories", Description: "Category management"},
		{Name: "Attributes", Description: "Attribute management"},
		{Name: "Variants", Description: "Variant management"},
		{Name: "SKUs", Description: "SKU management"},
		{Name: "Fulfillment Locations", Description: "Supplier fulfillment locations"},
		{Name: "Supplier Offers", Description: "Supplier offers and availability"},
		{Name: "Seller Listings", Description: "Seller listings and price/status controls"},
		{Name: "Inventory", Description: "Inventory snapshots and movements"},
		{Name: "Audit", Description: "Administrative moderation and operational inspection"},
	}
}

// ListResponses builds the standard response set for a collection endpoint.
func ListResponses[T any](description string) []ResponseSpec {
	return []ResponseSpec{
		OKResponse(description, contracts.CollectionResponse[T]{}),
		ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
		ErrorResponse(http.StatusForbidden, "Forbidden"),
		ErrorResponse(http.StatusNotFound, "Not found"),
		ErrorResponse(http.StatusInternalServerError, "Internal error"),
	}
}

// AuthReadResponses builds the standard response set for an authenticated read.
func AuthReadResponses(description string, body any) []ResponseSpec {
	return []ResponseSpec{
		OKResponse(description, body),
		ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
		ErrorResponse(http.StatusForbidden, "Forbidden"),
		ErrorResponse(http.StatusNotFound, "Not found"),
		ErrorResponse(http.StatusInternalServerError, "Internal error"),
	}
}

// AuthCreatedResponses builds the standard response set for an authenticated
// create.
func AuthCreatedResponses(description string, body any) []ResponseSpec {
	return []ResponseSpec{
		CreatedResponse(description, body),
		ErrorResponse(http.StatusBadRequest, "Validation error"),
		ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
		ErrorResponse(http.StatusForbidden, "Forbidden"),
		ErrorResponse(http.StatusNotFound, "Not found"),
		ErrorResponse(http.StatusConflict, "Conflict"),
		ErrorResponse(http.StatusInternalServerError, "Internal error"),
	}
}

// AuthOKResponses builds the standard response set for an authenticated
// mutation returning 200.
func AuthOKResponses(description string, body any) []ResponseSpec {
	return []ResponseSpec{
		OKResponse(description, body),
		ErrorResponse(http.StatusBadRequest, "Validation error"),
		ErrorResponse(http.StatusUnauthorized, "Unauthorized"),
		ErrorResponse(http.StatusForbidden, "Forbidden"),
		ErrorResponse(http.StatusNotFound, "Not found"),
		ErrorResponse(http.StatusConflict, "Conflict"),
		ErrorResponse(http.StatusInternalServerError, "Internal error"),
	}
}

// OKResponse describes a 200 response carrying body.
func OKResponse(description string, body any) ResponseSpec {
	return ResponseSpec{Status: http.StatusOK, Description: description, Body: body}
}

// CreatedResponse describes a 201 response carrying body.
func CreatedResponse(description string, body any) ResponseSpec {
	return ResponseSpec{Status: http.StatusCreated, Description: description, Body: body}
}

// ErrorResponse describes an error response using the platform error contract.
func ErrorResponse(status int, description string) ResponseSpec {
	return ResponseSpec{Status: status, Description: description, Body: httpx.ErrorResponse{}}
}

// LimitParam describes the shared pagination limit query parameter.
func LimitParam() ParameterSpec {
	return ParameterSpec{Name: "limit", In: "query", Required: false, Description: "Page size, capped by the service default", Schema: int64(0)}
}

// OffsetParam describes the shared pagination offset query parameter.
func OffsetParam() ParameterSpec {
	return ParameterSpec{Name: "offset", In: "query", Required: false, Description: "Zero-based offset", Schema: int64(0)}
}

// PathStringParam describes a required string path parameter.
func PathStringParam(name, description string) ParameterSpec {
	return ParameterSpec{Name: name, In: "path", Required: true, Description: description, Schema: ""}
}

// StringParam describes a string query parameter.
func StringParam(name, description string, required bool) ParameterSpec {
	return ParameterSpec{Name: name, In: "query", Required: required, Description: description, Schema: ""}
}
