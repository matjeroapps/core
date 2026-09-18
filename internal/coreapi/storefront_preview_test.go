package coreapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/modules/storefront"
	"core/modules/themes"
	"core/packages/i18n"
)

type previewStubCatalog struct {
	bootstrap storefront.StoreBootstrap
}

func (s previewStubCatalog) Bootstrap(ctx context.Context, scope storefront.CatalogScope) (storefront.StoreBootstrap, error) {
	return s.bootstrap, nil
}

func (s previewStubCatalog) Categories(ctx context.Context, scope storefront.CatalogScope) ([]storefront.CategoryNode, error) {
	return nil, nil
}

func (s previewStubCatalog) CategoryBySlug(ctx context.Context, scope storefront.CatalogScope, slug string) (storefront.CategoryNode, error) {
	return storefront.CategoryNode{}, nil
}

func (s previewStubCatalog) Products(ctx context.Context, scope storefront.CatalogScope, query storefront.ProductQuery) (storefront.ProductPage, error) {
	return storefront.ProductPage{}, nil
}

func (s previewStubCatalog) Search(ctx context.Context, scope storefront.CatalogScope, keyword string, query storefront.ProductQuery) (storefront.ProductPage, error) {
	return storefront.ProductPage{}, nil
}

func (s previewStubCatalog) ProductBySlug(ctx context.Context, scope storefront.CatalogScope, slug string) (storefront.ProductDetail, error) {
	return storefront.ProductDetail{}, nil
}

func (e integrationEnv) previewBootstrap(t *testing.T, host, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, host)
	if token != "" {
		req.Header.Set(HeaderStorefrontPreview, token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func coreAPIDefaultConfigWithHeroTitle(t *testing.T, title string) map[string]any {
	t.Helper()
	data, err := json.Marshal(themes.DefaultConfiguration)
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("clone default config: %v", err)
	}
	hero, ok := cfg["hero"].(map[string]any)
	if !ok {
		t.Fatal("default config hero is not an object")
	}
	hero["title"] = title
	return cfg
}

func themePreviewService(env integrationEnv) themes.Service {
	return themes.NewService(themes.NewRepository(env.db.Pool), env.repo, themes.Options{
		PreviewSecret: []byte("integration-preview-secret"),
	})
}

func configurePreviewTheme(t *testing.T, env integrationEnv, sellerID, storeID, publishedTitle, draftTitle string) (themes.Service, string) {
	t.Helper()
	svc := themePreviewService(env)
	if _, err := svc.Install(env.ctx, sellerID, storeID, themes.DefaultThemeKey, themes.DefaultThemeVersion); err != nil {
		t.Fatalf("install theme: %v", err)
	}
	if _, err := svc.UpdateDraftConfiguration(env.ctx, sellerID, storeID, coreAPIDefaultConfigWithHeroTitle(t, publishedTitle)); err != nil {
		t.Fatalf("write published draft: %v", err)
	}
	if _, err := svc.PublishConfiguration(env.ctx, sellerID, storeID); err != nil {
		t.Fatalf("publish theme: %v", err)
	}
	if _, err := svc.UpdateDraftConfiguration(env.ctx, sellerID, storeID, coreAPIDefaultConfigWithHeroTitle(t, draftTitle)); err != nil {
		t.Fatalf("write preview draft: %v", err)
	}
	token, err := svc.CreatePreviewToken(env.ctx, sellerID, storeID)
	if err != nil {
		t.Fatalf("issue preview token: %v", err)
	}
	return svc, token
}

func bootstrapHeroTitle(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	payload := decodeInto[storefrontStoreResponse](t, rec)
	if payload.Store.Theme == nil {
		t.Fatal("bootstrap theme is missing")
	}
	hero, ok := payload.Store.Theme.Configuration["hero"].(map[string]any)
	if !ok {
		t.Fatalf("theme hero is not an object: %+v", payload.Store.Theme.Configuration)
	}
	title, _ := hero["title"].(string)
	return title
}

func TestIntegrationStorefrontPreviewReturnsDraftWithoutLeakingToNormalBootstrap(t *testing.T) {
	env := setupIntegration(t)
	_, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft")

	normal := env.get(t, "/internal/v1/storefront/store", "seller", env.domainA)
	if normal.Code != http.StatusOK {
		t.Fatalf("normal status = %d (body %q)", normal.Code, normal.Body.String())
	}
	if got := bootstrapHeroTitle(t, normal); got != "Published" {
		t.Fatalf("normal hero title = %q, want Published", got)
	}
	if got := normal.Header().Get(HeaderStorefrontRevision); got == "" {
		t.Fatal("normal bootstrap did not carry the revision header")
	}

	preview := env.previewBootstrap(t, env.domainA, token)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d (body %q)", preview.Code, preview.Body.String())
	}
	if got := bootstrapHeroTitle(t, preview); got != "Draft" {
		t.Fatalf("preview hero title = %q, want Draft", got)
	}
	if got := preview.Header().Get(HeaderStorefrontRevision); got != "" {
		t.Fatalf("preview carried revision header %q", got)
	}
	if got := preview.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("preview Cache-Control = %q, want no-store", got)
	}

	after := env.get(t, "/internal/v1/storefront/store", "seller", env.domainA)
	if after.Code != http.StatusOK {
		t.Fatalf("normal after preview status = %d (body %q)", after.Code, after.Body.String())
	}
	if got := bootstrapHeroTitle(t, after); got != "Published" {
		t.Fatalf("normal after preview hero title = %q, want Published", got)
	}
}

