package settlement

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/balance"
)

type Calculator interface {
	Calculate(ctx context.Context, periodID string, accountBalance balance.AccountBalance) (*Settlement, error)
}

type calculator struct{}

func NewCalculator() Calculator {
	return &calculator{}
}

func (c *calculator) Calculate(ctx context.Context, periodID string, bal balance.AccountBalance) (*Settlement, error) {
	if periodID == "" {
		return nil, ErrInvalidSettlementPeriodID
	}
	if bal.AccountID == "" {
		return nil, ErrInvalidAccountID
	}

	// Initial calculation rule:
	// Settlement amount is derived directly from ledger balance projection.
	// gross_amount_minor = account balance projection
	// adjustment_amount_minor = 0 (no fees/commissions/taxes/splits)
	// net_amount_minor = gross + adjustment
	grossAmount := bal.BalanceMinor
	adjustmentAmount := int64(0)
	netAmount := grossAmount + adjustmentAmount

	now := time.Now().UTC()

	s := Settlement{
		ID:                    uuid.NewString(),
		SettlementPeriodID:    periodID,
		AccountID:             bal.AccountID,
		Currency:              bal.Currency,
		GrossAmountMinor:      grossAmount,
		AdjustmentAmountMinor: adjustmentAmount,
		NetAmountMinor:        netAmount,
		Status:                StatusCalculated,
		CreatedAt:             now,
		CalculatedAt:          &now,
	}

	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("calculated settlement invalid: %w", err)
	}

	return &s, nil
}
