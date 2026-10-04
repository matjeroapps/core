package coreapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"core/internal/merchants"
)

type stubValidationProvisioner struct {
	got merchants.ValidationProvisionRequest
	err error
}

func (s *stubValidationProvisioner) ProvisionValidationScenario(ctx context.Context, req merchants.ValidationProvisionRequest) (*merchants.ValidationScenarioManifest, error) {
	s.got = req
	if s.err != nil {
		return nil, s.err
	}
	return &merchants.ValidationScenarioManifest{
		Version: "1",
		Actors: map[string]merchants.ValidationActorRow{
			"dual_owner": {Subject: "feature-026-dual-owner"},
		},
		Merchants: map[string]merchants.ValidationMerchant{},
		Stores:    map[string]merchants.ValidationStore{},
		Supply:    map[string]string{},
		Denials:   map[string]string{},
	}, nil
}

func TestMerchantValidationProvisioningRequiresPlatformCaller(t *testing.T) {
	handler := newTestRouter(Dependencies{MerchantValidation: &stubValidationProvisioner{}})
	body := `{"local_only":true,"issuer":"http://localhost:8081","actors":{"dual_owner":"a","retail_owner":"b","supply_owner":"c","retail_limited_staff":"d","supply_limited_staff":"e","suspended_membership":"f","multi_merchant_actor":"g","no_membership_actor":"h"}}`

	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/local-validation/merchant-console/scenarios", "seller", testSellerToken)
	req.Header.Set("Idempotency-Key", "test-key")
	req.Body = io.NopCloser(strings.NewReader(body))

	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestMerchantValidationProvisioningRequiresIdempotencyKey(t *testing.T) {
	handler := newTestRouter(Dependencies{MerchantValidation: &stubValidationProvisioner{}})
	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/local-validation/merchant-console/scenarios", "platform", testPlatformToken)
	req.Body = io.NopCloser(strings.NewReader(`{"local_only":true,"issuer":"http://localhost:8081","actors":{}}`))

	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestMerchantValidationProvisioningPassesSanitizedRequest(t *testing.T) {
	provisioner := &stubValidationProvisioner{}
	handler := newTestRouter(Dependencies{MerchantValidation: provisioner})
	body := `{"local_only":true,"issuer":"http://localhost:8081","actors":{"dual_owner":"a","retail_owner":"b","supply_owner":"c","retail_limited_staff":"d","supply_limited_staff":"e","suspended_membership":"f","multi_merchant_actor":"g","no_membership_actor":"h"}}`

	req := authenticatedRequest(t, http.MethodPost, "/internal/v1/local-validation/merchant-console/scenarios", "platform", testPlatformToken)
	req.Header.Set("Idempotency-Key", "feature-026-test")
	req.Body = io.NopCloser(strings.NewReader(body))

	rec := doRequest(t, handler, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if provisioner.got.IdempotencyKey != "feature-026-test" || !provisioner.got.LocalOnly {
		t.Fatalf("provisioner request = %+v", provisioner.got)
	}
	var payload merchants.ValidationScenarioManifest
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Actors["dual_owner"].Subject != "feature-026-dual-owner" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
