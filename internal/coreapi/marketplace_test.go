package coreapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/internal/marketplace"
	"core/internal/serviceauth"
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

func TestMarketplaceCollectionAllowsAuthenticatedCaller(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: stubMarketplace{}})
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderService, "seller")
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
