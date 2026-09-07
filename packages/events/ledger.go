package events

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventTypeJournalEntryPosted = "ledger.journal_entry.posted.v1"
)

type JournalEntryPostedLinePayload struct {
	AccountID         string `json:"account_id"`
	DebitAmountMinor  int64  `json:"debit_amount_minor"`
	CreditAmountMinor int64  `json:"credit_amount_minor"`
}

type JournalEntryPostedPayload struct {
	JournalEntryID string                          `json:"journal_entry_id"`
	ReferenceType  string                          `json:"reference_type"`
	ReferenceID    string                          `json:"reference_id"`
	Description    string                          `json:"description,omitempty"`
	Currency       string                          `json:"currency"`
	PostedAt       time.Time                       `json:"posted_at"`
	Lines          []JournalEntryPostedLinePayload `json:"lines"`
}

func NewJournalEntryPostedEvent(payload JournalEntryPostedPayload, correlationID, causationID string) (EventEnvelope, error) {
	if payload.JournalEntryID == "" || payload.ReferenceType == "" || payload.ReferenceID == "" || payload.Currency == "" {
		return EventEnvelope{}, fmt.Errorf("invalid journal entry posted payload")
	}

	if len(payload.Lines) == 0 {
		return EventEnvelope{}, fmt.Errorf("journal entry posted event must contain lines")
	}

	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return EventEnvelope{}, err
	}

	envelope := EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeJournalEntryPosted,
		SchemaVersion:    1,
		AggregateType:    "journal_entry",
		AggregateID:      payload.JournalEntryID,
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       payload.PostedAt,
		Payload:          payloadMap,
	}

	if err := envelope.Validate(); err != nil {
		return EventEnvelope{}, fmt.Errorf("validate journal entry posted event: %w", err)
	}

	return envelope, nil
}
