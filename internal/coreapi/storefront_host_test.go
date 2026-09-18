package coreapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"core/internal/serviceauth"
)

func sellerRequest(t *testing.T, method, path, subject string) *http.Request {
	t.Helper()
	req := authenticatedRequest(t, method, path, "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderSubject, subject)
	return req
}

func setupStorefrontHostTestEnv(t *testing.T) (integrationEnv, string, string) {
	t.Helper()
	env := setupIntegration(t)

	subjectA := "subject-seller-a"
	subjectB := "subject-seller-b"

	if _, err := env.repo.CreateSellerMember(env.ctx, env.sellerA.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("create seller member A: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(env.ctx, env.sellerB.ID, subjectB, "owner", "active"); err != nil {
		t.Fatalf("create seller member B: %v", err)
	}

	return env, subjectA, subjectB
}

func TestIntegrationStorefrontHostDiscovery(t *testing.T) {
	env, subjectA, subjectB := setupStorefrontHostTestEnv(t)

	t.Run("authorized seller receives storefront host", func(t *testing.T) {
		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeA.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body %q)", rec.Code, rec.Body.String())
		}
		resp := decodeInto[StorefrontHostResponse](t, rec)
		if resp.Host != env.domainA {
			t.Errorf("host = %q, want %q", resp.Host, env.domainA)
		}
	})

	t.Run("cross-seller authorization returns safe 404", func(t *testing.T) {
		// Seller B attempting to discover Store A's storefront host
		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeA.ID+"/storefront-host", subjectB)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-seller store lookup, got %d (body %q)", rec.Code, rec.Body.String())
		}
		errResp := decodeInto[ErrorResponse](t, rec)
		if errResp.Error.Code != CodeNotFound {
			t.Errorf("error code = %q, want %q", errResp.Error.Code, CodeNotFound)
		}
	})

	t.Run("admin can resolve any store host", func(t *testing.T) {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeA.ID+"/storefront-host", "admin", testAdminToken)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin, got %d (body %q)", rec.Code, rec.Body.String())
		}
		resp := decodeInto[StorefrontHostResponse](t, rec)
		if resp.Host != env.domainA {
			t.Errorf("host = %q, want %q", resp.Host, env.domainA)
		}
	})

	t.Run("unknown store returns 404", func(t *testing.T) {
		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/00000000-0000-0000-0000-000000000000/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown store, got %d", rec.Code)
		}
	})

	t.Run("supplier caller is rejected", func(t *testing.T) {
		req := authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeA.ID+"/storefront-host", "supplier", testSupplierToken)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for supplier caller, got %d", rec.Code)
		}
	})
}

