package accounting_test

import (
	"errors"
	"testing"

	"github.com/matjeroapps/core/internal/accounting"
	"github.com/matjeroapps/core/packages/events"
)

func TestPaymentAccountingRules(t *testing.T) {
	engine := accounting.NewRuleEngine()

	t.Run("Payment Captured Event Rule", func(t *testing.T) {
		payload := map[string]any{
			"payment_id":         "pay_123",
			"order_id":           "ord_456",
			"amount_minor":       int64(15000),
			"currency":           "SAR",
			"payment_method":     "CREDIT_CARD",
			"provider":           "CHECKOUT",
			"provider_reference": "ref_789",
		}

		rule, err := engine.ResolveRule(events.EventTypePaymentCaptured, payload)
		if err != nil {
			t.Fatalf("unexpected error resolving captured payment rule: %v", err)
		}

		if !rule.ShouldPost {
			t.Errorf("expected ShouldPost to be true for captured payment")
		}
		if rule.ReferenceType != "PAYMENT_CAPTURED" {
			t.Errorf("expected ReferenceType PAYMENT_CAPTURED, got %s", rule.ReferenceType)
		}
		if rule.ReferenceID != "pay_123" {
			t.Errorf("expected ReferenceID pay_123, got %s", rule.ReferenceID)
		}
		if rule.AmountMinor != 15000 {
			t.Errorf("expected AmountMinor 15000, got %d", rule.AmountMinor)
		}
		if rule.Currency != "SAR" {
			t.Errorf("expected Currency SAR, got %s", rule.Currency)
		}
		if rule.DebitAccountRole != accounting.RolePaymentClearing {
			t.Errorf("expected DebitAccountRole PAYMENT_CLEARING, got %s", rule.DebitAccountRole)
		}
		if rule.CreditAccountRole != accounting.RoleCustomerFunds {
			t.Errorf("expected CreditAccountRole CUSTOMER_FUNDS, got %s", rule.CreditAccountRole)
		}
		if rule.Description != "Payment captured for order ord_456" {
			t.Errorf("expected description 'Payment captured for order ord_456', got '%s'", rule.Description)
		}
	})

	t.Run("Payment Captured Event Rule Invalid Payload", func(t *testing.T) {
		invalidPayloads := []map[string]any{
			{"payment_id": "", "order_id": "ord_456", "amount_minor": int64(100), "currency": "SAR"},
			{"payment_id": "pay_123", "order_id": "", "amount_minor": int64(100), "currency": "SAR"},
			{"payment_id": "pay_123", "order_id": "ord_456", "amount_minor": int64(0), "currency": "SAR"},
			{"payment_id": "pay_123", "order_id": "ord_456", "amount_minor": int64(100), "currency": ""},
		}

		for i, payload := range invalidPayloads {
			_, err := engine.ResolveRule(events.EventTypePaymentCaptured, payload)
			if err == nil {
				t.Errorf("case %d: expected error for invalid payload, got nil", i)
			}
			if !errors.Is(err, accounting.ErrInvalidEventPayload) {
				t.Errorf("case %d: expected ErrInvalidEventPayload, got %v", i, err)
			}
		}
	})

	t.Run("Payment Failed Event Rule", func(t *testing.T) {
		payload := map[string]any{
			"payment_id":    "pay_999",
			"order_id":      "ord_888",
			"amount_minor":  int64(20000),
			"currency":      "SAR",
			"error_message": "Insufficient funds",
		}

		rule, err := engine.ResolveRule(events.EventTypePaymentFailed, payload)
		if err != nil {
			t.Fatalf("unexpected error resolving failed payment rule: %v", err)
		}

		if rule.ShouldPost {
			t.Errorf("expected ShouldPost to be false for failed payment")
		}
		if rule.Reason == "" {
			t.Errorf("expected non-empty reason for not posting")
		}
	})

	t.Run("Unsupported Event Type", func(t *testing.T) {
		_, err := engine.ResolveRule("unknown.event.type.v1", map[string]any{})
		if err == nil {
			t.Fatalf("expected error for unsupported event type, got nil")
		}
		if !errors.Is(err, accounting.ErrUnsupportedEventType) {
			t.Fatalf("expected ErrUnsupportedEventType, got %v", err)
		}
	})
}
