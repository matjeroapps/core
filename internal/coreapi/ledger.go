package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleCreateLedgerAccount(w http.ResponseWriter, r *http.Request) {
	var req CreateLedgerAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	params := finance.CreateAccountParams{
		AccountCode: req.AccountCode,
		Name:        req.Name,
		AccountType: finance.AccountType(req.AccountType),
		Currency:    req.Currency,
	}

	account, err := s.deps.Finance.CreateAccount(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toLedgerAccountResponse(account))
}

func (s *server) handleGetLedgerAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, CodeValidationError)
		return
	}

	account, err := s.deps.Finance.GetAccount(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toLedgerAccountResponse(account))
}

func (s *server) handleListLedgerAccounts(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)
	accounts, err := s.deps.Finance.ListAccounts(r.Context(), page)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]LedgerAccountResponse, 0, len(accounts))
	for _, acc := range accounts {
		items = append(items, toLedgerAccountResponse(&acc))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[LedgerAccountResponse]{
		Items: items,
	})
}

func (s *server) handlePostJournalEntry(w http.ResponseWriter, r *http.Request) {
	var req PostJournalEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	lines := make([]finance.CreateJournalLineParams, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, finance.CreateJournalLineParams{
			AccountID:         l.AccountID,
			DebitAmountMinor:  l.DebitAmountMinor,
			CreditAmountMinor: l.CreditAmountMinor,
		})
	}

	params := finance.PostJournalEntryParams{
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceID,
		Description:   req.Description,
		Currency:      req.Currency,
		Lines:         lines,
		CorrelationID: r.Header.Get("X-Correlation-ID"),
		CausationID:   serviceauth.SubjectFrom(r),
	}

	entry, err := s.deps.Finance.PostJournalEntry(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toJournalEntryResponse(entry))
}

func (s *server) handleGetJournalEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, CodeValidationError)
		return
	}

	entry, err := s.deps.Finance.GetJournalEntry(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toJournalEntryResponse(entry))
}

func toLedgerAccountResponse(acc *finance.Account) LedgerAccountResponse {
	return LedgerAccountResponse{
		ID:          acc.ID,
		AccountCode: acc.AccountCode,
		Name:        acc.Name,
		AccountType: string(acc.AccountType),
		Currency:    acc.Currency,
		Status:      string(acc.Status),
		CreatedAt:   acc.CreatedAt,
		UpdatedAt:   acc.UpdatedAt,
	}
}

func toJournalEntryResponse(entry *finance.JournalEntry) JournalEntryResponse {
	lines := make([]JournalLineResponse, 0, len(entry.Lines))
	for _, l := range entry.Lines {
		lines = append(lines, JournalLineResponse{
			ID:                l.ID,
			JournalEntryID:    l.JournalEntryID,
			AccountID:         l.AccountID,
			DebitAmountMinor:  l.DebitAmountMinor,
			CreditAmountMinor: l.CreditAmountMinor,
			CreatedAt:         l.CreatedAt,
		})
	}

	return JournalEntryResponse{
		ID:            entry.ID,
		ReferenceType: entry.ReferenceType,
		ReferenceID:   entry.ReferenceID,
		Description:   entry.Description,
		Currency:      entry.Currency,
		PostedAt:      entry.PostedAt,
		CreatedAt:     entry.CreatedAt,
		Lines:         lines,
	}
}
