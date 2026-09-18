package storefront

import (
	"context"
	"errors"
	"testing"

	"core/modules/commerce"
	"core/packages/i18n"
)

type stubRevisionStore struct {
	revision int64
	err      error
	gotStore string
}

func (s *stubRevisionStore) StorefrontRevision(ctx context.Context, storeID string) (int64, error) {
	s.gotStore = storeID
	return s.revision, s.err
}

type stubStoreLookup struct {
	resolution commerce.StoreDomainResolution
	err        error
}

func (s stubStoreLookup) GetStoreByDomain(ctx context.Context, domain string) (commerce.StoreDomainResolution, error) {
	return s.resolution, s.err
}

func activeStoreLookup() stubStoreLookup {
	return stubStoreLookup{resolution: commerce.StoreDomainResolution{
		Store:       commerce.Store{ID: "store-1", MarketCode: "EG", Status: "active"},
		StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.com", Status: "active"},
	}}
}

func TestRevisionReaderResolvesTenantFromHost(t *testing.T) {
	store := &stubRevisionStore{revision: 41}
	reader := NewRevisionReader(NewStoreResolver(activeStoreLookup()), store)

	revision, err := reader.Revision(context.Background(), "Store-A.Matjero.com:8443")
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	if revision != 41 {
		t.Fatalf("revision = %d, want 41", revision)
	}
	if store.gotStore != "store-1" {
		t.Fatalf("revision read for store %q, want the host-resolved store", store.gotStore)
	}
}

// A storefront that no longer resolves publicly must have no revision, so a
// downstream cache cannot keep serving it.
func TestRevisionReaderFailsWhenStorefrontIsUnavailable(t *testing.T) {
	for name, lookup := range map[string]stubStoreLookup{
		"unknown domain": {err: commerce.ErrNotFound},
		"inactive domain": {resolution: commerce.StoreDomainResolution{
			Store:       commerce.Store{ID: "store-1", MarketCode: "EG", Status: "active"},
			StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.com", Status: "disabled"},
		}},
		"inactive store": {resolution: commerce.StoreDomainResolution{
			Store:       commerce.Store{ID: "store-1", MarketCode: "EG", Status: "suspended"},
			StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.com", Status: "active"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			reader := NewRevisionReader(NewStoreResolver(lookup), &stubRevisionStore{revision: 41})
			if _, err := reader.Revision(context.Background(), "store-a.matjero.com"); err == nil {
				t.Fatal("expected an unavailable storefront to yield no revision")
			}
		})
	}
}

func TestRevisionReaderRejectsEmptyHost(t *testing.T) {
	reader := NewRevisionReader(NewStoreResolver(activeStoreLookup()), &stubRevisionStore{revision: 41})

	if _, err := reader.Revision(context.Background(), ""); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("empty host error = %v, want ErrStoreNotFound", err)
	}
}

// A read that already holds a scope must be labelled with that scope's store, not
// by resolving the host a second time.
func TestRevisionReaderForScopeUsesScopeStore(t *testing.T) {
	store := &stubRevisionStore{revision: 7}
	reader := NewRevisionReader(NewStoreResolver(activeStoreLookup()), store)
	scope, err := NewCatalogScope(resolvedStore(), i18n.LocaleEnglish)
	if err != nil {
		t.Fatalf("build scope: %v", err)
	}

	revision, err := reader.RevisionFor(context.Background(), scope)
	if err != nil {
		t.Fatalf("RevisionFor: %v", err)
	}
	if revision != 7 {
		t.Fatalf("revision = %d, want 7", revision)
	}
	if store.gotStore != "store-1" {
		t.Fatalf("revision read for store %q, want the scope's store", store.gotStore)
	}
}

func TestRevisionReaderForScopeRequiresResolvedScope(t *testing.T) {
	reader := NewRevisionReader(NewStoreResolver(activeStoreLookup()), &stubRevisionStore{revision: 7})

	if _, err := reader.RevisionFor(context.Background(), CatalogScope{}); err == nil {
		t.Fatal("expected an unresolved scope to be rejected")
	}
}
