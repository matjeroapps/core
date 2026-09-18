package coreapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/modules/storefront"
	"core/modules/themes"
	"core/packages/money"
)

// Storefront revision behaviour through the network boundary.
//
// The revision endpoint is what a caching actor probes before it trusts a cached
// payload, so these tests assert the two properties that keep such a cache
// correct: the generation is scoped to the tenant resolved from the trusted host
// and nothing else, and it stops resolving the moment the store stops being
// public.

func (e integrationEnv) revision(t *testing.T, host string) int64 {
	t.Helper()
	rec := e.get(t, "/internal/v1/storefront/revision", "seller", host)
	if rec.Code != http.StatusOK {
		t.Fatalf("revision status = %d (body %q)", rec.Code, rec.Body.String())
	}
	return decodeInto[StorefrontRevisionResponse](t, rec).Revision
}

func TestIntegrationStorefrontRevisionIsHostScoped(t *testing.T) {
	env := setupIntegration(t)

	revisionA := env.revision(t, env.domainA)
	revisionB := env.revision(t, env.domainB)
	if revisionA < 1 || revisionB < 1 {
		t.Fatalf("revisions = (%d, %d), want both at least 1", revisionA, revisionB)
	}

	// Store A's own write must move store A only. Two hosts sharing a generation
	// would let one store's cache be served to the other after any write.
	if _, err := env.repo.SetSellerListingPrice(env.ctx, env.listingA.ID, money.MustNew(12345, "EGP")); err != nil {
		t.Fatalf("reprice store A listing: %v", err)
	}

	if got := env.revision(t, env.domainA); got <= revisionA {
		t.Errorf("store A revision = %d, want it to advance past %d", got, revisionA)
	}
	if got := env.revision(t, env.domainB); got != revisionB {
		t.Errorf("store B revision = %d, want it unchanged at %d", got, revisionB)
	}
}

// A store that stopped resolving publicly must not yield a revision, which is
// what stops a cache from continuing to serve it.
func TestIntegrationStorefrontRevisionFailsForUnavailableStorefront(t *testing.T) {
	env := setupIntegration(t)

	for _, host := range []string{
		"",                     // no forwarded host at all
		"unknown.matjero.test", // unregistered domain
		"store-c.matjero.test", // active domain, inactive store
	} {
		rec := env.get(t, "/internal/v1/storefront/revision", "seller", host)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("host %q: status = %d, want 404 (body %q)", host, rec.Code, rec.Body.String())
		}
		if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
			t.Errorf("host %q: error code = %q, want %q", host, got, CodeStorefrontUnavailable)
		}
	}
}

// Suspending a store must be observable immediately through the probe, even
// though a cached payload for it may still exist downstream.
func TestIntegrationStorefrontRevisionStopsResolvingWhenStoreIsSuspended(t *testing.T) {
	env := setupIntegration(t)

	if got := env.revision(t, env.domainA); got < 1 {
		t.Fatalf("revision = %d, want at least 1", got)
	}
	if err := env.repo.UpdateStoreStatus(env.ctx, env.storeA.ID, "inactive"); err != nil {
		t.Fatalf("suspend store A: %v", err)
	}

	rec := env.get(t, "/internal/v1/storefront/revision", "seller", env.domainA)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("suspended store status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
		t.Errorf("error code = %q, want %q", got, CodeStorefrontUnavailable)
	}
}

