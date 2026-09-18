package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/serviceauth"
	"core/modules/themes"
	"core/packages/httpx"
)

// Theme Engine handlers.
//
// The theme business logic (installation, draft editing, atomic publishing,
// version upgrades, preview-token issuance) stays in Core. The seller service
// only forwards the authenticated seller identity; every store-scoped operation
// enforces resource-level ownership inside themes.Service.

func (s *server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	items, err := s.deps.Themes.ListThemes(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[themes.Theme]{Items: items})
}

func (s *server) handleListThemeVersions(w http.ResponseWriter, r *http.Request) {
	theme, err := s.deps.Themes.GetThemeByKey(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items, err := s.deps.Themes.ListThemeVersions(r.Context(), theme.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[themes.ThemeVersion]{Items: items})
}

func (s *server) handleGetThemeInstallation(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	installation, config, err := s.deps.Themes.GetInstallation(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemeInstallationResponse{
		Installation:      installation,
		DraftConfig:       config.DraftConfig,
		PublishedConfig:   config.PublishedConfig,
		DraftRevision:     config.DraftRevision,
		PublishedRevision: config.PublishedRevision,
	})
}

func (s *server) handleInstallTheme(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	var body ThemeInstallRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	installation, err := s.deps.Themes.Install(r.Context(), sellerID, storeID, body.ThemeKey, body.Version)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ThemeInstallationResponse{Installation: installation})
}

func (s *server) handleGetThemeDraft(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	config, revision, err := s.deps.Themes.GetDraftConfiguration(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemeDraftResponse{Config: config, Revision: revision})
}

func (s *server) handleUpdateThemeDraft(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	var body ThemeConfigRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	revision, err := s.deps.Themes.UpdateDraftConfiguration(r.Context(), sellerID, storeID, body.Config)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemeDraftResponse{Config: body.Config, Revision: revision})
}

func (s *server) handlePublishTheme(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	revision, err := s.deps.Themes.PublishConfiguration(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemePublishResponse{PublishedRevision: revision})
}

func (s *server) handleDiscardThemeDraft(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	revision, err := s.deps.Themes.DiscardDraft(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	config, _, err := s.deps.Themes.GetDraftConfiguration(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemeDraftResponse{Config: config, Revision: revision})
}

func (s *server) handleUpgradeTheme(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	var body ThemeUpgradeRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := s.deps.Themes.UpgradeInstallation(r.Context(), sellerID, storeID, body.Version); err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, StatusResponse{Status: "upgraded"})
}

func (s *server) handleCreateThemePreview(w http.ResponseWriter, r *http.Request) {
	sellerID, storeID, ok := s.authorizeThemeStore(w, r)
	if !ok {
		return
	}
	token, err := s.deps.Themes.CreatePreviewToken(r.Context(), sellerID, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ThemePreviewResponse{Token: token})
}

// authorizeThemeStore resolves the seller identity for a store-scoped theme
// operation. The seller ID always comes from Core's own subject resolution, so a
// caller cannot manage themes for a store it does not own.
func (s *server) authorizeThemeStore(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeInvalidArgument)
		return "", "", false
	}
	sellerID, err := s.deps.Commerce.ResolveSellerIDForSubject(r.Context(), subject)
	if err != nil {
		writeDomainError(w, err)
		return "", "", false
	}
	return sellerID, chi.URLParam(r, "storeID"), true
}
