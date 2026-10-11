package coreapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"core/internal/balance"
	"core/modules/commerce"
)

type stubBalance struct {
	bal *balance.AccountBalance
	err error
}

func (s *stubBalance) GetAccountBalance(ctx context.Context, accountID string) (*balance.AccountBalance, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.bal, nil
}

func (s *stubBalance) ListAccountBalances(ctx context.Context, page commerce.Page) ([]balance.AccountBalance, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.bal == nil {
		return nil, nil
	}
	return []balance.AccountBalance{*s.bal}, nil
}

func TestGetStoreBalanceUsesAuthoritativeBalanceCurrencyForRealZero(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	handler := newTestRouter(Dependencies{Balance: &stubBalance{bal: &balance.AccountBalance{
		AccountID:        "store-123",
		Currency:         "EGP",
		DebitTotalMinor:  5000,
		CreditTotalMinor: 5000,
		BalanceMinor:     0,
		UpdatedAt:        now,
	}}})

	rec := doRequest(t, handler, authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/store-123/finance/balance", "seller", testSellerToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload StoreBalanceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode balance response: %v", err)
	}
	if payload.AvailableMinor != 0 {
		t.Errorf("available_minor = %d, want 0", payload.AvailableMinor)
	}
	if payload.PendingMinor != 0 {
		t.Errorf("pending_minor = %d, want 0", payload.PendingMinor)
	}
	if payload.Currency != "EGP" {
		t.Errorf("currency = %q, want authoritative balance currency EGP", payload.Currency)
	}
	if !payload.UpdatedAt.Equal(now) {
		t.Errorf("updated_at = %s, want %s", payload.UpdatedAt, now)
	}
}

func TestGetStoreBalanceReturnsErrorContractForUnresolvedAccount(t *testing.T) {
	handler := newTestRouter(Dependencies{Balance: &stubBalance{err: balance.ErrAccountNotFound}})

	rec := doRequest(t, handler, authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/missing-store/finance/balance", "seller", testSellerToken))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	payload := decodeError(t, rec)
	if payload.Error.Code != CodeNotFound {
		t.Errorf("error code = %q, want %q", payload.Error.Code, CodeNotFound)
	}
	if payload.Error.Message == "" {
		t.Error("expected error message from established error contract")
	}
}

func TestGetStoreBalanceReturnsErrorContractForReadFailure(t *testing.T) {
	handler := newTestRouter(Dependencies{Balance: &stubBalance{err: errBoom}})

	rec := doRequest(t, handler, authenticatedRequest(t, http.MethodGet, "/internal/v1/stores/store-123/finance/balance", "seller", testSellerToken))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	payload := decodeError(t, rec)
	if payload.Error.Code != CodeInternalError {
		t.Errorf("error code = %q, want %q", payload.Error.Code, CodeInternalError)
	}
	if payload.Error.Message != "internal error" {
		t.Errorf("error message = %q, want sanitized internal error", payload.Error.Message)
	}
}
