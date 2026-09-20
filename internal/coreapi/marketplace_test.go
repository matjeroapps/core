package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/internal/marketplace"
)

type stubMarketplace struct {
	err            error
	resolveResult  marketplace.ResolvedListing
	resolveErr     error
	receivedParams marketplace.ResolveListingParams
}

func (s *stubMarketplace) GetCollection(context.Context, marketplace.PageRequest, marketplace.CollectionType) (marketplace.Collection, error) {
	if s.err != nil {
		return marketplace.Collection{}, s.err
	}
	return marketplace.Collection{Items: []marketplace.Item{}}, nil
}

func (s *stubMarketplace) ResolveListing(_ context.Context, params marketplace.ResolveListingParams) (marketplace.ResolvedListing, error) {
	s.receivedParams = params
	if s.resolveErr != nil {
		return marketplace.ResolvedListing{}, s.resolveErr
	}
	return s.resolveResult, nil
}

func TestMarketplaceCollectionRequiresServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{}})
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", nil)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMarketplaceCollectionMapsInvalidCollectionType(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{err: marketplace.ErrInvalidCollectionType}})
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
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{}})

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
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{}})
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/EG/marketplace/collections/new_products", "attacker", testPlatformToken)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestResolveMarketplaceListingRequiresServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{}})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/listings/listing-123/resolve", nil)
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestResolveMarketplaceListingAllowsPlatformCaller(t *testing.T) {
	fastDelivery := string(marketplace.CollectionFastDelivery)
	stub := &stubMarketplace{
		resolveResult: marketplace.ResolvedListing{
			SellerListingID:       "listing-123",
			ProductID:             "prod-456",
			StoreID:               "store-789",
			MarketCode:            "EG",
			SKUID:                 "sku-101",
			Quantity:              2,
			UnitPriceMinor:        150000,
			CurrencyCode:          "EGP",
			FulfillmentLocationID: "loc-202",
			ProductTitle:          "Coffee Machine",
			SKUCode:               "CM-BLACK",
			Attribution: marketplace.MarketplaceAttribution{
				SellerListingID:  "listing-123",
				StoreID:          "store-789",
				MarketCode:       "EG",
				SourceCollection: &fastDelivery,
			},
		},
	}
	handler := newTestRouter(Dependencies{Marketplace: stub})

	bodyJSON, _ := json.Marshal(ResolveMarketplaceListingRequest{
		Quantity:         2,
		SKUID:            "sku-101",
		SourceCollection: "fast_delivery",
	})
	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/markets/EG/marketplace/listings/listing-123/resolve", "platform", testPlatformToken)
	req.Body = http.NoBody
	req = httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/listings/listing-123/resolve", bytes.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+testPlatformToken)
	req.Header.Set("X-Matjero-Service", "platform")
	req.Header.Set("Content-Type", "application/json")

	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp ResolveMarketplaceListingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.SellerListingID != "listing-123" || resp.StoreID != "store-789" || resp.UnitPriceMinor != 150000 || resp.Quantity != 2 {
		t.Fatalf("unexpected response payload: %+v", resp)
	}
	if resp.Attribution.SellerListingID != "listing-123" || resp.Attribution.StoreID != "store-789" || resp.Attribution.SourceCollection == nil || *resp.Attribution.SourceCollection != "fast_delivery" {
		t.Fatalf("unexpected attribution: %+v", resp.Attribution)
	}
}

