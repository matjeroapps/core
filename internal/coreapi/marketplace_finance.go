package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/marketplace_finance"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleCreateFinancialRule(w http.ResponseWriter, r *http.Request) {
	var req CreateFinancialRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	rule, err := s.deps.MarketplaceFinance.CreateRule(r.Context(), marketplace_finance.CreateRuleParams{
		Name:             req.Name,
		RuleType:         marketplace_finance.RuleType(req.RuleType),
		Percentage:       req.Percentage,
		FixedAmountMinor: req.FixedAmountMinor,
		Currency:         req.Currency,
		AllocationType:   marketplace_finance.AllocationType(req.AllocationType),
		Status:           marketplace_finance.RuleStatus(req.Status),
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toFinancialRuleResponse(rule))
}

func (s *server) handleListFinancialRules(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	rules, err := s.deps.MarketplaceFinance.ListRules(r.Context(), statusFilter)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]FinancialRuleResponse, 0, len(rules))
	for i := range rules {
		items = append(items, toFinancialRuleResponse(&rules[i]))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[FinancialRuleResponse]{
		Items: items,
	})
}

func (s *server) handleAllocateSettlement(w http.ResponseWriter, r *http.Request) {
	settlementID := chi.URLParam(r, "id")
	if settlementID == "" {
		writeError(w, CodeValidationError)
		return
	}

	allocations, err := s.deps.MarketplaceFinance.AllocateSettlement(r.Context(), marketplace_finance.AllocateSettlementParams{
		SettlementID: settlementID,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]SettlementAllocationResponse, 0, len(allocations))
	for i := range allocations {
		items = append(items, toSettlementAllocationResponse(&allocations[i]))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[SettlementAllocationResponse]{
		Items: items,
	})
}

func (s *server) handleListSettlementAllocations(w http.ResponseWriter, r *http.Request) {
	settlementID := chi.URLParam(r, "id")
	if settlementID == "" {
		writeError(w, CodeValidationError)
		return
	}

	allocations, err := s.deps.MarketplaceFinance.ListAllocations(r.Context(), settlementID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]SettlementAllocationResponse, 0, len(allocations))
	for i := range allocations {
		items = append(items, toSettlementAllocationResponse(&allocations[i]))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[SettlementAllocationResponse]{
		Items: items,
	})
}

func toFinancialRuleResponse(rule *marketplace_finance.FinancialRule) FinancialRuleResponse {
	return FinancialRuleResponse{
		ID:               rule.ID,
		Name:             rule.Name,
		RuleType:         string(rule.RuleType),
		Percentage:       rule.Percentage,
		FixedAmountMinor: rule.FixedAmountMinor,
		Currency:         rule.Currency,
		AllocationType:   string(rule.AllocationType),
		Status:           string(rule.Status),
		CreatedAt:        rule.CreatedAt,
		UpdatedAt:        rule.UpdatedAt,
	}
}

func toSettlementAllocationResponse(alloc *marketplace_finance.SettlementAllocation) SettlementAllocationResponse {
	return SettlementAllocationResponse{
		ID:             alloc.ID,
		SettlementID:   alloc.SettlementID,
		AccountID:      alloc.AccountID,
		AllocationType: string(alloc.AllocationType),
		AmountMinor:    alloc.AmountMinor,
		Currency:       alloc.Currency,
		CreatedAt:      alloc.CreatedAt,
	}
}
