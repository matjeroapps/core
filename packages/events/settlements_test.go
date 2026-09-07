package events

import (
	"testing"
	"time"
)

func TestSettlementEvents(t *testing.T) {
	now := time.Now().UTC()

	t.Run("NewSettlementCalculatedEvent valid", func(t *testing.T) {
		payload := SettlementCalculatedPayload{
			SettlementID:          "set-1",
			PeriodID:              "per-1",
			AccountID:             "acc-1",
			Currency:              "SAR",
			GrossAmountMinor:      1000,
			AdjustmentAmountMinor: 0,
			NetAmountMinor:        1000,
			Status:                "CALCULATED",
			CalculatedAt:          now,
		}
		evt, err := NewSettlementCalculatedEvent(payload, "corr-1", "caus-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if evt.EventType != EventTypeSettlementCalculated {
			t.Errorf("expected event_type %s, got %s", EventTypeSettlementCalculated, evt.EventType)
		}
		if evt.AggregateID != "set-1" {
			t.Errorf("expected aggregate_id set-1, got %s", evt.AggregateID)
		}
	})

	t.Run("NewSettlementCalculatedEvent invalid missing fields", func(t *testing.T) {
		payload := SettlementCalculatedPayload{
			SettlementID: "",
		}
		_, err := NewSettlementCalculatedEvent(payload, "corr-1", "caus-1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("NewSettlementFinalizedEvent valid", func(t *testing.T) {
		payload := SettlementFinalizedPayload{
			SettlementID:   "set-1",
			PeriodID:       "per-1",
			AccountID:      "acc-1",
			Currency:       "SAR",
			NetAmountMinor: 1000,
			Status:         "FINALIZED",
			FinalizedAt:    now,
		}
		evt, err := NewSettlementFinalizedEvent(payload, "corr-1", "caus-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if evt.EventType != EventTypeSettlementFinalized {
			t.Errorf("expected event_type %s, got %s", EventTypeSettlementFinalized, evt.EventType)
		}
	})
}
