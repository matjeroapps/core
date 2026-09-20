package coreapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/internal/marketplace"
)

type stubMarketplace struct {
	err error
}

func (s stubMarketplace) GetCollection(context.Context, marketplace.PageRequest, marketplace.CollectionType) (marketplace.Collection, error) {
	if s.err != nil {
		return marketplace.Collection{}, s.err
	}
	return marketplace.Collection{Items: []marketplace.Item{}}, nil
}

func TestMarketplaceCollectionRequiresServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: stubMarketplace{}})
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", nil)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMarketplaceCollectionMapsInvalidCollectionType(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: stubMarketplace{err: marketplace.ErrInvalidCollectionType}})
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/nope", "seller", testSellerToken)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := decodeError(t, rec).Error.Code; got != CodeInvalidArgument {
		t.Fatalf("error code = %q, want %q", got, CodeInvalidArgument)
	}
}

func TestMarketplaceCollectionAllowsAuthorizedCallers(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: stubMarketplace{}})

	callers := []struct {
		name   string
		caller string
		token  string
	}{
		{name: "platform", caller: "platform", token: testPlatformToken},
		{name: "seller", caller: "seller", token: testSellerToken},
		{name: "admin", caller: "admin", token: testAdminToken},
		{name: "supplier", caller: "supplier", token: testSupplierToken},
	}

	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", caller.caller, caller.token)
			rec := doRequest(t, handler, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestMarketplaceCollectionRejectsUnrecognizedCaller(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: stubMarketplace{}})
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", "attacker", testPlatformToken)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
