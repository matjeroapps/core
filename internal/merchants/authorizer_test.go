package merchants

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestShadowModeReturnsLegacyDecisionAndActivatesFallback(t *testing.T) {
	repo := newMemoryRepo()
	merchant, _ := NewMerchant("mer_test", "Test Merchant")
	repo.merchants[merchant.ID] = merchant

	cfg := NewConfigManager()
	cfg.SetAuthMode(AuthModeShadow)
	telemetry := NewTelemetry()
	auth := NewAuthorizer(repo, AuthModeShadow,
		WithConfigManager(cfg),
		WithTelemetry(telemetry),
		WithLegacyEvaluator(allowLegacy{}),
		WithMismatchThreshold(1),
	)

	err := auth.AuthorizeRequest(context.Background(), AuthRequest{
		MerchantID:         merchant.ID,
		Subject:            "subject-without-membership",
		RequiredCapability: CapabilityTypeRetail,
		RequiredPermission: PermissionRetailStoresManage,
		RequestedResource:  "store:test",
	})
	if err != nil {
		t.Fatalf("shadow mode returned canonical error instead of legacy decision: %v", err)
	}
	if cfg.GetAuthMode() != AuthModeLegacy {
		t.Fatalf("auth mode = %s, want legacy fallback", cfg.GetAuthMode())
	}
	if telemetry.GetMetrics().AuthShadowMismatchesTotal != 1 {
		t.Fatalf("shadow mismatches = %d, want 1", telemetry.GetMetrics().AuthShadowMismatchesTotal)
	}
}

func TestCanonicalModeRequiresCapabilityMembershipAndPermission(t *testing.T) {
	repo := newMemoryRepo()
	merchant, _ := NewMerchant("mer_test", "Test Merchant")
	repo.merchants[merchant.ID] = merchant
	capability, _ := NewMerchantCapability(merchant.ID, CapabilityTypeRetail)
	capability.Activate()
	repo.capabilities[capKey(merchant.ID, CapabilityTypeRetail)] = capability
	member, _ := NewMerchantMembership(merchant.ID, "subject")
	member.Permissions = []string{PermissionRetailStoresManage}
	repo.memberships[memKey(merchant.ID, "subject")] = member

	auth := NewAuthorizer(repo, AuthModeCanonical)
	if err := auth.Authorize(context.Background(), merchant.ID, "subject", CapabilityTypeRetail, PermissionRetailStoresManage); err != nil {
		t.Fatalf("canonical authorize failed: %v", err)
	}
	if err := auth.Authorize(context.Background(), merchant.ID, "subject", CapabilityTypeSupply, PermissionSupplyInventoryManage); !errors.Is(err, ErrCapabilitySuspended) {
		t.Fatalf("canonical missing capability err = %v, want ErrCapabilitySuspended", err)
	}
	if err := auth.Authorize(context.Background(), merchant.ID, "subject", CapabilityTypeRetail, PermissionFinanceManage); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("canonical permission err = %v, want ErrPermissionDenied", err)
	}
}

type allowLegacy struct{}

func (allowLegacy) AuthorizeLegacy(ctx context.Context, req AuthRequest) (AuthDecision, error) {
	return AuthDecision{Allowed: true, Mode: AuthModeLegacy, Reason: "legacy_allowed"}, nil
}

type memoryRepo struct {
	merchants    map[uuid.UUID]*Merchant
	capabilities map[string]*MerchantCapability
	memberships  map[string]*MerchantMembership
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		merchants:    map[uuid.UUID]*Merchant{},
		capabilities: map[string]*MerchantCapability{},
		memberships:  map[string]*MerchantMembership{},
	}
}

func (r *memoryRepo) CreateMerchant(ctx context.Context, m *Merchant) error {
	r.merchants[m.ID] = m
	return nil
}
func (r *memoryRepo) GetMerchantByID(ctx context.Context, id uuid.UUID) (*Merchant, error) {
	m, ok := r.merchants[id]
	if !ok {
		return nil, ErrMerchantNotFound
	}
	return m, nil
}
func (r *memoryRepo) GetMerchantByCode(ctx context.Context, code string) (*Merchant, error) {
	return nil, ErrMerchantNotFound
}
func (r *memoryRepo) UpdateMerchant(ctx context.Context, m *Merchant) error {
	r.merchants[m.ID] = m
	return nil
}
func (r *memoryRepo) UpsertCapability(ctx context.Context, cap *MerchantCapability) error {
	r.capabilities[capKey(cap.MerchantID, cap.CapabilityType)] = cap
	return nil
}
func (r *memoryRepo) GetCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	cap, ok := r.capabilities[capKey(merchantID, capType)]
	if !ok {
		return nil, ErrCapabilityNotFound
	}
	return cap, nil
}
func (r *memoryRepo) ListCapabilities(ctx context.Context, merchantID uuid.UUID) ([]*MerchantCapability, error) {
	return nil, nil
}
func (r *memoryRepo) CreateMembership(ctx context.Context, mem *MerchantMembership) error {
	r.memberships[memKey(mem.MerchantID, mem.PrincipalSubject)] = mem
	return nil
}
func (r *memoryRepo) GetMembership(ctx context.Context, merchantID uuid.UUID, subject string) (*MerchantMembership, error) {
	mem, ok := r.memberships[memKey(merchantID, subject)]
	if !ok {
		return nil, ErrMembershipNotFound
	}
	return mem, nil
}
func (r *memoryRepo) ListMemberships(ctx context.Context, merchantID uuid.UUID) ([]*MerchantMembership, error) {
	return nil, nil
}
func (r *memoryRepo) ListMembershipsBySubject(ctx context.Context, subject string) ([]*MerchantMembership, error) {
	return nil, nil
}
func (r *memoryRepo) GrantPermissions(ctx context.Context, membershipID uuid.UUID, permissions []string) error {
	return nil
}
func (r *memoryRepo) GetMembershipPermissions(ctx context.Context, membershipID uuid.UUID) ([]string, error) {
	return nil, nil
}
func (r *memoryRepo) LinkSellerProfile(ctx context.Context, sellerID, merchantID uuid.UUID) error {
	return nil
}
func (r *memoryRepo) LinkSupplierProfile(ctx context.Context, supplierID, merchantID uuid.UUID) error {
	return nil
}
func (r *memoryRepo) RecordCrosswalk(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, merchantID uuid.UUID) error {
	return nil
}
func (r *memoryRepo) RecordQuarantine(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, reason string, details map[string]any) error {
	return nil
}

func capKey(merchantID uuid.UUID, capType CapabilityType) string {
	return merchantID.String() + ":" + string(capType)
}

func memKey(merchantID uuid.UUID, subject string) string {
	return merchantID.String() + ":" + subject
}
