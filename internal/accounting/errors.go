package accounting

import "errors"

var (
	ErrUnsupportedEventType  = errors.New("unsupported event type for payment accounting")
	ErrMissingAccountMapping = errors.New("system account mapping not found")
	ErrInvalidEventPayload   = errors.New("invalid payment event payload")
	ErrNilEventPayload       = errors.New("event payload is nil")
)
