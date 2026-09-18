package storefront

import (
	"context"
	"net/http"
	"testing"

	"core/modules/commerce"
	"core/packages/config"
)

// fakeLookup is an in-memory StoreLookup for tests.
type fakeLookup struct {
	byDomain map[string]commerce.StoreDomainResolution
}

func (f fakeLookup) GetStoreByDomain(_ context.Context, domain string) (commerce.StoreDomainResolution, error) {
	res, ok := f.byDomain[domain]
	if !ok {
		return commerce.StoreDomainResolution{}, commerce.ErrNotFound
	}
	return res, nil
}

func storeResolution(storeID, domain, storeStatus, domainStatus string) commerce.StoreDomainResolution {
	return commerce.StoreDomainResolution{
		Store: commerce.Store{
			ID:     storeID,
			Status: storeStatus,
		},
		StoreDomain: commerce.StoreDomain{
			Domain: domain,
			Status: domainStatus,
		},
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Shop.Example.com", "shop.example.com"},
		{"shop.example.com:8080", "shop.example.com"},
		{"  SHOP.EXAMPLE.COM  ", "shop.example.com"},
		{"shop.example.com:443", "shop.example.com"},
		{"localhost:3000", "localhost"},
	}
	for _, tc := range cases {
		if got := NormalizeHost(tc.in); got != tc.want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDomainFromRequest(t *testing.T) {
	trusted := config.Config{TrustedForwardedHost: true}
	untrusted := config.Config{TrustedForwardedHost: false}

	t.Run("uses host header when proxy not trusted", func(t *testing.T) {
		r := &http.Request{Host: "shop.example.com", Header: http.Header{}}
		r.Header.Set("X-Forwarded-Host", "evil.example.com")
		if got := DomainFromRequest(r, untrusted); got != "shop.example.com" {
			t.Errorf("got %q, want shop.example.com", got)
		}
	})

	t.Run("honors forwarded host when proxy trusted", func(t *testing.T) {
		r := &http.Request{Host: "shop.example.com", Header: http.Header{}}
		r.Header.Set("X-Forwarded-Host", "evil.example.com")
		if got := DomainFromRequest(r, trusted); got != "evil.example.com" {
			t.Errorf("got %q, want evil.example.com", got)
		}
	})

	t.Run("takes first of comma-separated forwarded hosts", func(t *testing.T) {
		r := &http.Request{Host: "shop.example.com", Header: http.Header{}}
		r.Header.Set("X-Forwarded-Host", "a.example.com, b.example.com")
		if got := DomainFromRequest(r, trusted); got != "a.example.com" {
			t.Errorf("got %q, want a.example.com", got)
		}
	})

	t.Run("strips port from host header", func(t *testing.T) {
		r := &http.Request{Host: "shop.example.com:3000", Header: http.Header{}}
		if got := DomainFromRequest(r, untrusted); got != "shop.example.com" {
			t.Errorf("got %q, want shop.example.com", got)
		}
	})
}

func TestStoreResolverResolve(t *testing.T) {
	lookup := fakeLookup{byDomain: map[string]commerce.StoreDomainResolution{
		"store-a.matjero.com": storeResolution("store-a", "store-a.matjero.com", "active", "active"),
		"store-b.matjero.com": storeResolution("store-b", "store-b.matjero.com", "active", "active"),
		"pending.matjero.com": storeResolution("store-c", "pending.matjero.com", "active", "pending"), "verified.matjero.com": storeResolution("store-f", "verified.matjero.com", "active", "verified"), "inactive.matjero.com": storeResolution("store-d", "inactive.matjero.com", "inactive", "active"),
		"disabled.matjero.com": storeResolution("store-e", "disabled.matjero.com", "active", "disabled"),
	}}
	resolver := NewStoreResolver(lookup)
	ctx := context.Background()

	t.Run("resolves active store", func(t *testing.T) {
		res, err := resolver.Resolve(ctx, "store-a.matjero.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Store.ID != "store-a" {
			t.Errorf("got store %q, want store-a", res.Store.ID)
		}
	})

	t.Run("normalizes host before lookup", func(t *testing.T) {
		res, err := resolver.Resolve(ctx, "STORE-A.MATJERO.COM:443")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Store.ID != "store-a" {
			t.Errorf("got store %q, want store-a", res.Store.ID)
		}
	})

	t.Run("unknown domain fails safe", func(t *testing.T) {
		if _, err := resolver.Resolve(ctx, "unknown.matjero.com"); err != ErrStoreNotFound {
			t.Errorf("got %v, want ErrStoreNotFound", err)
		}
	})

	t.Run("pending domain fails safe", func(t *testing.T) {
		if _, err := resolver.Resolve(ctx, "pending.matjero.com"); err != ErrDomainInactive {
			t.Errorf("got %v, want ErrDomainInactive", err)
		}
	})

	t.Run("disabled domain fails safe", func(t *testing.T) {
		if _, err := resolver.Resolve(ctx, "disabled.matjero.com"); err != ErrDomainInactive {
			t.Errorf("got %v, want ErrDomainInactive", err)
		}
	})

	t.Run("verified-but-not-active domain fails safe", func(t *testing.T) {
		// VERIFIED is a distinct lifecycle state from ACTIVE: a verified domain is
		// not yet routable. Only ACTIVE domains resolve publicly.
		if _, err := resolver.Resolve(ctx, "verified.matjero.com"); err != ErrDomainInactive {
			t.Errorf("got %v, want ErrDomainInactive", err)
		}
	})

	t.Run("inactive store fails safe", func(t *testing.T) {
		if _, err := resolver.Resolve(ctx, "inactive.matjero.com"); err != ErrStoreInactive {
			t.Errorf("got %v, want ErrStoreInactive", err)
		}
	})

	t.Run("empty domain fails safe", func(t *testing.T) {
		if _, err := resolver.Resolve(ctx, ""); err != ErrStoreNotFound {
			t.Errorf("got %v, want ErrStoreNotFound", err)
		}
	})
}

// TestStoreResolverTenantIsolation verifies that resolving store A's domain never
// returns store B's data, and vice versa. This is the core multi-tenant safety
// guarantee for the public storefront.
func TestStoreResolverTenantIsolation(t *testing.T) {
	lookup := fakeLookup{byDomain: map[string]commerce.StoreDomainResolution{
		"store-a.matjero.com": storeResolution("store-a", "store-a.matjero.com", "active", "active"),
		"store-b.matjero.com": storeResolution("store-b", "store-b.matjero.com", "active", "active"),
	}}
	resolver := NewStoreResolver(lookup)
	ctx := context.Background()

	resA, err := resolver.Resolve(ctx, "store-a.matjero.com")
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	if resA.Store.ID != "store-a" {
		t.Fatalf("store A resolved to %q", resA.Store.ID)
	}

	resB, err := resolver.Resolve(ctx, "store-b.matjero.com")
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if resB.Store.ID != "store-b" {
		t.Fatalf("store B resolved to %q", resB.Store.ID)
	}

	// A's domain must never resolve to B's store identity.
	if resA.Store.ID == resB.Store.ID {
		t.Fatalf("tenant isolation broken: both resolved to %q", resA.Store.ID)
	}
}