func TestIntegrationStorefrontHostDomainSelectionRules(t *testing.T) {
	env, subjectA, _ := setupStorefrontHostTestEnv(t)
	ctx := env.ctx

	sellerRules, err := env.repo.CreateSeller(ctx, "seller-rules", "Rules Seller", "active", nil)
	if err != nil {
		t.Fatalf("create rules seller: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(ctx, sellerRules.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("create rules seller member: %v", err)
	}
	store, _, err := env.repo.CreateStoreWithDomain(ctx, sellerRules.ID, "EG", "store-rules", "Rules Store", "active", nil, "platform-secondary.matjero.test", "platform", "active", false, nil, nil)
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}

	t.Run("active secondary domain alone returns 404", func(t *testing.T) {
		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+store.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when store has no active primary domain, got %d (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("determinism: custom active primary preferred over platform secondary", func(t *testing.T) {
		now := time.Now()
		customDomain, err := env.repo.CreateStoreDomain(ctx, store.ID, "custom-primary.example.com", "custom", "active", true, &now, nil)
		if err != nil {
			t.Fatalf("create custom primary domain: %v", err)
		}

		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+store.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body %q)", rec.Code, rec.Body.String())
		}
		resp := decodeInto[StorefrontHostResponse](t, rec)
		if resp.Host != customDomain.Domain {
			t.Errorf("host = %q, want %q", resp.Host, customDomain.Domain)
		}
	})

	t.Run("pending primary domain is ignored", func(t *testing.T) {
		sellerP, err := env.repo.CreateSeller(ctx, "seller-pending", "Pending Seller", "active", nil)
		if err != nil {
			t.Fatalf("create pending seller: %v", err)
		}
		if _, err := env.repo.CreateSellerMember(ctx, sellerP.ID, subjectA, "owner", "active"); err != nil {
			t.Fatalf("create pending seller member: %v", err)
		}
		storePending, _, err := env.repo.CreateStoreWithDomain(ctx, sellerP.ID, "EG", "store-pending", "Pending Store", "active", nil, "pending-primary.example.com", "custom", "pending", true, nil, nil)
		if err != nil {
			t.Fatalf("create pending store: %v", err)
		}

		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+storePending.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for pending primary domain, got %d (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("disabled primary domain is ignored", func(t *testing.T) {
		sellerD, err := env.repo.CreateSeller(ctx, "seller-disabled", "Disabled Seller", "active", nil)
		if err != nil {
			t.Fatalf("create disabled seller: %v", err)
		}
		if _, err := env.repo.CreateSellerMember(ctx, sellerD.ID, subjectA, "owner", "active"); err != nil {
			t.Fatalf("create disabled seller member: %v", err)
		}
		storeDisabled, _, err := env.repo.CreateStoreWithDomain(ctx, sellerD.ID, "EG", "store-disabled", "Disabled Store", "active", nil, "disabled-primary.example.com", "custom", "disabled", true, nil, nil)
		if err != nil {
			t.Fatalf("create disabled store: %v", err)
		}

		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+storeDisabled.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled primary domain, got %d (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("verified but not active primary domain is ignored", func(t *testing.T) {
		sellerV, err := env.repo.CreateSeller(ctx, "seller-verified", "Verified Seller", "active", nil)
		if err != nil {
			t.Fatalf("create verified seller: %v", err)
		}
		if _, err := env.repo.CreateSellerMember(ctx, sellerV.ID, subjectA, "owner", "active"); err != nil {
			t.Fatalf("create verified seller member: %v", err)
		}
		now := time.Now()
		storeVerified, _, err := env.repo.CreateStoreWithDomain(ctx, sellerV.ID, "EG", "store-verified", "Verified Store", "active", nil, "verified-primary.example.com", "custom", "verified", true, &now, nil)
		if err != nil {
			t.Fatalf("create verified store: %v", err)
		}

		req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+storeVerified.ID+"/storefront-host", subjectA)
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for verified (non-active) primary domain, got %d (body %q)", rec.Code, rec.Body.String())
		}
	})
}

func TestIntegrationStorefrontHostLifecyclePromotion(t *testing.T) {
	env, subjectA, _ := setupStorefrontHostTestEnv(t)
	ctx := env.ctx

	// 1. Create store with platform domain active + primary
	sellerPromo, err := env.repo.CreateSeller(ctx, "seller-promo", "Promo Seller", "active", nil)
	if err != nil {
		t.Fatalf("create promo seller: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(ctx, sellerPromo.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("create promo seller member: %v", err)
	}
	platformDomainStr := "store-promo.matjero.test"
	store, platformDomain, err := env.repo.CreateStoreWithDomain(ctx, sellerPromo.ID, "EG", "store-promo", "Promo Store", "active", nil, platformDomainStr, "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatalf("create store with platform domain: %v", err)
	}

	// Verify initially returns platform domain
	req1 := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+store.ID+"/storefront-host", subjectA)
	rec1 := httptest.NewRecorder()
	env.handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("initial get expected 200, got %d", rec1.Code)
	}
	if resp := decodeInto[StorefrontHostResponse](t, rec1); resp.Host != platformDomainStr {
		t.Fatalf("initial host = %q, want %q", resp.Host, platformDomainStr)
	}

	// 2. Demote platform domain to secondary first, so adding active primary custom domain satisfies one_primary_per_store constraint
	if _, err := env.db.Pool.Exec(ctx, `UPDATE store_domains SET is_primary = false WHERE id = $1`, platformDomain.ID); err != nil {
		t.Fatalf("demote platform domain: %v", err)
	}

	now := time.Now()
	customDomainStr := "brand.example.com"
	_, err = env.repo.CreateStoreDomain(ctx, store.ID, customDomainStr, "custom", "active", true, &now, nil)
	if err != nil {
		t.Fatalf("create custom domain: %v", err)
	}

	// Verify endpoint automatically begins returning promoted custom domain
	req2 := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+store.ID+"/storefront-host", subjectA)
	rec2 := httptest.NewRecorder()
	env.handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("promoted get expected 200, got %d", rec2.Code)
	}
	if resp := decodeInto[StorefrontHostResponse](t, rec2); resp.Host != customDomainStr {
		t.Fatalf("promoted host = %q, want %q", resp.Host, customDomainStr)
	}
}

func TestIntegrationStorefrontHostResponsePrivacy(t *testing.T) {
	env, subjectA, _ := setupStorefrontHostTestEnv(t)

	req := sellerRequest(t, http.MethodGet, "/internal/v1/stores/"+env.storeA.ID+"/storefront-host", subjectA)
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var raw map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw json response: %v", err)
	}

	if len(raw) != 1 {
		t.Errorf("response contains %d fields, want exactly 1 (raw: %v)", len(raw), raw)
	}

	if _, ok := raw["host"]; !ok {
		t.Errorf("response missing 'host' key (raw: %v)", raw)
	}

	forbiddenKeys := []string{
		"id", "store_id", "seller_id", "domain_type", "status",
		"is_primary", "verification_token", "verified_at", "last_checked_at",
		"created_at", "updated_at",
	}
	for _, key := range forbiddenKeys {
		if _, found := raw[key]; found {
			t.Errorf("response leaks sensitive field %q", key)
		}
	}
}
