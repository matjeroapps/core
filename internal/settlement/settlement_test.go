package settlement

import (
	"testing"
	"time"
)

func TestSettlementStatusTransitions(t *testing.T) {
	tests := []struct {
		current  SettlementStatus
		target   SettlementStatus
		expected bool
	}{
		{StatusPending, StatusCalculated, true},
		{StatusPending, StatusFinalized, true},
		{StatusCalculated, StatusFinalized, true},
		{StatusCalculated, StatusPending, false},
		{StatusFinalized, StatusCalculated, false},
		{StatusFinalized, StatusPending, false},
	}

	for _, tt := range tests {
		got := tt.current.CanTransitionTo(tt.target)
		if got != tt.expected {
			t.Errorf("%s -> %s: expected %v, got %v", tt.current, tt.target, tt.expected, got)
		}
	}
}

func TestPeriodStatusTransitions(t *testing.T) {
	tests := []struct {
		current  PeriodStatus
		target   PeriodStatus
		expected bool
	}{
		{PeriodStatusOpen, PeriodStatusCalculated, true},
		{PeriodStatusOpen, PeriodStatusClosed, true},
		{PeriodStatusCalculated, PeriodStatusFinalized, true},
		{PeriodStatusCalculated, PeriodStatusClosed, true},
		{PeriodStatusFinalized, PeriodStatusCalculated, false},
		{PeriodStatusClosed, PeriodStatusOpen, false},
	}

	for _, tt := range tests {
		got := tt.current.CanTransitionTo(tt.target)
		if got != tt.expected {
			t.Errorf("%s -> %s: expected %v, got %v", tt.current, tt.target, tt.expected, got)
		}
	}
}

func TestSettlementValidation(t *testing.T) {
	now := time.Now().UTC()

	t.Run("Valid settlement", func(t *testing.T) {
		s := Settlement{
			ID:                    "set-1",
			SettlementPeriodID:    "per-1",
			AccountID:             "acc-1",
			Currency:              "SAR",
			GrossAmountMinor:      5000,
			AdjustmentAmountMinor: 0,
			NetAmountMinor:        5000,
			Status:                StatusCalculated,
			CreatedAt:             now,
		}
		if err := s.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Invalid net amount equation", func(t *testing.T) {
		s := Settlement{
			ID:                    "set-1",
			SettlementPeriodID:    "per-1",
			AccountID:             "acc-1",
			Currency:              "SAR",
			GrossAmountMinor:      5000,
			AdjustmentAmountMinor: 100,
			NetAmountMinor:        5000, // Should be 5100
			Status:                StatusCalculated,
			CreatedAt:             now,
		}
		if err := s.Validate(); err == nil {
			t.Error("expected error for net amount mismatch, got nil")
		}
	})

	t.Run("Missing required fields", func(t *testing.T) {
		s := Settlement{
			ID:                 "",
			SettlementPeriodID: "per-1",
			AccountID:          "acc-1",
			Currency:           "SAR",
			Status:             StatusCalculated,
		}
		if err := s.Validate(); err == nil {
			t.Error("expected error for missing ID, got nil")
		}
	})
}
