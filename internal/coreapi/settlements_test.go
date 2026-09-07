package coreapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/modules/commerce"
)

type stubSettlement struct {
	settlements []settlement.Settlement
	err         error
}

func (s *stubSettlement) CalculatePeriodSettlements(ctx context.Context, params settlement.CalculateSettlementParams) ([]settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.settlements, nil
}

func (s *stubSettlement) FinalizePeriodSettlements(ctx context.Context, params settlement.FinalizeSettlementParams) ([]settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.settlements, nil
}

func (s *stubSettlement) GetSettlement(ctx context.Context, id string) (*settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.settlements) > 0 {
		return &s.settlements[0], nil
	}
	return nil, settlement.ErrSettlementNotFound
}

func (s *stubSettlement) GetSettlementByPeriodAndAccount(ctx context.Context, periodID, accountID string) (*settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.settlements) > 0 {
		return &s.settlements[0], nil
	}
	return nil, settlement.ErrSettlementNotFound
}

func (s *stubSettlement) ListAccountSettlements(ctx context.Context, accountID string, page commerce.Page) ([]settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.settlements, nil
}

func (s *stubSettlement) ListPeriodSettlements(ctx context.Context, periodID string, page commerce.Page) ([]settlement.Settlement, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.settlements, nil
}

func TestSettlementAPIEndpoints(t *testing.T) {
	now := time.Now().UTC()
	st := settlement.Settlement{
		ID:                    "set-123",
		SettlementPeriodID:    "per-456",
		AccountID:             "acc-789",
		Currency:              "SAR",
		GrossAmountMinor:      10000,
		AdjustmentAmountMinor: 0,
		NetAmountMinor:        10000,
		Status:                settlement.StatusCalculated,
		CreatedAt:             now,
		CalculatedAt:          &now,
	}

	deps := Dependencies{
		Settlement: &stubSettlement{settlements: []settlement.Settlement{st}},
	}
	router := NewRouter(deps)

	authedReq := func(method, path string) *http.Request {
		req := httptest.NewRequest(method, path, nil)
		ctx := serviceauth.WithCaller(req.Context(), serviceauth.CallerSeller)
		return req.WithContext(ctx)
	}

	t.Run("POST /internal/v1/settlements/periods/{id}/calculate", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodPost, "/internal/v1/settlements/periods/per-456/calculate"))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		var res CollectionResponse[SettlementResponse]
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(res.Items) != 1 || res.Items[0].ID != "set-123" {
			t.Errorf("unexpected response items: %+v", res.Items)
		}
	})

	t.Run("GET /internal/v1/settlements/{id}", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, authedReq(http.MethodGet, "/internal/v1/settlements/set-123"))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		var res SettlementResponse
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if res.ID != "set-123" || res.GrossAmountMinor != 10000 {
			t.Errorf("unexpected settlement response: %+v", res)
		}
	})
}
