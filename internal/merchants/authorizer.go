package merchants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

type AuthMode string

const (
	AuthModeLegacy    AuthMode = "legacy"
	AuthModeShadow    AuthMode = "shadow"
	AuthModeCanonical AuthMode = "canonical"
)

type AuthRequest struct {
	MerchantID         uuid.UUID
	Subject            string
	RequiredCapability CapabilityType
	RequiredPermission string
	LegacySellerRole   string
	LegacySupplierRole string
	CorrelationID      string
	RequestedResource  string
}

type AuthDecision struct {
	Allowed bool
	Mode    AuthMode
	Reason  string
}

type LegacyEvaluator interface {
	AuthorizeLegacy(ctx context.Context, req AuthRequest) (AuthDecision, error)
}

type AuthorizerOption func(*Authorizer)

type Authorizer struct {
	repo              Repository
	mode              AuthMode
	config            *ConfigManager
	telemetry         *Telemetry
	legacy            LegacyEvaluator
	mismatchThreshold uint64
}

func NewAuthorizer(repo Repository, mode AuthMode, opts ...AuthorizerOption) *Authorizer {
	if mode == "" {
		mode = AuthModeShadow
	}
	a := &Authorizer{
		repo:              repo,
		mode:              mode,
		telemetry:         NewTelemetry(),
		mismatchThreshold: 1,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func WithConfigManager(config *ConfigManager) AuthorizerOption {
	return func(a *Authorizer) {
		a.config = config
	}
}

func WithTelemetry(telemetry *Telemetry) AuthorizerOption {
	return func(a *Authorizer) {
		a.telemetry = telemetry
	}
}

func WithLegacyEvaluator(legacy LegacyEvaluator) AuthorizerOption {
	return func(a *Authorizer) {
		a.legacy = legacy
	}
}

func WithMismatchThreshold(threshold uint64) AuthorizerOption {
	return func(a *Authorizer) {
		a.mismatchThreshold = threshold
	}
}

func (a *Authorizer) Authorize(ctx context.Context, merchantID uuid.UUID, subject string, requiredCap CapabilityType, requiredPerm string) error {
	return a.AuthorizeRequest(ctx, AuthRequest{
		MerchantID:         merchantID,
		Subject:            subject,
		RequiredCapability: requiredCap,
		RequiredPermission: requiredPerm,
	})
}

func (a *Authorizer) AuthorizeRequest(ctx context.Context, req AuthRequest) error {
	mode := a.currentMode()
	switch mode {
	case AuthModeLegacy:
		decision, err := a.legacyDecision(ctx, req)
		a.telemetry.RecordAuthDecision(mode, decision.Allowed, reasonFromError(err, decision.Reason))
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return ErrPermissionDenied
		}
		return nil
	case AuthModeCanonical:
		err := a.authorizeCanonical(ctx, req)
		a.telemetry.RecordAuthDecision(mode, err == nil, reasonFromError(err, "allowed"))
		return err
	default:
		legacy, legacyErr := a.legacyDecision(ctx, req)
		canonicalErr := a.authorizeCanonical(ctx, req)
		canonicalAllowed := canonicalErr == nil
		legacyAllowed := legacyErr == nil && legacy.Allowed
		matched := legacyAllowed == canonicalAllowed
		a.telemetry.RecordShadowComparison(matched, fmt.Sprintf("merchant_id=%s resource=%s legacy_allowed=%t canonical_allowed=%t legacy_reason=%s canonical_reason=%s",
			req.MerchantID, req.RequestedResource, legacyAllowed, canonicalAllowed, reasonFromError(legacyErr, legacy.Reason), reasonFromError(canonicalErr, "allowed")))
		if !matched && a.telemetry.GetMetrics().AuthShadowMismatchesTotal >= a.mismatchThreshold {
			a.telemetry.RecordFallbackActivation("shadow_mismatch_threshold")
			if a.config != nil {
				a.config.SetAuthMode(AuthModeLegacy)
			}
		}
		a.telemetry.RecordAuthDecision(mode, legacyAllowed, reasonFromError(legacyErr, legacy.Reason))
		if legacyErr != nil {
			return legacyErr
		}
		if !legacy.Allowed {
			return ErrPermissionDenied
		}
		return nil
	}
}

func (a *Authorizer) currentMode() AuthMode {
	if a.config != nil {
		return a.config.GetAuthMode()
	}
	return a.mode
}

func (a *Authorizer) legacyDecision(ctx context.Context, req AuthRequest) (AuthDecision, error) {
	if a.legacy != nil {
		return a.legacy.AuthorizeLegacy(ctx, req)
	}
	err := a.authorizeCanonical(ctx, req)
	return AuthDecision{Allowed: err == nil, Mode: AuthModeLegacy, Reason: reasonFromError(err, "allowed")}, err
}

func (a *Authorizer) authorizeCanonical(ctx context.Context, req AuthRequest) error {
	merchant, err := a.repo.GetMerchantByID(ctx, req.MerchantID)
	if err != nil {
		return fmtErr(ErrMerchantNotFound, err)
	}
	if !merchant.IsActive() {
		return ErrInvalidMerchantStatus
	}

	if req.RequiredCapability != "" {
		cap, err := a.repo.GetCapability(ctx, req.MerchantID, req.RequiredCapability)
		if err != nil || !cap.IsActive() {
			return ErrCapabilitySuspended
		}
	}

	mem, err := a.repo.GetMembership(ctx, req.MerchantID, req.Subject)
	if err != nil || !mem.IsActive() {
		return ErrMembershipNotFound
	}

	if req.RequiredPermission != "" {
		hasPerm := false
		for _, p := range mem.Permissions {
			if p == req.RequiredPermission || p == PermissionMerchantManage {
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

func reasonFromError(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func fmtErr(base, cause error) error {
	if errors.Is(cause, ErrMerchantNotFound) {
		return ErrMerchantNotFound
	}
	return base
}
