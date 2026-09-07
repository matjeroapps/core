package marketplace_finance

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/internal/settlement"
)

type Calculator struct{}

func NewCalculator() *Calculator {
	return &Calculator{}
}

func (c *Calculator) CalculateAllocations(st *settlement.Settlement, rules []FinancialRule) ([]SettlementAllocation, error) {
	if st == nil {
		return nil, ErrSettlementNotFound
	}

	if st.Status == settlement.StatusFinalized {
		return nil, ErrSettlementFinalized
	}

	if st.GrossAmountMinor < 0 {
		return nil, ErrNegativeAllocation
	}

	settlementCurrency := strings.ToUpper(strings.TrimSpace(st.Currency))
	if len(settlementCurrency) != 3 {
		return nil, ErrInvalidCurrency
	}

	// Maps allocation_type -> SettlementAllocation
	allocMap := make(map[AllocationType]SettlementAllocation)
	var explicitSellerRule *FinancialRule

	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return nil, err
		}

		if rule.Status != RuleStatusActive {
			return nil, ErrRuleInactive
		}

		if strings.ToUpper(strings.TrimSpace(rule.Currency)) != settlementCurrency {
			return nil, ErrInvalidCurrency
		}

		if _, exists := allocMap[rule.AllocationType]; exists {
			return nil, ErrDuplicateAllocation
		}

		if rule.AllocationType == AllocationTypeSellerShare {
			explicitRule := rule
			explicitSellerRule = &explicitRule
			continue
		}

		var amountMinor int64
		switch rule.RuleType {
		case RuleTypePercentage:
			calculated := math.Round(float64(st.GrossAmountMinor) * (rule.Percentage / 100.0))
			amountMinor = int64(calculated)
		case RuleTypeFixedAmount:
			amountMinor = rule.FixedAmountMinor
		default:
			return nil, ErrInvalidRuleType
		}

		if amountMinor < 0 {
			return nil, ErrNegativeAllocation
		}

		allocMap[rule.AllocationType] = SettlementAllocation{
			ID:             uuid.NewString(),
			SettlementID:   st.ID,
			AccountID:      st.AccountID,
			AllocationType: rule.AllocationType,
			AmountMinor:    amountMinor,
			Currency:       settlementCurrency,
			CreatedAt:      time.Now().UTC(),
		}
	}

	// Compute total non-seller allocation
	var nonSellerTotal int64
	for allocType, alloc := range allocMap {
		if allocType != AllocationTypeSellerShare {
			nonSellerTotal += alloc.AmountMinor
		}
	}

	var sellerAmount int64
	if explicitSellerRule != nil {
		switch explicitSellerRule.RuleType {
		case RuleTypePercentage:
			calculated := math.Round(float64(st.GrossAmountMinor) * (explicitSellerRule.Percentage / 100.0))
			sellerAmount = int64(calculated)
		case RuleTypeFixedAmount:
			sellerAmount = explicitSellerRule.FixedAmountMinor
		}
	} else {
		sellerAmount = st.GrossAmountMinor - nonSellerTotal
	}

	if sellerAmount < 0 {
		return nil, ErrNegativeAllocation
	}

	allocMap[AllocationTypeSellerShare] = SettlementAllocation{
		ID:             uuid.NewString(),
		SettlementID:   st.ID,
		AccountID:      st.AccountID,
		AllocationType: AllocationTypeSellerShare,
		AmountMinor:    sellerAmount,
		Currency:       settlementCurrency,
		CreatedAt:      time.Now().UTC(),
	}

	// Total verification
	var totalAllocated int64
	allocations := make([]SettlementAllocation, 0, len(allocMap))
	for _, alloc := range allocMap {
		if err := alloc.Validate(); err != nil {
			return nil, err
		}
		totalAllocated += alloc.AmountMinor
		allocations = append(allocations, alloc)
	}

	if totalAllocated != st.GrossAmountMinor {
		return nil, ErrAllocationTotalMismatch
	}

	return allocations, nil
}