func TestResolveMarketplaceListingErrorMappings(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not found",
			err:        marketplace.ErrListingNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   CodeNotFound,
		},
		{
			name:       "cross market",
			err:        marketplace.ErrCrossMarketAccess,
			wantStatus: http.StatusConflict,
			wantCode:   CodeMarketMismatch,
		},
		{
			name:       "not published",
			err:        marketplace.ErrListingNotPublished,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "product unavailable",
			err:        marketplace.ErrProductUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "price unavailable",
			err:        marketplace.ErrPriceUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodePriceChanged,
		},
		{
			name:       "no eligible location",
			err:        marketplace.ErrNoEligibleLocation,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "inventory unavailable",
			err:        marketplace.ErrInventoryUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodeInsufficientInventory,
		},
		{
			name:       "store mismatch",
			err:        marketplace.ErrStoreMismatch,
			wantStatus: http.StatusConflict,
			wantCode:   CodeConflict,
		},
		{
			name:       "quantity invalid",
			err:        marketplace.ErrQuantityInvalid,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
		{
			name:       "invalid input",
			err:        marketplace.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubMarketplace{resolveErr: tt.err}
			handler := newTestRouter(Dependencies{Marketplace: stub})
			req := authenticatedRequest(t, http.MethodPost, "/internal/v1/markets/EG/marketplace/listings/listing-123/resolve", "platform", testPlatformToken)
			rec := doRequest(t, handler, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			errResp := decodeError(t, rec)
			if errResp.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", errResp.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestAddMarketplaceCartItemRequiresServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{Marketplace: &stubMarketplace{}})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader([]byte(`{"seller_listing_id":"list-1"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAddMarketplaceCartItemRestrictedToPlatformCaller(t *testing.T) {
	stub := &stubMarketplace{
		resolveErr: marketplace.ErrListingNotFound, // will reach handler only if authorized
	}
	handler := newTestRouter(Dependencies{Marketplace: stub})

	bodyJSON, _ := json.Marshal(MarketplaceCartAddItemRequest{
		SellerListingID: "listing-123",
		Quantity:        1,
	})

	tests := []struct {
		name       string
		caller     string
		token      string
		wantStatus int
	}{
		{name: "platform caller allowed", caller: "platform", token: testPlatformToken, wantStatus: http.StatusNotFound},
		{name: "seller caller rejected", caller: "seller", token: testSellerToken, wantStatus: http.StatusForbidden},
		{name: "admin caller rejected", caller: "admin", token: testAdminToken, wantStatus: http.StatusForbidden},
		{name: "supplier caller rejected", caller: "supplier", token: testSupplierToken, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(bodyJSON))
			req.Header.Set("Authorization", "Bearer "+tt.token)
			req.Header.Set("X-Matjero-Service", tt.caller)
			req.Header.Set("Content-Type", "application/json")

			rec := doRequest(t, handler, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestAddMarketplaceCartItemResolutionErrorMappings(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "listing not found",
			err:        marketplace.ErrListingNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   CodeNotFound,
		},
		{
			name:       "cross market",
			err:        marketplace.ErrCrossMarketAccess,
			wantStatus: http.StatusConflict,
			wantCode:   CodeMarketMismatch,
		},
		{
			name:       "listing not published",
			err:        marketplace.ErrListingNotPublished,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "product unavailable",
			err:        marketplace.ErrProductUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "price unavailable",
			err:        marketplace.ErrPriceUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodePriceChanged,
		},
		{
			name:       "no eligible location",
			err:        marketplace.ErrNoEligibleLocation,
			wantStatus: http.StatusConflict,
			wantCode:   CodeListingUnavailable,
		},
		{
			name:       "inventory unavailable",
			err:        marketplace.ErrInventoryUnavailable,
			wantStatus: http.StatusConflict,
			wantCode:   CodeInsufficientInventory,
		},
		{
			name:       "quantity invalid",
			err:        marketplace.ErrQuantityInvalid,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
		{
			name:       "invalid input",
			err:        marketplace.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubMarketplace{resolveErr: tt.err}
			handler := newTestRouter(Dependencies{Marketplace: stub})
			bodyJSON, _ := json.Marshal(MarketplaceCartAddItemRequest{
				SellerListingID: "listing-123",
				Quantity:        1,
			})
			req := httptest.NewRequest(http.MethodPost, "/internal/v1/markets/EG/marketplace/carts/items", bytes.NewReader(bodyJSON))
			req.Header.Set("Authorization", "Bearer "+testPlatformToken)
			req.Header.Set("X-Matjero-Service", "platform")
			req.Header.Set("Content-Type", "application/json")

			rec := doRequest(t, handler, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			errResp := decodeError(t, rec)
			if errResp.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", errResp.Error.Code, tt.wantCode)
			}
		})
	}
}
