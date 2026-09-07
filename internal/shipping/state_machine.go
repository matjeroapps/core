package shipping

import (
	"errors"
	"fmt"
)

type Status string

const (
	StatusPending        Status = "PENDING"
	StatusProcessing     Status = "PROCESSING"
	StatusReadyForPickup Status = "READY_FOR_PICKUP"
	StatusShipped        Status = "SHIPPED"
	StatusOutForDelivery Status = "OUT_FOR_DELIVERY"
	StatusDelivered      Status = "DELIVERED"
	StatusFailed         Status = "FAILED"
	StatusReturned       Status = "RETURNED"
)

var (
	ErrInvalidStatus     = errors.New("invalid shipment status")
	ErrInvalidTransition = errors.New("invalid shipment status transition")
	ErrShipmentNotFound  = errors.New("shipment not found")
	ErrOrderNotFound     = errors.New("order not found")
	ErrInvalidInput      = errors.New("invalid input parameter")
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusProcessing, StatusReadyForPickup, StatusShipped,
		StatusOutForDelivery, StatusDelivered, StatusFailed, StatusReturned:
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
	case StatusPending:
		return to == StatusProcessing || to == StatusFailed
	case StatusProcessing:
		return to == StatusReadyForPickup || to == StatusShipped || to == StatusFailed
	case StatusReadyForPickup:
		return to == StatusShipped || to == StatusFailed
	case StatusShipped:
		return to == StatusOutForDelivery || to == StatusDelivered || to == StatusFailed || to == StatusReturned
	case StatusOutForDelivery:
		return to == StatusDelivered || to == StatusFailed || to == StatusReturned
	case StatusDelivered:
		return to == StatusReturned
	case StatusFailed:
		return to == StatusProcessing || to == StatusReturned
	case StatusReturned:
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
		return fmt.Errorf("%w: cannot transition shipment from '%s' to '%s'", ErrInvalidTransition, from, to)
	}
	return nil
}
