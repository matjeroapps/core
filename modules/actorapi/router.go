package actorapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/modules/api"
	"core/modules/markets"
	"core/packages/auth"
	"core/packages/httpx"
	"core/packages/i18n"
)

type Config struct {
	AppName      string
	Actor        string
	RequireAuth  bool
	AllowedRoles []string
	Register     func(r chi.Router)
}

type MarketService interface {
	List(ctx context.Context, locale i18n.Locale) ([]markets.Market, error)
	GetByCode(ctx context.Context, code string, locale i18n.Locale) (markets.Market, error)
}

type Server struct {
	markets MarketService
	config  Config
}

func NewRouter(config Config, marketService MarketService, verifier auth.Verifier) chi.Router {
	if config.RequireAuth && verifier == nil {
		panic("actor api requires verifier")
	}

	r := chi.NewRouter()
	r.Use(i18n.Middleware(i18n.Default()))

	if config.RequireAuth {
		r.Use(auth.Middleware(verifier))
		if len(config.AllowedRoles) > 0 {
			r.Use(auth.RequireAnyRole(config.AllowedRoles...))
		}
	}

	server := Server{markets: marketService, config: config}

	r.Route("/v1", func(r chi.Router) {
		r.Get("/bootstrap", server.handleBootstrap)
		r.Get("/markets", server.handleMarkets)
		r.Get("/markets/{code}", server.handleMarket)
		if config.Register != nil {
			config.Register(r)
		}
	})

	return r
}

func (s Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	principal := auth.PrincipalOrNil(r)
	locale := i18n.FromContext(r.Context())
	marketsList, err := s.markets.List(r.Context(), locale)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "bootstrap_unavailable", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, api.NewBootstrap(
		s.config.AppName,
		s.config.Actor,
		principal,
		locale,
		i18n.SupportedLocales,
		marketsList,
	))
}

func (s Server) handleMarkets(w http.ResponseWriter, r *http.Request) {
	locale := i18n.FromContext(r.Context())
	marketsList, err := s.markets.List(r.Context(), locale)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "markets_unavailable", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"markets": marketsList,
	})
}

func (s Server) handleMarket(w http.ResponseWriter, r *http.Request) {
	locale := i18n.FromContext(r.Context())
	code := chi.URLParam(r, "code")
	market, err := s.markets.GetByCode(r.Context(), code, locale)
	if err != nil {
		if errors.Is(err, markets.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "market_not_found", "market not found")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "market_unavailable", err.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, market)
}
