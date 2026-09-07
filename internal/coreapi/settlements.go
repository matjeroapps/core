package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleCalculateSettlement(w http.ResponseWriter, r *http.Request) {
	periodID := chi.URLParam(r, "id")
	if periodID == "" {
		writeError(w, CodeValidationError)
		return
	}

	var req CalculateSettlementRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	settlements, err := s.deps.Settlement.CalculatePeriodSettlements(r.Context(), settlement.CalculateSettlementParams{
		PeriodID:  periodID,
		AccountID: req.AccountID,
	})
	if err != nil {
		writeDomainError(w, err)
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

func (s *server) handleFinalizeSettlement(w http.ResponseWriter, r *http.Request) {
	periodID := chi.URLParam(r, "id")
	if periodID == "" {
		writeError(w, CodeValidationError)
		return
	}

	settlements, err := s.deps.Settlement.FinalizePeriodSettlements(r.Context(), settlement.FinalizeSettlementParams{
		PeriodID: periodID,
	})
	if err != nil {
		writeDomainError(w, err)
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

func (s *server) handleGetSettlement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, CodeValidationError)
		return
	}

	st, err := s.deps.Settlement.GetSettlement(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSettlementResponse(st))
}

func (s *server) handleListAccountSettlements(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	if accountID == "" {
		writeError(w, CodeValidationError)
		return
	}

	page := parsePage(r)
	settlements, err := s.deps.Settlement.ListAccountSettlements(r.Context(), accountID, page)
	if err != nil {
		writeDomainError(w, err)
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

func (s *server) handleListPeriodSettlements(w http.ResponseWriter, r *http.Request) {
	periodID := chi.URLParam(r, "id")
	if periodID == "" {
		writeError(w, CodeValidationError)
		return
	}

	page := parsePage(r)
	settlements, err := s.deps.Settlement.ListPeriodSettlements(r.Context(), periodID, page)
	if err != nil {
		writeDomainError(w, err)
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

func toSettlementResponse(st *settlement.Settlement) SettlementResponse {
	return SettlementResponse{
		ID:                    st.ID,
		SettlementPeriodID:    st.SettlementPeriodID,
		AccountID:             st.AccountID,
		Currency:              st.Currency,
		GrossAmountMinor:      st.GrossAmountMinor,
		AdjustmentAmountMinor: st.AdjustmentAmountMinor,
		NetAmountMinor:        st.NetAmountMinor,
		Status:                string(st.Status),
		CreatedAt:             st.CreatedAt,
		CalculatedAt:          st.CalculatedAt,
		FinalizedAt:           st.FinalizedAt,
	}
}
