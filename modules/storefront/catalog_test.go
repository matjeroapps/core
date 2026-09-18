package storefront

import (
	"testing"

	"core/modules/commerce"
	"core/packages/i18n"
)

func resolvedStore() ResolvedStore {
	return ResolvedStore{
		Store:       commerce.Store{ID: "store-1", MarketCode: "EG"},
		StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.com"},
	}
}

func TestNewCatalogScopeValidatesLocale(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.LocaleArabic, i18n.LocaleEnglish} {
		scope, err := NewCatalogScope(resolvedStore(), locale)
		if err != nil {
			t.Fatalf("locale %q rejected: %v", locale, err)
		}
		if scope.Locale() != locale {
			t.Fatalf("locale %q not retained, got %q", locale, scope.Locale())
		}
	}

	scope, err := NewCatalogScope(resolvedStore(), "")
	if err != nil {
		t.Fatalf("empty locale rejected: %v", err)
	}
	if scope.Locale() != i18n.Default() {
		t.Fatalf("empty locale did not default, got %q", scope.Locale())
	}

	// An arbitrary locale must be rejected at the boundary so it can never reach
	// a SQL parameter.
	if _, err := NewCatalogScope(resolvedStore(), "fr-CA'; DROP TABLE products; --"); err == nil {
		t.Fatal("expected unsupported locale to be rejected")
	}
}

func TestNewCatalogScopeRequiresResolvedStore(t *testing.T) {
	if _, err := NewCatalogScope(ResolvedStore{}, i18n.LocaleEnglish); err == nil {
		t.Fatal("expected an unresolved store to be rejected")
	}
}

func TestProductQueryNormalizePageBounds(t *testing.T) {
	query, err := ProductQuery{}.normalize()
	if err != nil {
		t.Fatalf("default query rejected: %v", err)
	}
	if query.Page.Limit != DefaultPageLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultPageLimit, query.Page.Limit)
	}
	if query.Sort != SortNewest {
		t.Fatalf("expected default sort %q, got %q", SortNewest, query.Sort)
	}

	if _, err := (ProductQuery{Page: Page{Limit: 1000000}}).normalize(); err == nil {
		t.Fatal("expected an unbounded limit to be rejected")
	}
	if _, err := (ProductQuery{Page: Page{Limit: -1}}).normalize(); err == nil {
		t.Fatal("expected a negative limit to be rejected")
	}
	if _, err := (ProductQuery{Page: Page{Offset: -1}}).normalize(); err == nil {
		t.Fatal("expected a negative offset to be rejected")
	}
}

func TestProductQueryNormalizeRejectsInvalidFilters(t *testing.T) {
	negative := int64(-1)
	min := int64(5000)
	max := int64(1000)

	cases := map[string]ProductQuery{
		"unknown sort":         {Sort: "price"},
		"unknown availability": {Availability: "maybe"},
		"negative min price":   {MinPriceMinor: &negative},
		"negative max price":   {MaxPriceMinor: &negative},
		"min above max":        {MinPriceMinor: &min, MaxPriceMinor: &max},
	}
	for name, query := range cases {
		if _, err := query.normalize(); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestAvailabilityState(t *testing.T) {
	if got := availabilityState(true); got != AvailabilityInStock {
		t.Fatalf("expected %q, got %q", AvailabilityInStock, got)
	}
	if got := availabilityState(false); got != AvailabilityOutOfStock {
		t.Fatalf("expected %q, got %q", AvailabilityOutOfStock, got)
	}
}

func TestFallbackLocaleMirrorsMarketPolicy(t *testing.T) {
	if got := fallbackLocale(i18n.LocaleArabic); got != i18n.LocaleEnglish {
		t.Fatalf("expected ar to fall back to en, got %q", got)
	}
	if got := fallbackLocale(i18n.LocaleEnglish); got != i18n.LocaleArabic {
		t.Fatalf("expected en to fall back to ar, got %q", got)
	}
}
