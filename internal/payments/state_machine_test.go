package payments_test

import (
	"errors"
	"testing"

	"github.com/matjeroapps/core/internal/payments"
)

func TestStateMachine_ValidStatuses(t *testing.T) {
	validStatuses := []payments.Status{
		payments.StatusCreated,
		payments.StatusPending,
		payments.StatusAuthorized,
		payments.StatusCaptured,
		payments.StatusFailed,
		payments.StatusCancelled,
		payments.StatusRefunded,
	}

	for _, st := range validStatuses {
		if !st.Valid() {
			t.Errorf("expected status %s to be valid", st)
		}
	}

	invalidStatuses := []payments.Status{
		"UNKNOWN",
		"pending",
		"captured",
		"",
	}

	for _, st := range invalidStatuses {
		if st.Valid() {
			t.Errorf("expected status %s to be invalid", st)
		}
	}
}

func TestStateMachine_CanTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     payments.Status
		to       payments.Status
		expected bool
	}{
		// Valid transitions from CREATED
		{"CREATED to PENDING", payments.StatusCreated, payments.StatusPending, true},
		{"CREATED to AUTHORIZED", payments.StatusCreated, payments.StatusAuthorized, true},
		{"CREATED to CAPTURED", payments.StatusCreated, payments.StatusCaptured, true},
		{"CREATED to FAILED", payments.StatusCreated, payments.StatusFailed, true},
		{"CREATED to CANCELLED", payments.StatusCreated, payments.StatusCancelled, true},

		// Valid transitions from PENDING
		{"PENDING to AUTHORIZED", payments.StatusPending, payments.StatusAuthorized, true},
		{"PENDING to CAPTURED", payments.StatusPending, payments.StatusCaptured, true},
		{"PENDING to FAILED", payments.StatusPending, payments.StatusFailed, true},
		{"PENDING to CANCELLED", payments.StatusPending, payments.StatusCancelled, true},

		// Valid transitions from AUTHORIZED
		{"AUTHORIZED to CAPTURED", payments.StatusAuthorized, payments.StatusCaptured, true},
		{"AUTHORIZED to FAILED", payments.StatusAuthorized, payments.StatusFailed, true},
		{"AUTHORIZED to CANCELLED", payments.StatusAuthorized, payments.StatusCancelled, true},

		// Valid transitions from CAPTURED
		{"CAPTURED to REFUNDED", payments.StatusCaptured, payments.StatusRefunded, true},

		// Self transitions
		{"CAPTURED to CAPTURED", payments.StatusCaptured, payments.StatusCaptured, true},
		{"PENDING to PENDING", payments.StatusPending, payments.StatusPending, true},

		// Invalid transitions from terminal states
		{"FAILED to CAPTURED (Blocked)", payments.StatusFailed, payments.StatusCaptured, false},
		{"FAILED to AUTHORIZED (Blocked)", payments.StatusFailed, payments.StatusAuthorized, false},
		{"FAILED to PENDING (Blocked)", payments.StatusFailed, payments.StatusPending, false},
		{"CANCELLED to CAPTURED (Blocked)", payments.StatusCancelled, payments.StatusCaptured, false},
		{"REFUNDED to CAPTURED (Blocked)", payments.StatusRefunded, payments.StatusCaptured, false},

		// Invalid backward transitions
		{"CAPTURED to AUTHORIZED (Blocked)", payments.StatusCaptured, payments.StatusAuthorized, false},
		{"CAPTURED to PENDING (Blocked)", payments.StatusCaptured, payments.StatusPending, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := payments.CanTransition(tt.from, tt.to)
			if got != tt.expected {
				t.Errorf("CanTransition(%s, %s) = %v; want %v", tt.from, tt.to, got, tt.expected)
			}

			err := payments.ValidateTransition(tt.from, tt.to)
			if tt.expected && err != nil {
				t.Errorf("ValidateTransition(%s, %s) returned error: %v", tt.from, tt.to, err)
			}
			if !tt.expected && err == nil {
				t.Errorf("ValidateTransition(%s, %s) expected error, got nil", tt.from, tt.to)
			}
			if !tt.expected && err != nil && !errors.Is(err, payments.ErrInvalidTransition) {
				t.Errorf("ValidateTransition(%s, %s) error %v does not wrap ErrInvalidTransition", tt.from, tt.to, err)
			}
		})
	}
}
