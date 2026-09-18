// Package actorhttp exposes the shared HTTP helpers required by every actor API
// repository (admin, seller, supplier).
//
// These helpers were extracted verbatim from the monorepo's internal
// platformapi package during the multi-repository folder split. They are public
// because admin, seller and supplier now live in separate repositories and must
// share identical pagination parsing, principal extraction, JSON decoding and
// commerce error mapping. Duplicating them per repository would let error codes
// and status mappings drift apart.
//
// Only helpers with genuine cross-repository consumers are exported here.
package actorhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"core/modules/commerce"
	"core/packages/auth"
	"core/packages/httpx"
)

// Page carries the normalised pagination window parsed from a request.
type Page struct {
	Limit  int
	Offset int
}

// ParsePage reads limit/offset query parameters, defaulting the limit to 25
// when it is missing, non-positive or above 100, and clamping a negative
// offset to zero.
func ParsePage(r *http.Request) Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: limit, Offset: offset}
}

// SubjectFrom returns the authenticated principal subject carried on the
// request context.
func SubjectFrom(r *http.Request) (string, error) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return "", errors.New("missing principal")
	}
	if principal.Subject == "" {
		return "", errors.New("missing principal subject")
	}
	return principal.Subject, nil
}

// DecodeJSON decodes the request body into dst. It writes a 400 response and
// reports false when the body is not valid JSON.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return false
	}
	return true
}

// TranslationInput is the localized name/description payload shared by the
// actor write endpoints.
type TranslationInput struct {
	Locale      string `json:"locale"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateStatusHandler implements the shared "POST .../{id}/status" contract.
func UpdateStatusHandler(w http.ResponseWriter, r *http.Request, fn func(context.Context, string, string) error) {
	var body struct {
		Status string `json:"status"`
	}
	if !DecodeJSON(w, r, &body) {
		return
	}
	if err := fn(r.Context(), chi.URLParam(r, "id"), body.Status); err != nil {
		WriteCommerceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

// ResolveSupplierID maps an authenticated subject to its supplier identifier.
func ResolveSupplierID(ctx context.Context, svc commerce.Service, subject string) (string, error) {
	supplierID, err := svc.ResolveSupplierIDForSubject(ctx, subject)
	if err != nil {
		return "", err
	}
	return supplierID, nil
}

// ResolveSellerID maps an authenticated subject to its seller identifier.
func ResolveSellerID(ctx context.Context, svc commerce.Service, subject string) (string, error) {
	sellerID, err := svc.ResolveSellerIDForSubject(ctx, subject)
	if err != nil {
		return "", err
	}
	return sellerID, nil
}

// WriteCommerceError maps commerce domain errors onto the platform HTTP error
// contract. Every actor repository must map them identically.
func WriteCommerceError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, commerce.ErrInvalidInput):
		httpx.WriteError(w, http.StatusBadRequest, "validation_error", "invalid input")
	case errors.Is(err, commerce.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, commerce.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "conflict", "conflict")
	case errors.Is(err, commerce.ErrMarketMismatch):
		httpx.WriteError(w, http.StatusConflict, "market_mismatch", "market mismatch")
	case errors.Is(err, commerce.ErrInsufficientInventory):
		httpx.WriteError(w, http.StatusConflict, "insufficient_inventory", "insufficient inventory")
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
