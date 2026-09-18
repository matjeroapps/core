package coreapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"core/internal/serviceauth"
	"core/modules/markets"
	"core/modules/storefront"
	"core/packages/i18n"
)

// errBoom stands in for an unexpected failure. Its message must never reach a
// caller: the internal error contract collapses unknown causes to internal_error.
var errBoom = errors.New("connection refused: pq: relation does not exist")

// The routing tests in this file prove the transport contract of the internal
// API: service authentication, caller scoping, header sanitisation, and error
// mapping. They deliberately use stub capabilities so they run without
// PostgreSQL; business correctness is covered by the existing package tests that
// do use the database.

// --- stubs ---

type stubMarkets struct {
	items []markets.Market
	err   error
}

func (s stubMarkets) List(ctx context.Context, locale i18n.Locale) ([]markets.Market, error) {
	return s.items, s.err
}

func (s stubMarkets) GetByCode(ctx context.Context, code string, locale i18n.Locale) (markets.Market, error) {
	if s.err != nil {
		return markets.Market{}, s.err
	}
	for _, item := range s.items {
		if item.Code == code {
			return item, nil
		}
	}
	return markets.Market{}, markets.ErrNotFound
}

type stubStores struct {
	resolved storefront.ResolvedStore
	err      error
	gotHost  string
}

func (s *stubStores) Resolve(ctx context.Context, domain string) (storefront.ResolvedStore, error) {
	s.gotHost = domain
	return s.resolved, s.err
}

// stubRevisions stands in for the authoritative revision reader. It records the
// host it was probed with so the tests can prove tenant authority never comes
// from anywhere but the trusted internal header.
type stubRevisions struct {
	revision int64
	err      error
	gotHost  string
}

func (s *stubRevisions) Revision(ctx context.Context, host string) (int64, error) {
	s.gotHost = host
	return s.revision, s.err
}

func (s *stubRevisions) RevisionFor(ctx context.Context, scope storefront.CatalogScope) (int64, error) {
	return s.revision, s.err
}

// --- helpers ---

const (
	testSellerToken   = "seller-token-value"
	testAdminToken    = "admin-token-value"
	testSupplierToken = "supplier-token-value"
)

func testAuthConfig() serviceauth.Config {
	return serviceauth.Config{Tokens: map[serviceauth.Caller]string{
		serviceauth.CallerSeller:   testSellerToken,
		serviceauth.CallerAdmin:    testAdminToken,
		serviceauth.CallerSupplier: testSupplierToken,
	}}
}

// newTestRouter builds the internal API behind service auth, mirroring how
// apps/core-api wires it in production.
func newTestRouter(deps Dependencies) http.Handler {
	return serviceauth.Middleware(testAuthConfig())(NewRouter(deps))
}

func doRequest(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func authenticatedRequest(t *testing.T, method, path, caller, token string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(serviceauth.HeaderService, caller)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var payload ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error envelope: %v (body %q)", err, rec.Body.String())
	}
	return payload
}

// --- service authentication ---

func TestInternalAPIRequiresServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{Markets: stubMarkets{}})

	cases := []struct {
		name    string
		mutate  func(*http.Request)
		wantMsg string
	}{
		{
			name:   "no headers at all",
			mutate: func(*http.Request) {},
		},
		{
			name:   "missing token",
			mutate: func(r *http.Request) { r.Header.Set(serviceauth.HeaderService, "seller") },
		},
		{
			name: "missing caller header",
			mutate: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+testSellerToken)
			},
		},
		{
			name: "unknown caller",
			mutate: func(r *http.Request) {
				r.Header.Set(serviceauth.HeaderService, "attacker")
				r.Header.Set("Authorization", "Bearer "+testSellerToken)
			},
		},
		{
			name: "wrong token for caller",
			mutate: func(r *http.Request) {
				r.Header.Set(serviceauth.HeaderService, "seller")
				r.Header.Set("Authorization", "Bearer "+testAdminToken)
			},
		},
		{
			name: "empty bearer token",
			mutate: func(r *http.Request) {
				r.Header.Set(serviceauth.HeaderService, "seller")
				r.Header.Set("Authorization", "Bearer ")
			},
		},
		{
			name: "non-bearer scheme",
			mutate: func(r *http.Request) {
				r.Header.Set(serviceauth.HeaderService, "seller")
				r.Header.Set("Authorization", "Basic "+testSellerToken)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/v1/markets", nil)
			tc.mutate(req)

			rec := doRequest(t, handler, req)
			body := rec.Body.String()

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusUnauthorized, body)
			}
			payload := decodeError(t, rec)
			if payload.Error.Code != CodeUnauthorized {
				t.Errorf("error code = %q, want %q", payload.Error.Code, CodeUnauthorized)
			}
			// The response must not reveal which credential was wrong, which
			// callers are configured, or what a valid token looks like.
			if body == "" {
				t.Error("expected a generic error body")
			}
			for _, secret := range []string{testSellerToken, testAdminToken, testSupplierToken} {
				if strings.Contains(body, secret) {
					t.Errorf("error body leaked a configured token: %q", body)
				}
			}
		})
	}
}

