package events

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypePaymentCaptured = "payments.payment.captured.v1"
	EventTypePaymentFailed   = "payments.payment.failed.v1"
)

type PaymentCapturedPayload struct {
	PaymentID         string    `json:"payment_id"`
	OrderID           string    `json:"order_id"`
	AmountMinor       int64     `json:"amount_minor"`
	Currency          string    `json:"currency"`
	PaymentMethod     string    `json:"payment_method"`
	Provider          string    `json:"provider,omitempty"`
	ProviderReference string    `json:"provider_reference,omitempty"`
	CapturedAt        time.Time `json:"captured_at"`
}

type PaymentFailedPayload struct {
	PaymentID     string    `json:"payment_id"`
	OrderID       string    `json:"order_id"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	PaymentMethod string    `json:"payment_method"`
	Provider      string    `json:"provider,omitempty"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	FailedAt      time.Time `json:"failed_at"`
}

func NewPaymentCapturedEvent(payload PaymentCapturedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.PaymentID == "" || payload.OrderID == "" {
		return EventEnvelope{}, fmt.Errorf("invalid payment captured payload")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypePaymentCaptured,
		SchemaVersion:    1,
		AggregateType:    "payment",
		AggregateID:      payload.PaymentID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.CapturedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate payment captured event: %w", err)
	}

	return envelope, nil
}

func NewPaymentFailedEvent(payload PaymentFailedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.PaymentID == "" || payload.OrderID == "" {
		return EventEnvelope{}, fmt.Errorf("invalid payment failed payload")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypePaymentFailed,
		SchemaVersion:    1,
		AggregateType:    "payment",
		AggregateID:      payload.PaymentID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.FailedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate payment failed event: %w", err)
	}

	return envelope, nil
}
