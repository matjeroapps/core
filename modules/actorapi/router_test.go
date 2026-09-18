package actorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"core/modules/markets"
	"core/packages/auth"
	"core/packages/i18n"
)

func TestRouterBootstrapIncludesPrincipalAndLocale(t *testing.T) {
	router := NewRouter(Config{
		AppName:      "Admin API",
		Actor:        "admin",
		RequireAuth:  true,
		AllowedRoles: []string{auth.RolePlatformAdmin},
	}, fakeMarketService{markets: []markets.Market{sampleMarket()}}, fakeVerifier{
		principal: auth.Principal{
			Subject: "user-1",
			Roles:   []string{auth.RolePlatformAdmin},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/bootstrap?locale=ar", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}

	var payload struct {
		App       string         `json:"app"`
		Locale    string         `json:"locale"`
		Direction string         `json:"direction"`
		Principal auth.Principal `json:"principal"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Locale != string(i18n.LocaleArabic) {
		t.Fatalf("Locale = %q", payload.Locale)
	}
	if payload.Direction != "rtl" {
		t.Fatalf("Direction = %q", payload.Direction)
	}
	if payload.Principal.Subject != "user-1" {
		t.Fatalf("Principal.Subject = %q", payload.Principal.Subject)
	}
}

func TestRouterRejectsMissingRole(t *testing.T) {
	router := NewRouter(Config{
		AppName:      "Seller API",
		Actor:        "seller",
		RequireAuth:  true,
		AllowedRoles: []string{auth.RoleSellerOwner},
	}, fakeMarketService{}, fakeVerifier{
		principal: auth.Principal{
			Subject: "user-2",
			Roles:   []string{auth.RoleSupplierOwner},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d", resp.Code)
	}
}

type fakeMarketService struct {
	markets []markets.Market
}

func (f fakeMarketService) List(ctx context.Context, locale i18n.Locale) ([]markets.Market, error) {
	return append([]markets.Market(nil), f.markets...), nil
}

func (f fakeMarketService) GetByCode(ctx context.Context, code string, locale i18n.Locale) (markets.Market, error) {
	for _, market := range f.markets {
		if market.Code == code {
			return market, nil
		}
	}
	return markets.Market{}, markets.ErrNotFound
}

type fakeVerifier struct {
	principal auth.Principal
}

func (f fakeVerifier) Verify(ctx context.Context, token string) (auth.Principal, error) {
	return f.principal, nil
}

func sampleMarket() markets.Market {
	return markets.Market{
		Code:          "EG",
		DefaultLocale: i18n.LocaleArabic,
		SupportedLocales: []i18n.Locale{
			i18n.LocaleArabic,
			i18n.LocaleEnglish,
		},
		Timezone: "Africa/Cairo",
		Status:   "active",
		Country: markets.Country{
			Code:     "EG",
			Name:     "Egypt",
			Timezone: "Africa/Cairo",
			Status:   "active",
		},
		Currency: markets.Currency{
			Code:      "EGP",
			Symbol:    "E£",
			MinorUnit: 2,
			Status:    "active",
		},
		Configuration: map[string]any{"release_track": "launch"},
	}
}