func TestIntegrationStorefrontPreviewDoesNotBumpRevision(t *testing.T) {
	env := setupIntegration(t)
	_, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft")
	before := env.revision(t, env.domainA)

	rec := env.previewBootstrap(t, env.domainA, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := env.revision(t, env.domainA); got != before {
		t.Fatalf("preview moved revision from %d to %d", before, got)
	}
}

func TestIntegrationStorefrontPreviewRejectsStaleToken(t *testing.T) {
	env := setupIntegration(t)
	svc, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft 2")

	if _, err := svc.UpdateDraftConfiguration(env.ctx, env.sellerA.ID, env.storeA.ID, coreAPIDefaultConfigWithHeroTitle(t, "Draft 3")); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	stale := env.previewBootstrap(t, env.domainA, token)
	if stale.Code != http.StatusNotFound {
		t.Fatalf("stale token status = %d, want 404 (body %q)", stale.Code, stale.Body.String())
	}
	if got := decodeError(t, stale).Error.Code; got != CodeStorefrontUnavailable {
		t.Fatalf("stale token code = %q, want %q", got, CodeStorefrontUnavailable)
	}

	fresh, err := svc.CreatePreviewToken(env.ctx, env.sellerA.ID, env.storeA.ID)
	if err != nil {
		t.Fatalf("issue fresh token: %v", err)
	}
	rec := env.previewBootstrap(t, env.domainA, fresh)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh preview status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := bootstrapHeroTitle(t, rec); got != "Draft 3" {
		t.Fatalf("fresh preview hero title = %q, want Draft 3", got)
	}
}

func TestIntegrationStorefrontPreviewRejectsTokenAfterThemeSwitch(t *testing.T) {
	env := setupIntegration(t)
	svc, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft")

	if _, err := svc.Install(env.ctx, env.sellerA.ID, env.storeA.ID, themes.DefaultThemeKey, themes.DefaultThemeVersion); err != nil {
		t.Fatalf("switch theme: %v", err)
	}
	old := env.previewBootstrap(t, env.domainA, token)
	if old.Code != http.StatusNotFound {
		t.Fatalf("old token status = %d, want 404 (body %q)", old.Code, old.Body.String())
	}
	if got := decodeError(t, old).Error.Code; got != CodeStorefrontUnavailable {
		t.Fatalf("old token code = %q, want %q", got, CodeStorefrontUnavailable)
	}

	fresh, err := svc.CreatePreviewToken(env.ctx, env.sellerA.ID, env.storeA.ID)
	if err != nil {
		t.Fatalf("issue fresh token: %v", err)
	}
	rec := env.previewBootstrap(t, env.domainA, fresh)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh preview status = %d (body %q)", rec.Code, rec.Body.String())
	}
}

