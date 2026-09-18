package coreapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"core/modules/commerce"
	"core/packages/httpx"
)

const HeaderCartToken = "X-Matjero-Cart-Token"

func cartResponse(cart commerce.Cart, rawToken string) CartResponse {
	response := CartResponse{
		ID: cart.ID, Status: cart.Status, MarketCode: cart.MarketCode,
		CartToken: rawToken, Items: make([]CartLineResponse, 0, len(cart.Items)),
	}
	for _, item := range cart.Items {
		response.Items = append(response.Items, CartLineResponse{
			ID: item.ID, SKUID: item.SKUID, Quantity: item.Quantity,
			ExpectedUnitPriceMinor: item.ExpectedUnitPriceMinor,
			ExpectedCurrencyCode:   item.ExpectedCurrencyCode,
		})
	}
	return response
}

func cartTokenFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(HeaderCartToken))
}

func (s *server) handleCreateCart(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	cart, token, err := s.deps.Repo.CreateCart(r.Context(), scope.StoreID(), scope.MarketCode(), nil)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, cartResponse(cart, token))
}

func (s *server) handleGetCart(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	token := cartTokenFromRequest(r)
	cart, err := s.deps.Repo.GetCartByToken(r.Context(), scope.StoreID(), token)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cartResponse(cart, ""))
}

func (s *server) handleAddCartItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	var body CartAddItemRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cart, err := s.deps.Repo.AddCartItem(r.Context(), scope.StoreID(), cartTokenFromRequest(r), body.SKUID, body.Quantity)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cartResponse(cart, ""))
}

func (s *server) handleUpdateCartItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	var body struct {
		Quantity int64 `json:"quantity"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	cart, err := s.deps.Repo.UpdateCartItemQuantity(r.Context(), scope.StoreID(), cartTokenFromRequest(r), chi.URLParam(r, "itemID"), body.Quantity)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cartResponse(cart, ""))
}

func (s *server) handleRemoveCartItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.scopeFor(w, r)
	if !ok {
		return
	}
	cart, err := s.deps.Repo.RemoveCartItem(r.Context(), scope.StoreID(), cartTokenFromRequest(r), chi.URLParam(r, "itemID"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cartResponse(cart, ""))
}
