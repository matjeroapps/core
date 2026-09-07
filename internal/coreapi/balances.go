package coreapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleGetAccountBalance(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	if accountID == "" {
		writeError(w, CodeValidationError)
		return
	}

	bal, err := s.deps.Balance.GetAccountBalance(r.Context(), accountID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toAccountBalanceResponse(bal))
}

func (s *server) handleListAccountBalances(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)
	balances, err := s.deps.Balance.ListAccountBalances(r.Context(), page)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]AccountBalanceResponse, 0, len(balances))
	for _, b := range balances {
		items = append(items, toAccountBalanceResponse(&b))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[AccountBalanceResponse]{
		Items: items,
	})
}

func toAccountBalanceResponse(bal *balance.AccountBalance) AccountBalanceResponse {
	return AccountBalanceResponse{
		AccountID:        bal.AccountID,
		Currency:         bal.Currency,
		DebitTotalMinor:  bal.DebitTotalMinor,
		CreditTotalMinor: bal.CreditTotalMinor,
		BalanceMinor:     bal.BalanceMinor,
		UpdatedAt:        bal.UpdatedAt,
	}
}
