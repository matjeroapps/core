// Package storefront provides tenant resolution for the public storefront.
//
// Public storefront requests derive their tenant identity exclusively from a
// trusted domain-to-store mapping. Client-supplied store/seller identifiers are
// never trusted on public routes when the request host already determines the
// tenant. See ADR-010 (storefront tenant resolution).
package storefront

import (
	"context"
	"errors"

	"core/modules/commerce"
)

// ResolvedStore is the tenant context derived from a trusted storefront domain.
type ResolvedStore struct {
	Store       commerce.Store
	StoreDomain commerce.StoreDomain
}

// Resolution errors. These are deliberately generic to avoid leaking which part
// of the mapping failed (unknown domain vs inactive store) to public callers.
var (
	ErrStoreNotFound  = errors.New("store not found for domain")
	ErrDomainInactive = errors.New("store domain is not active")
	ErrStoreInactive  = errors.New("store is not active")
)

// StoreLookup resolves a normalized domain to its store + domain record.
// commerce.Repository satisfies this interface via GetStoreByDomain.
type StoreLookup interface {
	GetStoreByDomain(ctx context.Context, domain string) (commerce.StoreDomainResolution, error)
}

// StoreResolver resolves a request host to a tenant store using a trusted
// domain-to-store mapping. Unknown, inactive, unverified, or disabled mappings
// fail safely so the storefront can render a Not Found / Inactive page without
// exposing moderation details.
type StoreResolver struct {
	lookup StoreLookup
}

// NewStoreResolver builds a resolver backed by the given lookup.
func NewStoreResolver(lookup StoreLookup) StoreResolver {
	return StoreResolver{lookup: lookup}
}

// Resolve maps a (already normalized or raw) domain to a tenant store. It fails
// safely for empty, unknown, inactive-domain, or inactive-store domains.
func (r StoreResolver) Resolve(ctx context.Context, domain string) (ResolvedStore, error) {
	domain = NormalizeHost(domain)
	if domain == "" {
		return ResolvedStore{}, ErrStoreNotFound
	}

	resolution, err := r.lookup.GetStoreByDomain(ctx, domain)
	if err != nil {
		if errors.Is(err, commerce.ErrNotFound) {
			return ResolvedStore{}, ErrStoreNotFound
		}
		return ResolvedStore{}, err
	}

	if resolution.StoreDomain.Status != "active" {
		return ResolvedStore{}, ErrDomainInactive
	}
	if resolution.Store.Status != "active" {
		return ResolvedStore{}, ErrStoreInactive
	}

	return ResolvedStore{
		Store:       resolution.Store,
		StoreDomain: resolution.StoreDomain,
	}, nil
}