func TestIntegrationStorefrontPreviewRejectsCrossStoreToken(t *testing.T) {
	env := setupIntegration(t)
	_, tokenA := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published A", "Draft A")
	_, tokenB := configurePreviewTheme(t, env, env.sellerB.ID, env.storeB.ID, "Published B", "Draft B")

	if rec := env.previewBootstrap(t, env.domainA, tokenA); rec.Code != http.StatusOK {
		t.Fatalf("token A host A status = %d (body %q)", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		name  string
		host  string
		token string
	}{
		{"token A host B", env.domainB, tokenA},
		{"token B host A", env.domainA, tokenB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.previewBootstrap(t, tc.host, tc.token)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, "Draft A") || strings.Contains(body, "Draft B") {
				t.Fatalf("failure body leaked draft content: %q", body)
			}
			if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
				t.Fatalf("error code = %q, want %q", got, CodeStorefrontUnavailable)
			}
		})
	}
}

func TestIntegrationStorefrontPreviewRejectsTamperedMalformedAndExpiredTokens(t *testing.T) {
	env := setupIntegration(t)
	_, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft")

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"tampered", token + "x"},
		{"malformed", "not-a-token"},
		{"extra segment", token + ".extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.previewBootstrap(t, env.domainA, tc.token)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body %q)", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "Draft") {
				t.Fatalf("failure body leaked draft content: %q", rec.Body.String())
			}
			if got := decodeError(t, rec).Error.Code; got != CodeStorefrontUnavailable {
				t.Fatalf("error code = %q, want %q", got, CodeStorefrontUnavailable)
			}
		})
	}
}

func TestIntegrationStorefrontPreviewPublishMakesDraftPublic(t *testing.T) {
	env := setupIntegration(t)
	svc, token := configurePreviewTheme(t, env, env.sellerA.ID, env.storeA.ID, "Published", "Draft")

	preview := env.previewBootstrap(t, env.domainA, token)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d (body %q)", preview.Code, preview.Body.String())
	}
	if _, err := svc.PublishConfiguration(env.ctx, env.sellerA.ID, env.storeA.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	normal := env.get(t, "/internal/v1/storefront/store", "seller", env.domainA)
	if normal.Code != http.StatusOK {
		t.Fatalf("normal status = %d (body %q)", normal.Code, normal.Body.String())
	}
	if got := bootstrapHeroTitle(t, normal); got != "Draft" {
		t.Fatalf("normal after publish hero title = %q, want Draft", got)
	}
}

func TestStorefrontPreviewMissingSecretMapsToPreviewUnavailable(t *testing.T) {
	storeID := "11111111-1111-1111-1111-111111111111"
	resolved := storefront.ResolvedStore{
		Store:       commerce.Store{ID: storeID, MarketCode: "EG"},
		StoreDomain: commerce.StoreDomain{Domain: "store-a.matjero.test"},
	}
	handler := newTestRouter(Dependencies{
		Stores: &stubStores{resolved: resolved},
		Catalog: previewStubCatalog{bootstrap: storefront.StoreBootstrap{
			StoreCode:        "store-a",
			StoreName:        "Store A",
			Market:           "EG",
			DefaultLocale:    string(i18n.LocaleEnglish),
			SupportedLocales: []string{string(i18n.LocaleEnglish)},
			Settings:         map[string]any{},
		}},
		Themes: themes.NewService(themes.Repository{}, nil, themes.Options{}),
	})
	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/storefront/store", "seller", testSellerToken)
	req.Header.Set(serviceauth.HeaderStorefrontHost, "store-a.matjero.test")
	req.Header.Set(HeaderStorefrontPreview, "signed-token")

	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %q)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error.Code; got != CodePreviewUnavailable {
		t.Fatalf("error code = %q, want %q", got, CodePreviewUnavailable)
	}
	if got := rec.Header().Get(HeaderStorefrontRevision); got != "" {
		t.Fatalf("preview failure carried revision header %q", got)
	}
}