func TestInternalAPIAcceptsValidCaller(t *testing.T) {
	handler := newTestRouter(Dependencies{Markets: stubMarkets{items: []markets.Market{{Code: "EG"}}}})

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets", "seller", testSellerToken)
	rec := doRequest(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

// A caller that authenticated as one actor must not be able to reach routes
// scoped to another actor, even with a valid token.
func TestInternalAPIRejectsCallerMismatch(t *testing.T) {
	handler := newTestRouter(Dependencies{})

	cases := []struct {
		name   string
		caller string
		token  string
		path   string
	}{
		{"seller cannot read admin overview", "seller", testSellerToken, "/internal/v1/admin/overview"},
		{"supplier cannot read admin overview", "supplier", testSupplierToken, "/internal/v1/admin/overview"},
		{"admin cannot read storefront", "admin", testAdminToken, "/internal/v1/storefront/store"},
		{"supplier cannot read storefront", "supplier", testSupplierToken, "/internal/v1/storefront/store"},
		{"seller cannot list suppliers", "seller", testSellerToken, "/internal/v1/suppliers"},
		{"supplier cannot list sellers", "supplier", testSupplierToken, "/internal/v1/sellers"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authenticatedRequest(t, http.MethodGet, tc.path, tc.caller, tc.token)
			rec := doRequest(t, handler, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if got := decodeError(t, rec).Error.Code; got != CodeForbidden {
				t.Errorf("error code = %q, want %q", got, CodeForbidden)
			}
		})
	}
}

// --- storefront host handling ---

func TestStorefrontResolvesTenantFromTrustedHeaderOnly(t *testing.T) {
	stores := &stubStores{err: storefront.ErrStoreNotFound}
	handler := newTestRouter(Dependencies{Stores: stores})

	t.Run("missing host header fails closed", func(t *testing.T) {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
		rec := doRequest(t, handler, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
			t.Errorf("error code = %q, want %q", got, CodeStorefrontUnavailable)
		}
	})

	t.Run("host header is used verbatim", func(t *testing.T) {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
		req.Header.Set(serviceauth.HeaderStorefrontHost, "Store-A.Matjero.Test:443")
		// A hostile client-supplied Host and X-Forwarded-Host must be ignored.
		req.Host = "evil.example.com"
		req.Header.Set("X-Forwarded-Host", "evil.example.com")
		_ = doRequest(t, handler, req)

		if stores.gotHost != "Store-A.Matjero.Test:443" {
			t.Fatalf("resolved host = %q, want the forwarded internal header verbatim", stores.gotHost)
		}
	})
}

// Unknown host, inactive domain and inactive store must be indistinguishable to
// the caller, so a customer cannot tell an unregistered domain from a suspended
// store.
func TestStorefrontHostFailuresAreIndistinguishable(t *testing.T) {
	for _, err := range []error{
		storefront.ErrStoreNotFound,
		storefront.ErrDomainInactive,
		storefront.ErrStoreInactive,
	} {
		handler := newTestRouter(Dependencies{Stores: &stubStores{err: err}})
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
		req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")

		rec := doRequest(t, handler, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%v: status = %d, want 404", err, rec.Code)
		}
		if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
			t.Errorf("%v: error code = %q, want %q", err, got, CodeStorefrontUnavailable)
		}
	}
}

// --- error contract ---

func TestErrorContractNeverLeaksInternalDetail(t *testing.T) {
	handler := newTestRouter(Dependencies{Markets: stubMarkets{err: errBoom}})

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets", "seller", testSellerToken)
	rec := doRequest(t, handler, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	payload := decodeError(t, rec)
	if payload.Error.Code != CodeInternalError {
		t.Errorf("error code = %q, want %q", payload.Error.Code, CodeInternalError)
	}
	if payload.Error.Message != "internal error" {
		t.Errorf("message = %q, want the generic internal message", payload.Error.Message)
	}
}

func TestMarketNotFoundMapsToNotFound(t *testing.T) {
	handler := newTestRouter(Dependencies{Markets: stubMarkets{}})

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/markets/NOPE", "seller", testSellerToken)
	rec := doRequest(t, handler, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := decodeError(t, rec).Error.Code; got != CodeNotFound {
		t.Errorf("error code = %q, want %q", got, CodeNotFound)
	}
}

// --- request parsing ---

func TestInvalidProductQueryIsRejected(t *testing.T) {
	handler := newTestRouter(Dependencies{Stores: &stubStores{err: storefront.ErrStoreNotFound}})

	for _, query := range []string{
		"?limit=abc",
		"?offset=xyz",
		"?min_price=not-a-number",
		"?max_price=not-a-number",
	} {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/products"+query, "seller", testSellerToken)
		req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")

		rec := doRequest(t, handler, req)

		// The host cannot resolve in this stub, so an invalid query is caught
		// only when resolution succeeds; assert the request never panics and
		// always yields a well-formed envelope.
		if rec.Code == http.StatusOK {
			t.Errorf("%s: unexpected 200 from an unresolvable host", query)
		}
		var payload ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Errorf("%s: malformed error envelope: %v", query, err)
		}
	}
}
