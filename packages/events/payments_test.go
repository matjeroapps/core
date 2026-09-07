package events_test

import (
	"testing"
	"time"

	"github.com/matjeroapps/core/packages/events"
)

func TestNewPaymentCapturedEvent(t *testing.T) {
	now := time.Now().UTC()
	payload := events.PaymentCapturedPayload{
		PaymentID:         "pay-123",
		OrderID:           "ord-456",
		AmountMinor:       1000,
		Currency:          "SAR",
		PaymentMethod:     "card",
		Provider:          "tap",
		ProviderReference: "chg_789",
		CapturedAt:        now,
	}

	env, err := events.NewPaymentCapturedEvent(payload, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if env.EventType != events.EventTypePaymentCaptured {
		t.Errorf("expected event type %s, got %s", events.EventTypePaymentCaptured, env.EventType)
	}
	if env.AggregateID != "pay-123" {
		t.Errorf("expected aggregate id pay-123, got %s", env.AggregateID)
	}

	// Test invalid payload
	_, err = events.NewPaymentCapturedEvent(events.PaymentCapturedPayload{}, "corr-1", "caus-1")
	if err == nil {
		t.Errorf("expected error for empty payload")
	}
}

func TestNewPaymentFailedEvent(t *testing.T) {
	now := time.Now().UTC()
	payload := events.PaymentFailedPayload{
		PaymentID:     "pay-123",
		OrderID:       "ord-456",
		AmountMinor:   1000,
		Currency:      "SAR",
		PaymentMethod: "card",
		Provider:      "tap",
		ErrorMessage:  "card_declined",
		FailedAt:      now,
	}

	env, err := events.NewPaymentFailedEvent(payload, "corr-1", "caus-1")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if env.EventType != events.EventTypePaymentFailed {
		t.Errorf("expected event type %s, got %s", events.EventTypePaymentFailed, env.EventType)
	}

	// Test invalid payload
	_, err = events.NewPaymentFailedEvent(events.PaymentFailedPayload{}, "corr-1", "caus-1")
	if err == nil {
		t.Errorf("expected error for empty payload")
	}
}
