package settlement

import (
	"context"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/balance"
)

func TestCalculator(t *testing.T) {
	calc := NewCalculator()
	ctx := context.Background()

	t.Run("Valid balance calculation", func(t *testing.T) {
		bal := balance.AccountBalance{
			ID:               "bal-1",
			AccountID:        "acc-123",
			Currency:         "USD",
			DebitTotalMinor:  10000,
			CreditTotalMinor: 2000,
			BalanceMinor:     8000,
			UpdatedAt:        time.Now().UTC(),
		}

		st, err := calc.Calculate(ctx, "per-456", bal)
		if err != nil {
			t.Fatalf("unexpected calculation error: %v", err)
		}

		if st.SettlementPeriodID != "per-456" {
			t.Errorf("expected period id per-456, got %s", st.SettlementPeriodID)
		}
		if st.AccountID != "acc-123" {
			t.Errorf("expected account id acc-123, got %s", st.AccountID)
		}
		if st.Currency != "USD" {
			t.Errorf("expected currency USD, got %s", st.Currency)
		}
		if st.GrossAmountMinor != 8000 {
			t.Errorf("expected gross amount 8000, got %d", st.GrossAmountMinor)
		}
		if st.AdjustmentAmountMinor != 0 {
			t.Errorf("expected adjustment amount 0, got %d", st.AdjustmentAmountMinor)
		}
		if st.NetAmountMinor != 8000 {
			t.Errorf("expected net amount 8000, got %d", st.NetAmountMinor)
		}
		if st.Status != StatusCalculated {
			t.Errorf("expected status CALCULATED, got %s", st.Status)
		}
	})

	t.Run("Invalid missing period id", func(t *testing.T) {
		bal := balance.AccountBalance{
			AccountID: "acc-123",
			Currency:  "USD",
		}
		_, err := calc.Calculate(ctx, "", bal)
		if err == nil {
			t.Error("expected error for missing period id, got nil")
		}
	})

	t.Run("Invalid missing account id", func(t *testing.T) {
		bal := balance.AccountBalance{
			AccountID: "",
			Currency:  "USD",
		}
		_, err := calc.Calculate(ctx, "per-123", bal)
		if err == nil {
			t.Error("expected error for missing account id, got nil")
		}
	})
}
