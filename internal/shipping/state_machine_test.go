package shipping

import (
	"testing"
)

func TestStatusValidation(t *testing.T) {
	validStatuses := []Status{
		StatusPending,
		StatusProcessing,
		StatusReadyForPickup,
		StatusShipped,
		StatusOutForDelivery,
		StatusDelivered,
		StatusFailed,
		StatusReturned,
	}

	for _, s := range validStatuses {
		if !s.Valid() {
			t.Errorf("expected status %s to be valid", s)
		}
	}

	invalidStatuses := []Status{
		Status("INVALID"),
		Status("pending"),
		Status("SHIPPED_OUT"),
		Status(""),
	}

	for _, s := range invalidStatuses {
		if s.Valid() {
			t.Errorf("expected status %s to be invalid", s)
		}
	}
}

func TestStateMachineTransitions(t *testing.T) {
	tests := []struct {
		from    Status
		to      Status
		allowed bool
	}{
		// Valid transitions
		{StatusPending, StatusPending, true},
		{StatusPending, StatusProcessing, true},
		{StatusPending, StatusFailed, true},

		{StatusProcessing, StatusProcessing, true},
		{StatusProcessing, StatusReadyForPickup, true},
		{StatusProcessing, StatusShipped, true},
		{StatusProcessing, StatusFailed, true},

		{StatusReadyForPickup, StatusReadyForPickup, true},
		{StatusReadyForPickup, StatusShipped, true},
		{StatusReadyForPickup, StatusFailed, true},

		{StatusShipped, StatusShipped, true},
		{StatusShipped, StatusOutForDelivery, true},
		{StatusShipped, StatusDelivered, true},
		{StatusShipped, StatusFailed, true},
		{StatusShipped, StatusReturned, true},

		{StatusOutForDelivery, StatusOutForDelivery, true},
		{StatusOutForDelivery, StatusDelivered, true},
		{StatusOutForDelivery, StatusFailed, true},
		{StatusOutForDelivery, StatusReturned, true},

		{StatusDelivered, StatusDelivered, true},
		{StatusDelivered, StatusReturned, true},

		{StatusFailed, StatusFailed, true},
		{StatusFailed, StatusProcessing, true},
		{StatusFailed, StatusReturned, true},

		// Invalid transitions
		{StatusPending, StatusShipped, false},
		{StatusPending, StatusOutForDelivery, false},
		{StatusPending, StatusDelivered, false},
		{StatusPending, StatusReturned, false},

		{StatusProcessing, StatusDelivered, false},
		{StatusProcessing, StatusReturned, false},

		{StatusReadyForPickup, StatusPending, false},
		{StatusReadyForPickup, StatusDelivered, false},

		{StatusShipped, StatusPending, false},
		{StatusShipped, StatusProcessing, false},

		{StatusOutForDelivery, StatusPending, false},
		{StatusOutForDelivery, StatusProcessing, false},

		{StatusDelivered, StatusPending, false},
		{StatusDelivered, StatusProcessing, false},
		{StatusDelivered, StatusShipped, false},

		{StatusReturned, StatusPending, false},
		{StatusReturned, StatusProcessing, false},
		{StatusReturned, StatusShipped, false},
		{StatusReturned, StatusDelivered, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"_to_"+string(tt.to), func(t *testing.T) {
			got := CanTransition(tt.from, tt.to)
			if got != tt.allowed {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.allowed)
			}

			err := ValidateTransition(tt.from, tt.to)
			if tt.allowed && err != nil {
				t.Errorf("ValidateTransition(%s, %s) error = %v, want nil", tt.from, tt.to, err)
			}
			if !tt.allowed && err == nil {
				t.Errorf("ValidateTransition(%s, %s) error = nil, want error", tt.from, tt.to)
			}
		})
	}
}
