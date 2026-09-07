package settlement

import "errors"

var (
	ErrSettlementNotFound           = errors.New("settlement not found")
	ErrSettlementPeriodNotFound     = errors.New("settlement period not found")
	ErrInvalidAccountID             = errors.New("invalid account id")
	ErrInvalidSettlementPeriodID    = errors.New("invalid settlement period id")
	ErrInvalidPeriodState           = errors.New("invalid settlement period state transition")
	ErrInvalidSettlementState       = errors.New("invalid settlement state transition")
	ErrFinalizedSettlementImmutable = errors.New("finalized settlement is immutable")
	ErrSettlementAlreadyFinalized   = errors.New("settlement period is already finalized")
	ErrNoAccountBalances            = errors.New("no account balances found for settlement calculation")
)
