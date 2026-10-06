package coreapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"core/packages/httpx"
)

func (s *server) handleGetStoreBalance(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	if storeID == "" {
		writeError(w, CodeValidationError)
		return
	}

	bal, err := s.deps.Balance.GetAccountBalance(r.Context(), storeID)
	if err != nil {
		// Fallback for store accounts not yet initialized in ledger/balance
		httpx.WriteJSON(w, http.StatusOK, StoreBalanceResponse{
			AvailableMinor: 0,
			PendingMinor:   0,
			Currency:       "SAR",
			UpdatedAt:      time.Now().UTC(),
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, StoreBalanceResponse{
		AvailableMinor: bal.BalanceMinor,
		PendingMinor:   0,
		Currency:       bal.Currency,
		UpdatedAt:      bal.UpdatedAt,
	})
}

func (s *server) handleListStoreLedgerEntries(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	if storeID == "" {
		writeError(w, CodeValidationError)
		return
	}

	// Store ledger transaction journal entries
	items := []JournalEntryResponse{}
	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[JournalEntryResponse]{
		Items: items,
	})
}

func (s *server) handleListStoreSettlements(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	if storeID == "" {
		writeError(w, CodeValidationError)
		return
	}

	page := parsePage(r)
	settlements, err := s.deps.Settlement.ListAccountSettlements(r.Context(), storeID, page)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, CollectionResponse[SettlementResponse]{
			Items: []SettlementResponse{},
		})
		return
	}

	items := make([]SettlementResponse, 0, len(settlements))
	for _, st := range settlements {
		items = append(items, toSettlementResponse(&st))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[SettlementResponse]{
		Items: items,
	})
}

func (s *server) handleListStorePayouts(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	if storeID == "" {
		writeError(w, CodeValidationError)
		return
	}

	page := parsePage(r)
	pageNum := (page.Offset / page.Limit) + 1
	if pageNum < 1 {
		pageNum = 1
	}

	payouts, _, err := s.deps.Finance.ListStorePayouts(r.Context(), storeID, pageNum, page.Limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]PayoutResponse, 0, len(payouts))
	for _, p := range payouts {
		ref := ""
		if p.ReferenceNumber != nil {
			ref = *p.ReferenceNumber
		}
		items = append(items, PayoutResponse{
			ID:           p.ID,
			StoreID:      p.StoreID,
			AmountMinor:  p.AmountMinorUnits,
			Currency:     p.Currency,
			Status:       p.Status,
			PayoutMethod: p.DestinationBank,
			Reference:    ref,
			CreatedAt:    p.CreatedAt,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[PayoutResponse]{
		Items: items,
	})
}
