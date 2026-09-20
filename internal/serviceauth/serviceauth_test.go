package serviceauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	platformToken = "pl4tform-s3cr3t"
	sellerToken   = "s3ll3r-s3cr3t"
	adminToken    = "4dm1n-s3cr3t"
)

func testConfig() Config {
	return Config{Tokens: map[Caller]string{
		CallerPlatform: platformToken,
		CallerSeller:   sellerToken,
		CallerAdmin:    adminToken,
		CallerSupplier: "",
	}}
}

func request(caller, token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/markets", nil)
	if caller != "" {
		req.Header.Set(HeaderService, caller)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestAuthenticateAcceptsMatchingCallerAndToken(t *testing.T) {
	caller, err := Authenticate(request("platform", platformToken), testConfig())
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if caller != CallerPlatform {
		t.Fatalf("caller = %q, want %q", caller, CallerPlatform)
	}
}

func TestAuthenticateRejectsMismatches(t *testing.T) {
	cases := []struct {
		name   string
		caller string
		token  string
	}{
		{"no credentials", "", ""},
		{"caller without token", "seller", ""},
		{"token without caller", "", sellerToken},
		{"unknown caller", "attacker", sellerToken},
		{"seller token as platform", "platform", sellerToken},
		{"platform token as seller", "seller", platformToken},
		{"seller token as admin", "admin", sellerToken},
		{"admin token as seller", "seller", adminToken},
		{"caller with no configured token", "supplier", sellerToken},
		{"wrong token", "seller", "wrong"},
		{"empty bearer", "seller", " "},
		{"basic scheme", "seller", sellerToken},
		{"case-sensitive caller", "SELLER", sellerToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := request(tc.caller, tc.token)
			if tc.name == "basic scheme" {
				req.Header.Set("Authorization", "Basic "+sellerToken)
			}
			if _, err := Authenticate(req, testConfig()); err == nil {
				t.Fatal("expected authentication to fail")
			}
		})
	}
}

// A token must not be accepted for a caller it was not issued to, so a
// compromised actor credential cannot borrow another actor's authorization.
func TestTokenIsBoundToCaller(t *testing.T) {
	if _, err := Authenticate(request("admin", sellerToken), testConfig()); err == nil {
		t.Fatal("seller token must not authenticate as admin")
	}
	if _, err := Authenticate(request("seller", adminToken), testConfig()); err == nil {
		t.Fatal("admin token must not authenticate as seller")
	}
}

func TestMiddlewareRejectsUnauthenticated(t *testing.T) {
	handler := Middleware(testConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not run for an unauthenticated request")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request("seller", "wrong"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unauthorized") {
		t.Errorf("body = %q, want the unauthorized code", body)
	}
	for _, secret := range []string{sellerToken, adminToken} {
		if strings.Contains(body, secret) {
			t.Errorf("body leaked a token: %q", body)
		}
	}
}

func TestMiddlewareBindsCallerToContext(t *testing.T) {
	var seen Caller
	handler := Middleware(testConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = CallerFrom(r.Context())
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request("admin", adminToken))

	if seen != CallerAdmin {
		t.Fatalf("caller in context = %q, want %q", seen, CallerAdmin)
	}
}

func TestRequireCallerNarrowsAuthorization(t *testing.T) {
	handler := Middleware(testConfig())(
		RequireCaller(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}), CallerAdmin),
	)

	t.Run("allowed caller", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request("admin", adminToken))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("disallowed caller", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request("seller", sellerToken))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestConfigEnabled(t *testing.T) {
	cases := []struct {
		name   string
		config Config
		want   bool
	}{
		{"no tokens", Config{}, false},
		{"empty map", Config{Tokens: map[Caller]string{}}, false},
		{"all blank", Config{Tokens: map[Caller]string{CallerAdmin: ""}}, false},
		{"one configured", Config{Tokens: map[Caller]string{CallerAdmin: "x"}}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.config.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCallersWithTokensIsSortedAndOmitsBlank(t *testing.T) {
	got := testConfig().CallersWithTokens()
	want := []Caller{CallerAdmin, CallerPlatform, CallerSeller}
	if len(got) != len(want) {
		t.Fatalf("callers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("callers = %v, want %v", got, want)
		}
	}
}

func TestHeaderAccessorsTrimAndReadTrustedValues(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/storefront/store", nil)
	req.Header.Set(HeaderSubject, "  user-123  ")
	req.Header.Set(HeaderStorefrontHost, "  store-a.matjero.test  ")

	if got := SubjectFrom(req); got != "user-123" {
		t.Errorf("subject = %q, want %q", got, "user-123")
	}
	if got := StorefrontHostFrom(req); got != "store-a.matjero.test" {
		t.Errorf("host = %q, want %q", got, "store-a.matjero.test")
	}
}

func TestCallerValid(t *testing.T) {
	for _, caller := range []Caller{CallerAdmin, CallerPlatform, CallerSeller, CallerSupplier} {
		if !caller.Valid() {
			t.Errorf("%q should be valid", caller)
		}
	}
	for _, caller := range []Caller{"", "attacker", "ADMIN", "storefront"} {
		if caller.Valid() {
			t.Errorf("%q should be invalid", caller)
		}
	}
}
