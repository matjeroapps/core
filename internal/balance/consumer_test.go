package balance_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/packages/events"
)

func TestConsumerValidation(t *testing.T) {
	consumer := balance.NewConsumer(nil, balance.NewRepository(nil))
	ctx := context.Background()

	t.Run("Invalid Event Envelope", func(t *testing.T) {
		err := consumer.HandleEvent(ctx, events.EventEnvelope{})
		if err == nil {
			t.Fatalf("expected error for empty envelope")
		}
	})

	t.Run("Nil Event Payload", func(t *testing.T) {
		env := events.EventEnvelope{
			EventID:          uuid.NewString(),
			EventType:        events.EventTypeJournalEntryPosted,
			SchemaVersion:    1,
			AggregateType:    "journal_entry",
			AggregateID:      uuid.NewString(),
			AggregateVersion: 1,
			OccurredAt:       time.Now(),
			Payload:          nil,
		}
		err := consumer.HandleEvent(ctx, env)
		if err != balance.ErrNilEventPayload {
			t.Fatalf("expected ErrNilEventPayload, got %v", err)
		}
	})

	t.Run("Ignored Event Type", func(t *testing.T) {
		env := events.EventEnvelope{
			EventID:          uuid.NewString(),
			EventType:        "payment.captured.v1",
			SchemaVersion:    1,
			AggregateType:    "payment",
			AggregateID:      uuid.NewString(),
			AggregateVersion: 1,
			OccurredAt:       time.Now(),
			Payload:          map[string]any{"foo": "bar"},
		}
		err := consumer.HandleEvent(ctx, env)
		if err != nil {
			t.Fatalf("expected nil for ignored event type, got %v", err)
		}
	})

	t.Run("Uninitialized DB Pool", func(t *testing.T) {
		payload := events.JournalEntryPostedPayload{
			JournalEntryID: uuid.NewString(),
			ReferenceType:  "PAYMENT_CAPTURED",
			ReferenceID:    "pay_123",
			Currency:       "SAR",
			PostedAt:       time.Now(),
			Lines: []events.JournalEntryPostedLinePayload{
				{
					AccountID:        uuid.NewString(),
					DebitAmountMinor: 1000,
				},
			},
		}

		env, err := events.NewJournalEntryPostedEvent(payload, "corr-1", "caus-1")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		err = consumer.HandleEvent(ctx, env)
		if err == nil || err.Error() != "database pool is not initialized" {
			t.Fatalf("expected uninitialized db pool error, got %v", err)
		}
	})
}
