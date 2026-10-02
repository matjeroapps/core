package merchants

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewMerchant(t *testing.T) {
	m, err := NewMerchant("mer_123", "Test Legal Name")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m.Code != "mer_123" {
		t.Errorf("expected code mer_123, got %s", m.Code)
	}
	if !m.IsActive() {
		t.Errorf("expected active status")
	}

	m.Suspend()
	if m.IsActive() {
		t.Errorf("expected suspended status")
	}

	m.Reactivate()
	if !m.IsActive() {
		t.Errorf("expected active status after reactivation")
	}
}

func TestMerchantCapabilities(t *testing.T) {
	mID := uuid.New()
	cap, err := NewMerchantCapability(mID, CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cap.IsActive() {
		t.Errorf("expected initial status to be inactive")
	}

	cap.Activate()
	if !cap.IsActive() {
		t.Errorf("expected active status")
	}
	if cap.ActivatedAt == nil {
		t.Errorf("expected ActivatedAt timestamp to be set")
	}

	cap.Suspend()
	if cap.IsActive() {
		t.Errorf("expected status to be suspended")
	}
	if cap.SuspendedAt == nil {
		t.Errorf("expected SuspendedAt timestamp to be set")
	}
}

func TestMerchantMembership(t *testing.T) {
	mID := uuid.New()
	mem, err := NewMerchantMembership(mID, "sub_12345")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !mem.IsActive() {
		t.Errorf("expected active membership status")
	}
}

func TestMembershipPermissions(t *testing.T) {
	memID := uuid.New()
	perm, err := NewMembershipPermission(memID, PermissionRetailStoresManage)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if perm.PermissionCode != PermissionRetailStoresManage {
		t.Errorf("expected permission code %s, got %s", PermissionRetailStoresManage, perm.PermissionCode)
	}
}
