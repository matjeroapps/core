package payments

import (
	"errors"
	"fmt"
)

type Status string

const (
	StatusCreated    Status = "CREATED"
	StatusPending    Status = "PENDING"
	StatusAuthorized Status = "AUTHORIZED"
	StatusCaptured   Status = "CAPTURED"
	StatusFailed     Status = "FAILED"
	StatusCancelled  Status = "CANCELLED"
	StatusRefunded   Status = "REFUNDED"
)

var (
	ErrInvalidStatus     = errors.New("invalid payment status")
	ErrInvalidTransition = errors.New("invalid payment status transition")
	ErrPaymentNotFound   = errors.New("payment not found")
	ErrOrderNotFound     = errors.New("order not found")
	ErrInvalidInput      = errors.New("invalid input parameter")
)

func (s Status) Valid() bool {
	switch s {
	case StatusCreated, StatusPending, StatusAuthorized, StatusCaptured,
		StatusFailed, StatusCancelled, StatusRefunded:
		return true
	default:
		return false
	}
}

func (s Status) IsTerminal() bool {
	switch s {
	case StatusFailed, StatusCancelled, StatusRefunded:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}

	if from == to {
		return true
	}

	switch from {
	case StatusCreated:
		return to == StatusPending || to == StatusAuthorized || to == StatusCaptured || to == StatusFailed || to == StatusCancelled
	case StatusPending:
		return to == StatusAuthorized || to == StatusCaptured || to == StatusFailed || to == StatusCancelled
	case StatusAuthorized:
		return to == StatusCaptured || to == StatusFailed || to == StatusCancelled
	case StatusCaptured:
		return to == StatusRefunded
	case StatusFailed, StatusCancelled, StatusRefunded:
		return false
	default:
		return false
	}
}

func ValidateTransition(from, to Status) error {
	if !from.Valid() {
		return fmt.Errorf("%w: current status '%s'", ErrInvalidStatus, from)
	}
	if !to.Valid() {
		return fmt.Errorf("%w: target status '%s'", ErrInvalidStatus, to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: cannot transition payment from '%s' to '%s'", ErrInvalidTransition, from, to)
	}
	return nil
}
