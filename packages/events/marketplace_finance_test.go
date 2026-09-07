package events

import (
	"testing"
	"time"
)

func TestNewAllocationCalculatedEvent(t *testing.T) {
	now := time.Now().UTC()
	payload := AllocationCalculatedPayload{
		SettlementID:   "set_123",
		AllocationID:   "alloc_456",
		AccountID:      "acc_789",
		AllocationType: "PLATFORM_SHARE",
		AmountMinor:    1000,
		Currency:       "USD",
		CalculatedAt:   now,
	}

	evt, err := NewAllocationCalculatedEvent(payload, "corr_1", "caus_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if evt.EventType != EventTypeAllocationCalculated {
		t.Errorf("expected event type %s, got %s", EventTypeAllocationCalculated, evt.EventType)
	}
	if evt.AggregateID != "alloc_456" {
		t.Errorf("expected aggregate ID alloc_456, got %s", evt.AggregateID)
	}
	if evt.CorrelationID != "corr_1" {
		t.Errorf("expected correlation ID corr_1, got %s", evt.CorrelationID)
	}
}

func TestNewAllocationCalculatedEvent_Invalid(t *testing.T) {
	payload := AllocationCalculatedPayload{
		SettlementID: "",
		AllocationID: "alloc_456",
	}
	_, err := NewAllocationCalculatedEvent(payload, "corr_1", "caus_1")
	if err == nil {
		t.Fatal("expected error for invalid payload, got nil")
	}
}
