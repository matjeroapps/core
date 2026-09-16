package commerce_test

import (
	"testing"

	"github.com/matjeroapps/core/modules/commerce"
)

func TestStoreEntitlementPolicy(t *testing.T) {
	t.Run("default limit is 1", func(t *testing.T) {
		p := commerce.NewStoreEntitlementPolicy(1)
		if p.EffectiveLimit() != 1 {
			t.Errorf("expected 1, got %d", p.EffectiveLimit())
		}
	})

	t.Run("zero or negative defaults to 1", func(t *testing.T) {
		p := commerce.NewStoreEntitlementPolicy(0)
		if p.EffectiveLimit() != 1 {
			t.Errorf("expected 1 for 0 input, got %d", p.EffectiveLimit())
		}

		pNeg := commerce.NewStoreEntitlementPolicy(-5)
		if pNeg.EffectiveLimit() != 1 {
			t.Errorf("expected 1 for negative input, got %d", pNeg.EffectiveLimit())
		}
	})

	t.Run("custom higher limit", func(t *testing.T) {
		p := commerce.NewStoreEntitlementPolicy(5)
		if p.EffectiveLimit() != 5 {
			t.Errorf("expected 5, got %d", p.EffectiveLimit())
		}
	})
}

func TestRoleNormalizationAndPredicates(t *testing.T) {
	tests := []struct {
		role      string
		isOwner   bool
		isManager bool
		isStaff   bool
	}{
		{"owner", true, true, true},
		{"seller_owner", true, true, true},
		{"OWNER", true, true, true},
		{"manager", false, true, true},
		{"seller_manager", false, true, true},
		{"staff", false, false, true},
		{"seller_staff", false, false, true},
		{"unknown", false, false, false},
		{"", false, false, false},
	}

	for _, tt := range tests {
		t.Run("role_"+tt.role, func(t *testing.T) {
			if got := commerce.IsOwnerRole(tt.role); got != tt.isOwner {
				t.Errorf("IsOwnerRole(%q) = %v, want %v", tt.role, got, tt.isOwner)
			}
			if got := commerce.IsManagerRole(tt.role); got != tt.isManager {
				t.Errorf("IsManagerRole(%q) = %v, want %v", tt.role, got, tt.isManager)
			}
			if got := commerce.IsStaffRole(tt.role); got != tt.isStaff {
				t.Errorf("IsStaffRole(%q) = %v, want %v", tt.role, got, tt.isStaff)
			}
		})
	}
}
