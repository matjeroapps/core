package marketplace_finance

import "errors"

var (
	ErrRuleNotFound            = errors.New("financial rule not found")
	ErrInvalidRuleType         = errors.New("invalid financial rule type")
	ErrInvalidPercentage       = errors.New("percentage must be between 0 and 100")
	ErrInvalidFixedAmount      = errors.New("fixed amount minor must be non-negative")
	ErrInvalidCurrency         = errors.New("invalid or mismatched currency")
	ErrInvalidAllocationType   = errors.New("invalid allocation type")
	ErrRuleInactive            = errors.New("financial rule is inactive")
	ErrSettlementNotFound      = errors.New("settlement not found")
	ErrSettlementFinalized     = errors.New("allocations cannot be calculated for finalized settlement")
	ErrAllocationTotalMismatch = errors.New("total allocations do not equal settlement gross amount")
	ErrNegativeAllocation      = errors.New("allocation amount cannot be negative")
	ErrDuplicateAllocation     = errors.New("duplicate allocation type for settlement")
)
