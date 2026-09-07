package events

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeSettlementCalculated = "settlement.calculated.v1"
	EventTypeSettlementFinalized  = "settlement.finalized.v1"
)

type SettlementCalculatedPayload struct {
	SettlementID          string    `json:"settlement_id"`
	PeriodID              string    `json:"period_id"`
	AccountID             string    `json:"account_id"`
	Currency              string    `json:"currency"`
	GrossAmountMinor      int64     `json:"gross_amount_minor"`
	AdjustmentAmountMinor int64     `json:"adjustment_amount_minor"`
	NetAmountMinor        int64     `json:"net_amount_minor"`
	Status                string    `json:"status"`
	CalculatedAt          time.Time `json:"calculated_at"`
}

type SettlementFinalizedPayload struct {
	SettlementID   string    `json:"settlement_id"`
	PeriodID       string    `json:"period_id"`
	AccountID      string    `json:"account_id"`
	Currency       string    `json:"currency"`
	NetAmountMinor int64     `json:"net_amount_minor"`
	Status         string    `json:"status"`
	FinalizedAt    time.Time `json:"finalized_at"`
}

func NewSettlementCalculatedEvent(payload SettlementCalculatedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.SettlementID == "" || payload.PeriodID == "" || payload.AccountID == "" || payload.Currency == "" {
		return EventEnvelope{}, fmt.Errorf("invalid settlement calculated payload: missing required fields")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSettlementCalculated,
		SchemaVersion:    1,
		AggregateType:    "settlement",
		AggregateID:      payload.SettlementID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.CalculatedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate settlement calculated event: %w", err)
	}

	return envelope, nil
}

func NewSettlementFinalizedEvent(payload SettlementFinalizedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.SettlementID == "" || payload.PeriodID == "" || payload.AccountID == "" || payload.Currency == "" {
		return EventEnvelope{}, fmt.Errorf("invalid settlement finalized payload: missing required fields")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSettlementFinalized,
		SchemaVersion:    1,
		AggregateType:    "settlement",
		AggregateID:      payload.SettlementID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.FinalizedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate settlement finalized event: %w", err)
	}

	return envelope, nil
}
