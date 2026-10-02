package merchants

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type AuthMode string

const (
	AuthModeLegacy    AuthMode = "legacy"
	AuthModeShadow    AuthMode = "shadow"
	AuthModeCanonical AuthMode = "canonical"
)

type Authorizer struct {
	repo Repository
	mode AuthMode
}

func NewAuthorizer(repo Repository, mode AuthMode) *Authorizer {
	if mode == "" {
		mode = AuthModeShadow
	}
	return &Authorizer{
		repo: repo,
		mode: mode,
	}
}

func (a *Authorizer) Authorize(ctx context.Context, merchantID uuid.UUID, subject string, requiredCap CapabilityType, requiredPerm string) error {
	// 1. Check Merchant active status
	merchant, err := a.repo.GetMerchantByID(ctx, merchantID)
	if err != nil {
		return fmtErr(ErrMerchantNotFound, err)
	}
	if !merchant.IsActive() {
		return ErrInvalidMerchantStatus
	}

	// 2. Check Required Capability active status
	if requiredCap != "" {
		cap, err := a.repo.GetCapability(ctx, merchantID, requiredCap)
		if err != nil || !cap.IsActive() {
			return ErrCapabilitySuspended
		}
	}

	// 3. Check Membership active status
	mem, err := a.repo.GetMembership(ctx, merchantID, subject)
	if err != nil || !mem.IsActive() {
		return ErrMembershipNotFound
	}

	// 4. Check Granular Permission
	if requiredPerm != "" {
		hasPerm := false
		for _, p := range mem.Permissions {
			if p == requiredPerm || p == PermissionMerchantManage {
				hasPerm = true
				break
			}
		}
		if !hasPerm {
			return ErrPermissionDenied
		}
	}

	return nil
}

func fmtErr(base, cause error) error {
	if errors.Is(cause, ErrMerchantNotFound) {
		return ErrMerchantNotFound
	}
	return base
}
