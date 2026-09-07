package marketplace_finance

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type RuleType string

const (
	RuleTypePercentage  RuleType = "PERCENTAGE"
	RuleTypeFixedAmount RuleType = "FIXED_AMOUNT"
)

func (t RuleType) IsValid() bool {
	switch t {
	case RuleTypePercentage, RuleTypeFixedAmount:
		return true
	default:
		return false
	}
}

type RuleStatus string

const (
	RuleStatusActive   RuleStatus = "ACTIVE"
	RuleStatusInactive RuleStatus = "INACTIVE"
)

func (s RuleStatus) IsValid() bool {
	switch s {
	case RuleStatusActive, RuleStatusInactive:
		return true
	default:
		return false
	}
}

type AllocationType string

const (
	AllocationTypeSellerShare   AllocationType = "SELLER_SHARE"
	AllocationTypeSupplierShare AllocationType = "SUPPLIER_SHARE"
	AllocationTypePlatformShare AllocationType = "PLATFORM_SHARE"
)

func (a AllocationType) IsValid() bool {
	switch a {
	case AllocationTypeSellerShare, AllocationTypeSupplierShare, AllocationTypePlatformShare:
		return true
	default:
		return false
	}
}

type FinancialRule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	RuleType         RuleType       `json:"rule_type"`
	Percentage       float64        `json:"percentage"`
	FixedAmountMinor int64          `json:"fixed_amount_minor"`
	Currency         string         `json:"currency"`
	AllocationType   AllocationType `json:"allocation_type"`
	Status           RuleStatus     `json:"status"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func (r *FinancialRule) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return ErrInvalidRuleType
	}
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if len(r.Currency) != 3 {
		return ErrInvalidCurrency
	}

	if !r.RuleType.IsValid() {
		return ErrInvalidRuleType
	}

	switch r.RuleType {
	case RuleTypePercentage:
		if r.Percentage < 0 || r.Percentage > 100 {
			return ErrInvalidPercentage
		}
	case RuleTypeFixedAmount:
		if r.FixedAmountMinor < 0 {
			return ErrInvalidFixedAmount
		}
	}

	if !r.AllocationType.IsValid() {
		return ErrInvalidAllocationType
	}

	if !r.Status.IsValid() {
		return ErrRuleInactive
	}

	return nil
}

type SettlementAllocation struct {
	ID             string         `json:"id"`
	SettlementID   string         `json:"settlement_id"`
	AccountID      string         `json:"account_id"`
	AllocationType AllocationType `json:"allocation_type"`
	AmountMinor    int64          `json:"amount_minor"`
	Currency       string         `json:"currency"`
	CreatedAt      time.Time      `json:"created_at"`
}

func (a *SettlementAllocation) Validate() error {
	if _, err := uuid.Parse(a.SettlementID); err != nil {
		return ErrSettlementNotFound
	}
	if _, err := uuid.Parse(a.AccountID); err != nil {
		return ErrSettlementNotFound
	}
	if a.AmountMinor < 0 {
		return ErrNegativeAllocation
	}
	if len(strings.TrimSpace(a.Currency)) != 3 {
		return ErrInvalidCurrency
	}
	if !a.AllocationType.IsValid() {
		return ErrInvalidAllocationType
	}
	return nil
}
