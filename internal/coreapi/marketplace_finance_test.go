package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/marketplace_finance"
	"github.com/matjeroapps/core/internal/serviceauth"
)

type stubMarketplaceFinance struct {
	rules       []marketplace_finance.FinancialRule
	allocations []marketplace_finance.SettlementAllocation
	err         error
}

func (s *stubMarketplaceFinance) CreateRule(ctx context.Context, params marketplace_finance.CreateRuleParams) (*marketplace_finance.FinancialRule, error) {
	if s.err != nil {
		return nil, s.err
	}
	rule := &marketplace_finance.FinancialRule{
		ID:               "rule-123",
		Name:             params.Name,
		RuleType:         params.RuleType,
		Percentage:       params.Percentage,
		FixedAmountMinor: params.FixedAmountMinor,
		Currency:         params.Currency,
		AllocationType:   params.AllocationType,
		Status:           params.Status,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	return rule, nil
}

func (s *stubMarketplaceFinance) ListRules(ctx context.Context, statusFilter string) ([]marketplace_finance.FinancialRule, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rules, nil
}

func (s *stubMarketplaceFinance) AllocateSettlement(ctx context.Context, params marketplace_finance.AllocateSettlementParams) ([]marketplace_finance.SettlementAllocation, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.allocations, nil
}

func (s *stubMarketplaceFinance) ListAllocations(ctx context.Context, settlementID string) ([]marketplace_finance.SettlementAllocation, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.allocations, nil
}

func TestMarketplaceFinanceAPIEndpoints(t *testing.T) {
	now := time.Now().UTC()
	rule := marketplace_finance.FinancialRule{
		ID:             "rule-123",
		Name:           "Platform Fee 10%",
		RuleType:       marketplace_finance.RuleTypePercentage,
		Percentage:     10.0,
		Currency:       "SAR",
		AllocationType: marketplace_finance.AllocationTypePlatformShare,
		Status:         marketplace_finance.RuleStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	alloc := marketplace_finance.SettlementAllocation{
		ID:             "alloc-456",
		SettlementID:   "set-123",
		AccountID:      "acc-789",
		AllocationType: marketplace_finance.AllocationTypePlatformShare,
		AmountMinor:    1000,
		Currency:       "SAR",
		CreatedAt:      now,
	}

	deps := Dependencies{
		MarketplaceFinance: &stubMarketplaceFinance{
			rules:       []marketplace_finance.FinancialRule{rule},
			allocations: []marketplace_finance.SettlementAllocation{alloc},
		},
	}
	router := NewRouter(deps)

	authedReq := func(method, path string, body []byte) *http.Request {
		var req *http.Request
		if body != nil {
			req = httptest.NewRequest(method, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		ctx := serviceauth.WithCaller(req.Context(), serviceauth.CallerAdmin)
		return req.WithContext(ctx)
	}

	t.Run("POST /internal/v1/financial-rules", func(t *testing.T) {
		reqBody, _ := json.Marshal(CreateFinancialRuleRequest{
			Name:           "Platform Fee 10%",
			RuleType:       "PERCENTAGE",
			Percentage:     10.0,
			Currency:       "SAR",
			AllocationType: "PLATFORM_SHARE",
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodPost, "/internal/v1/financial-rules", reqBody))
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d (body: %s)", w.Code, w.Body.String())
		}
	})

	t.Run("GET /internal/v1/financial-rules", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodGet, "/internal/v1/financial-rules?status=ACTIVE", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("POST /internal/v1/settlements/{id}/allocate", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodPost, "/internal/v1/settlements/set-123/allocate", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("GET /internal/v1/settlements/{id}/allocations", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodGet, "/internal/v1/settlements/set-123/allocations", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
	})
}
