package coreapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"core/internal/merchants"
	"core/internal/serviceauth"
)

func TestMerchantRoutesRequireInternalAdminOrPlatform(t *testing.T) {
	router := serviceauth.Middleware(testAuthConfig())(NewRouter(Dependencies{
		Merchants: merchants.NewService(newFakeMerchantRepo()),
	}))

	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/merchants", "seller", testSellerToken)
	req.Body = ioBody(`{"code":"mer_test","legal_name":"Test Merchant","initial_capability":"RETAIL"}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("seller create status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	req = authenticatedRequest(t, http.MethodPost, "/internal/v1/merchants", "admin", testAdminToken)
	req.Body = ioBody(`{"code":"mer_test","legal_name":"Test Merchant","initial_capability":"RETAIL"}`)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMerchantRoutesRejectMalformedMerchantID(t *testing.T) {
	router := serviceauth.Middleware(testAuthConfig())(NewRouter(Dependencies{
		Merchants: merchants.NewService(newFakeMerchantRepo()),
	}))

	req := authenticatedRequest(t, http.MethodGet, "/internal/v1/merchants/not-a-uuid", "admin", testAdminToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed get status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func ioBody(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}

type fakeMerchantRepo struct {
	merchants   map[uuid.UUID]*merchants.Merchant
	caps        map[string]*merchants.MerchantCapability
	memberships map[string]*merchants.MerchantMembership
}

func newFakeMerchantRepo() *fakeMerchantRepo {
	return &fakeMerchantRepo{
		merchants:   map[uuid.UUID]*merchants.Merchant{},
		caps:        map[string]*merchants.MerchantCapability{},
		memberships: map[string]*merchants.MerchantMembership{},
	}
}

func (r *fakeMerchantRepo) CreateMerchant(ctx context.Context, m *merchants.Merchant) error {
	r.merchants[m.ID] = m
	return nil
}

func (r *fakeMerchantRepo) GetMerchantByID(ctx context.Context, id uuid.UUID) (*merchants.Merchant, error) {
	m, ok := r.merchants[id]
	if !ok {
		return nil, merchants.ErrMerchantNotFound
	}
	return m, nil
}

func (r *fakeMerchantRepo) GetMerchantByCode(ctx context.Context, code string) (*merchants.Merchant, error) {
	for _, m := range r.merchants {
		if m.Code == code {
			return m, nil
		}
	}
	return nil, merchants.ErrMerchantNotFound
}

func (r *fakeMerchantRepo) UpdateMerchant(ctx context.Context, m *merchants.Merchant) error {
	r.merchants[m.ID] = m
	return nil
}

func (r *fakeMerchantRepo) UpsertCapability(ctx context.Context, cap *merchants.MerchantCapability) error {
	r.caps[capKey(cap.MerchantID, cap.CapabilityType)] = cap
	return nil
}

func (r *fakeMerchantRepo) GetCapability(ctx context.Context, merchantID uuid.UUID, capType merchants.CapabilityType) (*merchants.MerchantCapability, error) {
	cap, ok := r.caps[capKey(merchantID, capType)]
	if !ok {
		return nil, merchants.ErrCapabilityNotFound
	}
	return cap, nil
}

func (r *fakeMerchantRepo) ListCapabilities(ctx context.Context, merchantID uuid.UUID) ([]*merchants.MerchantCapability, error) {
	var out []*merchants.MerchantCapability
	for _, cap := range r.caps {
		if cap.MerchantID == merchantID {
			out = append(out, cap)
		}
	}
	return out, nil
}

func (r *fakeMerchantRepo) CreateMembership(ctx context.Context, mem *merchants.MerchantMembership) error {
	r.memberships[memKey(mem.MerchantID, mem.PrincipalSubject)] = mem
	return nil
}

func (r *fakeMerchantRepo) GetMembership(ctx context.Context, merchantID uuid.UUID, subject string) (*merchants.MerchantMembership, error) {
	mem, ok := r.memberships[memKey(merchantID, subject)]
	if !ok {
		return nil, merchants.ErrMembershipNotFound
	}
	return mem, nil
}

func (r *fakeMerchantRepo) ListMemberships(ctx context.Context, merchantID uuid.UUID) ([]*merchants.MerchantMembership, error) {
	var out []*merchants.MerchantMembership
	for _, mem := range r.memberships {
		if mem.MerchantID == merchantID {
			out = append(out, mem)
		}
	}
	return out, nil
}

func (r *fakeMerchantRepo) GrantPermissions(ctx context.Context, membershipID uuid.UUID, permissions []string) error {
	for _, mem := range r.memberships {
		if mem.ID == membershipID {
			mem.Permissions = append(mem.Permissions, permissions...)
		}
	}
	return nil
}

func (r *fakeMerchantRepo) GetMembershipPermissions(ctx context.Context, membershipID uuid.UUID) ([]string, error) {
	for _, mem := range r.memberships {
		if mem.ID == membershipID {
			return mem.Permissions, nil
		}
	}
	return nil, merchants.ErrMembershipNotFound
}

func (r *fakeMerchantRepo) LinkSellerProfile(ctx context.Context, sellerID, merchantID uuid.UUID) error {
	return nil
}
func (r *fakeMerchantRepo) LinkSupplierProfile(ctx context.Context, supplierID, merchantID uuid.UUID) error {
	return nil
}
func (r *fakeMerchantRepo) RecordCrosswalk(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, merchantID uuid.UUID) error {
	return nil
}
func (r *fakeMerchantRepo) RecordQuarantine(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, reason string, details map[string]any) error {
	return nil
}

func capKey(merchantID uuid.UUID, capType merchants.CapabilityType) string {
	return merchantID.String() + ":" + string(capType)
}

func memKey(merchantID uuid.UUID, subject string) string {
	return merchantID.String() + ":" + subject
}
