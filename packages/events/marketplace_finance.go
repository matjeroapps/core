package events

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeAllocationCalculated = "marketplace_finance.allocation.calculated.v1"
)

type AllocationCalculatedPayload struct {
	SettlementID   string    `json:"settlement_id"`
	AllocationID   string    `json:"allocation_id"`
	AccountID      string    `json:"account_id"`
	AllocationType string    `json:"allocation_type"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	CalculatedAt   time.Time `json:"calculated_at"`
}

func NewAllocationCalculatedEvent(payload AllocationCalculatedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.SettlementID == "" || payload.AllocationID == "" || payload.AccountID == "" || payload.AllocationType == "" || payload.Currency == "" {
		return EventEnvelope{}, fmt.Errorf("invalid allocation calculated payload: missing required fields")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeAllocationCalculated,
		SchemaVersion:    1,
		AggregateType:    "settlement_allocation",
		AggregateID:      payload.AllocationID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.CalculatedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate allocation calculated event: %w", err)
	}

	return envelope, nil
}
