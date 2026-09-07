package events_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/packages/events"
)

func TestNewJournalEntryPostedEvent(t *testing.T) {
	entryID := uuid.NewString()
	now := time.Now().UTC()

	payload := events.JournalEntryPostedPayload{
		JournalEntryID: entryID,
		ReferenceType:  "PAYMENT",
		ReferenceID:    uuid.NewString(),
		Description:    "Payment ledger record",
		Currency:       "SAR",
		PostedAt:       now,
		Lines: []events.JournalEntryPostedLinePayload{
			{AccountID: uuid.NewString(), DebitAmountMinor: 1000, CreditAmountMinor: 0},
			{AccountID: uuid.NewString(), DebitAmountMinor: 0, CreditAmountMinor: 1000},
		},
	}

	env, err := events.NewJournalEntryPostedEvent(payload, "corr-123", "caus-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env.EventType != events.EventTypeJournalEntryPosted {
		t.Errorf("expected event type %s, got %s", events.EventTypeJournalEntryPosted, env.EventType)
	}
	if env.AggregateID != entryID {
		t.Errorf("expected aggregate id %s, got %s", entryID, env.AggregateID)
	}
	if env.CorrelationID != "corr-123" || env.CausationID != "caus-456" {
		t.Errorf("correlation/causation mismatch")
	}
}