// A client-supplied tenant selector must never influence which store's generation
// is returned.
func TestIntegrationStorefrontRevisionIgnoresClientSuppliedTenantSelectors(t *testing.T) {
	env := setupIntegration(t)

	want := env.revision(t, env.domainA)
	req := authenticatedRequest(t, http.MethodGet,
		"/internal/v1/storefront/revision?store_id="+env.storeB.ID+"&seller_id="+env.sellerB.ID, "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, env.domainA)
	// A hostile Host and X-Forwarded-Host must be ignored entirely.
	req.Host = "store-b.matjero.test"
	req.Header.Set("X-Forwarded-Host", "store-b.matjero.test")
	req.Header.Set("X-Store-ID", env.storeB.ID)
	rec := doRequest(t, env.handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := decodeInto[StorefrontRevisionResponse](t, rec).Revision; got != want {
		t.Fatalf("revision = %d, want store A's %d; a client selector changed the tenant", got, want)
	}
}

// Every successful public read must be labelled with the generation it belongs
// to, so a cache stores each payload under the generation that produced it rather
// than one it probed earlier.
func TestIntegrationStorefrontReadsCarryRevisionHeader(t *testing.T) {
	env := setupIntegration(t)

	want := env.revision(t, env.domainA)
	for _, path := range []string{
		"/internal/v1/storefront/store",
		"/internal/v1/storefront/categories",
		"/internal/v1/storefront/categories/store-a-lighting",
		"/internal/v1/storefront/products",
		"/internal/v1/storefront/products/store-a-desk-lamp",
		"/internal/v1/storefront/search?q=lamp",
	} {
		rec := env.get(t, path, "seller", env.domainA)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d (body %q)", path, rec.Code, rec.Body.String())
		}
		header := rec.Header().Get(HeaderStorefrontRevision)
		got, err := strconv.ParseInt(header, 10, 64)
		if err != nil {
			t.Fatalf("%s: revision header = %q, want an integer", path, header)
		}
		if got != want {
			t.Errorf("%s: revision header = %d, want %d", path, got, want)
		}
	}
}

// A read labelled with a store's generation must never be labelled with
// another's, and a write must move the label forward.
func TestIntegrationStorefrontReadRevisionHeaderTracksItsStore(t *testing.T) {
	env := setupIntegration(t)

	headerFor := func(host string) string {
		rec := env.get(t, "/internal/v1/storefront/store", "seller", host)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %q: status = %d (body %q)", host, rec.Code, rec.Body.String())
		}
		return rec.Header().Get(HeaderStorefrontRevision)
	}

	beforeA, beforeB := headerFor(env.domainA), headerFor(env.domainB)
	if err := env.repo.UpdateSellerListingStatus(env.ctx, env.listingA.ID, "draft"); err != nil {
		t.Fatalf("disable store A listing: %v", err)
	}

	if got := headerFor(env.domainA); got == beforeA {
		t.Errorf("store A revision header = %q, want it to advance past %q", got, beforeA)
	}
	if got := headerFor(env.domainB); got != beforeB {
		t.Errorf("store B revision header = %q, want it unchanged at %q", got, beforeB)
	}
}

// An unavailable or rejected read must disclose no generation at all.
func TestIntegrationStorefrontFailedReadsCarryNoRevisionHeader(t *testing.T) {
	env := setupIntegration(t)

	for _, tc := range []struct {
		path string
		host string
	}{
		{"/internal/v1/storefront/store", "unknown.matjero.test"},
		{"/internal/v1/storefront/products?limit=abc", env.domainA},
		{"/internal/v1/storefront/products/no-such-product", env.domainA},
	} {
		rec := env.get(t, tc.path, "seller", tc.host)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s: unexpected 200", tc.path)
		}
		if got := rec.Header().Get(HeaderStorefrontRevision); got != "" {
			t.Errorf("%s: failed read carried revision header %q", tc.path, got)
		}
	}
}

