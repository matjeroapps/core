// Package contracts holds the generic API response and request shapes shared by
// every actor API repository.
//
// Only actor-agnostic shapes live here. Actor-specific request/response DTOs
// belong to the repository that owns the corresponding HTTP surface.
package contracts

import (
	"core/modules/markets"
)

// CollectionResponse is the standard list envelope returned by list endpoints.
type CollectionResponse[T any] struct {
	Items []T `json:"items"`
}

// StatusResponse is the standard response for status mutations.
type StatusResponse struct {
	Status string `json:"status"`
}

// CountResponse is the standard aggregate-count envelope.
type CountResponse struct {
	Counts map[string]int `json:"counts"`
}

// MarketsResponse is the shared markets listing envelope served by every actor.
type MarketsResponse struct {
	Markets []markets.Market `json:"markets"`
}

// StatusUpdateRequest is the standard status mutation payload.
type StatusUpdateRequest struct {
	Status string `json:"status"`
}
