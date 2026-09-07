package marketplace_finance

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/internal/settlement"
)

func TestCalculator_CalculateAllocations(t *testing.T) {
	calc := NewCalculator()
	validSettlementID := uuid.NewString()
	validAccountID := uuid.NewString()

	t.Run("single percentage rule (platform fee 10%)", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:             uuid.NewString(),
				Name:           "Platform Fee 10%",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
		}

		allocs, err := calc.CalculateAllocations(st, rules)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(allocs) != 2 {
			t.Fatalf("expected 2 allocations, got %d", len(allocs))
		}

		var platformAlloc, sellerAlloc *SettlementAllocation
		for i := range allocs {
			if allocs[i].AllocationType == AllocationTypePlatformShare {
				platformAlloc = &allocs[i]
			} else if allocs[i].AllocationType == AllocationTypeSellerShare {
				sellerAlloc = &allocs[i]
			}
		}

		if platformAlloc == nil || platformAlloc.AmountMinor != 100 {
			t.Errorf("expected platform allocation 100, got %v", platformAlloc)
		}
		if sellerAlloc == nil || sellerAlloc.AmountMinor != 900 {
			t.Errorf("expected seller allocation 900, got %v", sellerAlloc)
		}

		// Verify total equals settlement gross amount
		total := platformAlloc.AmountMinor + sellerAlloc.AmountMinor
		if total != st.GrossAmountMinor {
			t.Errorf("total %d does not equal gross %d", total, st.GrossAmountMinor)
		}
	})

	t.Run("multiple rules (platform fee 10%, supplier share 30%)", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:             uuid.NewString(),
				Name:           "Platform Fee 10%",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			{
				ID:             uuid.NewString(),
				Name:           "Supplier Share 30%",
				RuleType:       RuleTypePercentage,
				Percentage:     30.0,
				Currency:       "USD",
				AllocationType: AllocationTypeSupplierShare,
				Status:         RuleStatusActive,
			},
		}

		allocs, err := calc.CalculateAllocations(st, rules)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var platform, supplier, seller int64
		for _, a := range allocs {
			switch a.AllocationType {
			case AllocationTypePlatformShare:
				platform = a.AmountMinor
			case AllocationTypeSupplierShare:
				supplier = a.AmountMinor
			case AllocationTypeSellerShare:
				seller = a.AmountMinor
			}
		}

		if platform != 100 || supplier != 300 || seller != 600 {
			t.Errorf("expected platform 100, supplier 300, seller 600; got platform %d, supplier %d, seller %d",
				platform, supplier, seller)
		}
	})

	t.Run("fixed amount platform fee", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 5000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:               uuid.NewString(),
				Name:             "Platform Fixed Fee",
				RuleType:         RuleTypeFixedAmount,
				FixedAmountMinor: 250,
				Currency:         "USD",
				AllocationType:   AllocationTypePlatformShare,
				Status:           RuleStatusActive,
			},
		}

		allocs, err := calc.CalculateAllocations(st, rules)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var platform, seller int64
		for _, a := range allocs {
			if a.AllocationType == AllocationTypePlatformShare {
				platform = a.AmountMinor
			} else if a.AllocationType == AllocationTypeSellerShare {
				seller = a.AmountMinor
			}
		}

		if platform != 250 || seller != 4750 {
			t.Errorf("expected platform 250, seller 4750; got platform %d, seller %d", platform, seller)
		}
	})

	t.Run("inactive rule rejected", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:             uuid.NewString(),
				Name:           "Inactive Fee",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusInactive,
			},
		}

		_, err := calc.CalculateAllocations(st, rules)
		if err != ErrRuleInactive {
			t.Errorf("expected ErrRuleInactive, got %v", err)
		}
	})

	t.Run("finalized settlement rejected", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusFinalized,
			CalculatedAt:     &time.Time{},
			FinalizedAt:      &time.Time{},
		}

		_, err := calc.CalculateAllocations(st, nil)
		if err != ErrSettlementFinalized {
			t.Errorf("expected ErrSettlementFinalized, got %v", err)
		}
	})

	t.Run("currency mismatch rejected", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:             uuid.NewString(),
				Name:           "SAR Rule",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "SAR",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
		}

		_, err := calc.CalculateAllocations(st, rules)
		if err != ErrInvalidCurrency {
			t.Errorf("expected ErrInvalidCurrency, got %v", err)
		}
	})

	t.Run("total allocations exceeding gross amount rejected", func(t *testing.T) {
		st := &settlement.Settlement{
			ID:               validSettlementID,
			AccountID:        validAccountID,
			Currency:         "USD",
			GrossAmountMinor: 1000,
			Status:           settlement.StatusCalculated,
		}

		rules := []FinancialRule{
			{
				ID:             uuid.NewString(),
				Name:           "Platform Fee 80%",
				RuleType:       RuleTypePercentage,
				Percentage:     80.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			{
				ID:             uuid.NewString(),
				Name:           "Supplier Share 40%",
				RuleType:       RuleTypePercentage,
				Percentage:     40.0,
				Currency:       "USD",
				AllocationType: AllocationTypeSupplierShare,
				Status:         RuleStatusActive,
			},
		}

		_, err := calc.CalculateAllocations(st, rules)
		if err != ErrNegativeAllocation && err != ErrAllocationTotalMismatch {
			t.Errorf("expected negative allocation or total mismatch error, got %v", err)
		}
	})
}
