package balance

import "errors"

var (
	ErrAccountNotFound               = errors.New("ledger account not found")
	ErrBalanceNotFound               = errors.New("account balance not found")
	ErrInvalidEventPayload           = errors.New("invalid event payload")
	ErrNilEventPayload               = errors.New("event payload cannot be nil")
	ErrInvalidAccountID              = errors.New("account_id is required")
	ErrInvalidCurrency               = errors.New("currency must be a valid 3-letter code")
	ErrSettlementPeriodNotFound      = errors.New("settlement period not found")
	ErrInvalidSettlementPeriodDates  = errors.New("settlement period end_date must be after start_date")
	ErrSettlementPeriodAlreadyClosed = errors.New("settlement period is already closed")
)
