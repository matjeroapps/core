package marketplace_finance

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFinancialRuleValidation(t *testing.T) {
	tests := []struct {
		name    string
		rule    FinancialRule
		wantErr error
	}{
		{
			name: "valid percentage rule",
			rule: FinancialRule{
				Name:           "Platform Fee",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			wantErr: nil,
		},
		{
			name: "valid fixed amount rule",
			rule: FinancialRule{
				Name:             "Platform Fixed Fee",
				RuleType:         RuleTypeFixedAmount,
				FixedAmountMinor: 500,
				Currency:         "USD",
				AllocationType:   AllocationTypePlatformShare,
				Status:           RuleStatusActive,
			},
			wantErr: nil,
		},
		{
			name: "invalid percentage > 100",
			rule: FinancialRule{
				Name:           "Invalid Fee",
				RuleType:       RuleTypePercentage,
				Percentage:     150.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			wantErr: ErrInvalidPercentage,
		},
		{
			name: "invalid percentage < 0",
			rule: FinancialRule{
				Name:           "Invalid Fee",
				RuleType:       RuleTypePercentage,
				Percentage:     -5.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			wantErr: ErrInvalidPercentage,
		},
		{
			name: "invalid fixed amount < 0",
			rule: FinancialRule{
				Name:             "Invalid Fee",
				RuleType:         RuleTypeFixedAmount,
				FixedAmountMinor: -100,
				Currency:         "USD",
				AllocationType:   AllocationTypePlatformShare,
				Status:           RuleStatusActive,
			},
			wantErr: ErrInvalidFixedAmount,
		},
		{
			name: "invalid currency length",
			rule: FinancialRule{
				Name:           "Invalid Currency",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "US",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatusActive,
			},
			wantErr: ErrInvalidCurrency,
		},
		{
			name: "invalid allocation type",
			rule: FinancialRule{
				Name:           "Invalid Allocation",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationType("UNKNOWN"),
				Status:         RuleStatusActive,
			},
			wantErr: ErrInvalidAllocationType,
		},
		{
			name: "invalid status",
			rule: FinancialRule{
				Name:           "Invalid Status",
				RuleType:       RuleTypePercentage,
				Percentage:     10.0,
				Currency:       "USD",
				AllocationType: AllocationTypePlatformShare,
				Status:         RuleStatus("INVALID"),
			},
			wantErr: ErrRuleInactive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSettlementAllocationValidation(t *testing.T) {
	validUUID := uuid.NewString()

	tests := []struct {
		name    string
		alloc   SettlementAllocation
		wantErr error
	}{
		{
			name: "valid allocation",
			alloc: SettlementAllocation{
				SettlementID:   validUUID,
				AccountID:      validUUID,
				AllocationType: AllocationTypeSellerShare,
				AmountMinor:    900,
				Currency:       "USD",
				CreatedAt:      time.Now(),
			},
			wantErr: nil,
		},
		{
			name: "negative amount",
			alloc: SettlementAllocation{
				SettlementID:   validUUID,
				AccountID:      validUUID,
				AllocationType: AllocationTypeSellerShare,
				AmountMinor:    -10,
				Currency:       "USD",
			},
			wantErr: ErrNegativeAllocation,
		},
		{
			name: "invalid settlement UUID",
			alloc: SettlementAllocation{
				SettlementID:   "invalid",
				AccountID:      validUUID,
				AllocationType: AllocationTypeSellerShare,
				AmountMinor:    100,
				Currency:       "USD",
			},
			wantErr: ErrSettlementNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.alloc.Validate()
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