// A theme publish is a public output change, so it must move the generation while
// the draft edit that preceded it does not.
func TestIntegrationStorefrontRevisionTracksThemePublish(t *testing.T) {
	env := setupIntegration(t)

	// The theme service is exercised directly: its own HTTP surface is covered
	// elsewhere, and what matters here is that a theme write is observable through
	// the revision probe a cache relies on.
	svc := themes.NewService(themes.NewRepository(env.db.Pool), env.repo, themes.Options{
		PreviewSecret: []byte("integration-preview-secret"),
	})
	if _, err := svc.Install(env.ctx, env.storeA.SellerID, env.storeA.ID, themes.DefaultThemeKey, themes.DefaultThemeVersion); err != nil {
		t.Fatalf("install theme: %v", err)
	}

	afterInstall := env.revision(t, env.domainA)
	revisionB := env.revision(t, env.domainB)

	if _, err := svc.UpdateDraftConfiguration(env.ctx, env.storeA.SellerID, env.storeA.ID, map[string]any{
		"logo": "https://cdn.matjero.test/draft.png",
	}); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if got := env.revision(t, env.domainA); got != afterInstall {
		t.Fatalf("draft edit moved the revision (%d -> %d); drafts are not public", afterInstall, got)
	}

	if _, err := svc.PublishConfiguration(env.ctx, env.storeA.SellerID, env.storeA.ID); err != nil {
		t.Fatalf("publish configuration: %v", err)
	}
	if got := env.revision(t, env.domainA); got <= afterInstall {
		t.Fatalf("publish did not move the revision (%d -> %d)", afterInstall, got)
	}
	if got := env.revision(t, env.domainB); got != revisionB {
		t.Fatalf("store A theme publish moved store B (%d -> %d)", revisionB, got)
	}
}

// The revision endpoint must sit behind the same service authentication and
// caller scoping as the rest of the public storefront namespace.
func TestStorefrontRevisionRequiresSellerServiceAuth(t *testing.T) {
	handler := newTestRouter(Dependencies{
		Stores:    &stubStores{},
		Revisions: &stubRevisions{revision: 7},
	})

	t.Run("unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/storefront/revision", nil)
		req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")
		rec := doRequest(t, handler, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("wrong token for caller", func(t *testing.T) {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/revision", "seller", testAdminToken)
		req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")
		rec := doRequest(t, handler, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %q)", rec.Code, rec.Body.String())
		}
	})

	for _, caller := range []struct{ name, token string }{
		{"admin", testAdminToken},
		{"supplier", testSupplierToken},
	} {
		t.Run(caller.name+" is not a storefront caller", func(t *testing.T) {
			req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/revision", caller.name, caller.token)
			req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")
			rec := doRequest(t, handler, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
			}
			if got := decodeError(t, rec).Error.Code; got != CodeForbidden {
				t.Errorf("error code = %q, want %q", got, CodeForbidden)
			}
		})
	}
}

// Tenant authority for the probe comes from the trusted internal header only.
func TestStorefrontRevisionResolvesTenantFromTrustedHeaderOnly(t *testing.T) {
	revisions := &stubRevisions{revision: 42}
	handler := newTestRouter(Dependencies{Stores: &stubStores{}, Revisions: revisions})

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/revision", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, "Store-A.Matjero.Test:443")
	req.Host = "evil.example.com"
	req.Header.Set("X-Forwarded-Host", "evil.example.com")
	rec := doRequest(t, handler, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if revisions.gotHost != "Store-A.Matjero.Test:443" {
		t.Fatalf("probed host = %q, want the forwarded internal header verbatim", revisions.gotHost)
	}
	if got := decodeInto[StorefrontRevisionResponse](t, rec).Revision; got != 42 {
		t.Fatalf("revision = %d, want 42", got)
	}
}

// A revision lookup failure must not degrade into serving an unlabelled payload.
func TestStorefrontReadFailsWhenRevisionIsUnavailable(t *testing.T) {
	handler := newTestRouter(Dependencies{
		Stores: &stubStores{resolved: storefront.ResolvedStore{
			Store:       commerce.Store{ID: "11111111-1111-1111-1111-111111111111", MarketCode: "EG"},
			StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.test"},
		}},
		Revisions: &stubRevisions{err: errBoom},
	})

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")
	rec := doRequest(t, handler, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(HeaderStorefrontRevision); got != "" {
		t.Errorf("failed read carried revision header %q", got)
	}
	if got := decodeError(t, rec).Error.Code; got != CodeInternalError {
		t.Errorf("error code = %q, want %q", got, CodeInternalError)
	}
}
