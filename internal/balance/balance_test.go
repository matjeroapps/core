package balance_test

import (
	"context"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/balance"
)

func TestBalanceDomainAndValidation(t *testing.T) {
	t.Run("SettlementPeriodStatus Validation", func(t *testing.T) {
		if !balance.SettlementPeriodStatusOpen.IsValid() {
			t.Errorf("expected OPEN to be valid")
		}
		if !balance.SettlementPeriodStatusClosed.IsValid() {
			t.Errorf("expected CLOSED to be valid")
		}
		if balance.SettlementPeriodStatus("INVALID").IsValid() {
			t.Errorf("expected INVALID to be invalid")
		}
	})

	t.Run("CreateSettlementPeriod Validation", func(t *testing.T) {
		repo := balance.NewRepository(nil)
		svc := balance.NewService(repo)
		ctx := context.Background()

		// Zero dates
		_, err := svc.CreateSettlementPeriod(ctx, balance.CreateSettlementPeriodParams{})
		if err != balance.ErrInvalidSettlementPeriodDates {
			t.Errorf("expected ErrInvalidSettlementPeriodDates, got %v", err)
		}

		// End date before start date
		now := time.Now()
		_, err = svc.CreateSettlementPeriod(ctx, balance.CreateSettlementPeriodParams{
			StartDate: now,
			EndDate:   now.Add(-1 * time.Hour),
		})
		if err != balance.ErrInvalidSettlementPeriodDates {
			t.Errorf("expected ErrInvalidSettlementPeriodDates, got %v", err)
		}

		// Empty GetAccountBalance
		_, err = svc.GetAccountBalance(ctx, "")
		if err != balance.ErrInvalidAccountID {
			t.Errorf("expected ErrInvalidAccountID, got %v", err)
		}

		// Empty GetSettlementPeriod
		_, err = svc.GetSettlementPeriod(ctx, "")
		if err != balance.ErrSettlementPeriodNotFound {
			t.Errorf("expected ErrSettlementPeriodNotFound, got %v", err)
		}

		// Empty CloseSettlementPeriod
		_, err = svc.CloseSettlementPeriod(ctx, "")
		if err != balance.ErrSettlementPeriodNotFound {
			t.Errorf("expected ErrSettlementPeriodNotFound, got %v", err)
		}
	})
}
