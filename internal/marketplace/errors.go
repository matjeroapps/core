package marketplace

import "errors"

var (
	ErrInvalidInput          = errors.New("invalid marketplace input")
	ErrInvalidCollectionType = errors.New("invalid marketplace collection type")
	ErrCollectionUnavailable = errors.New("marketplace collection unavailable")
)
