package coreapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"core/internal/merchants"
	"core/packages/httpx"
)

type MerchantValidationProvisioner interface {
	ProvisionValidationScenario(ctx context.Context, req merchants.ValidationProvisionRequest) (*merchants.ValidationScenarioManifest, error)
}

type MerchantValidationProvisioningHandler struct {
	provisioner MerchantValidationProvisioner
}

func NewMerchantValidationProvisioningHandler(provisioner MerchantValidationProvisioner) *MerchantValidationProvisioningHandler {
	return &MerchantValidationProvisioningHandler{provisioner: provisioner}
}

func (h *MerchantValidationProvisioningHandler) Provision(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.provisioner == nil {
		writeError(w, CodeNotFound)
		return
	}
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	var req merchants.ValidationProvisionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	req.IdempotencyKey = idempotencyKey

	manifest, err := h.provisioner.ProvisionValidationScenario(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, merchants.ErrValidationProvisioningDisabled):
			writeError(w, CodeNotFound)
		case errors.Is(err, merchants.ErrValidationProvisioningUnsafe):
			writeError(w, CodeForbidden)
		case errors.Is(err, merchants.ErrValidationProvisioningInvalid):
			writeError(w, CodeValidationError)
		default:
			slog.Error("local validation scenario provisioning failed", "error", err)
			writeError(w, CodeInternalError)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, manifest)
}
