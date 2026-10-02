package merchants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateMerchant(ctx context.Context, code, legalName string, initialCap CapabilityType) (*Merchant, error) {
	merchant, err := NewMerchant(code, legalName)
	if err != nil {
		return nil, err
	}

	if err := s.repo.CreateMerchant(ctx, merchant); err != nil {
		return nil, fmt.Errorf("create merchant: %w", err)
	}

	// Create Retail capability
	retailCap, err := NewMerchantCapability(merchant.ID, CapabilityTypeRetail)
	if err != nil {
		return nil, err
	}
	if initialCap == CapabilityTypeRetail {
		retailCap.Activate()
	}
	if err := s.repo.UpsertCapability(ctx, retailCap); err != nil {
		return nil, fmt.Errorf("upsert retail capability: %w", err)
	}

	// Create Supply capability
	supplyCap, err := NewMerchantCapability(merchant.ID, CapabilityTypeSupply)
	if err != nil {
		return nil, err
	}
	if initialCap == CapabilityTypeSupply {
		supplyCap.Activate()
	}
	if err := s.repo.UpsertCapability(ctx, supplyCap); err != nil {
		return nil, fmt.Errorf("upsert supply capability: %w", err)
	}

	return merchant, nil
}

func (s *Service) GetMerchant(ctx context.Context, id uuid.UUID) (*Merchant, error) {
	return s.repo.GetMerchantByID(ctx, id)
}

func (s *Service) ActivateCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	merchant, err := s.repo.GetMerchantByID(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if !merchant.IsActive() {
		return nil, ErrInvalidMerchantStatus
	}

	cap, err := s.repo.GetCapability(ctx, merchantID, capType)
	if err != nil {
		cap, err = NewMerchantCapability(merchantID, capType)
		if err != nil {
			return nil, err
		}
	}

	cap.Activate()
	if err := s.repo.UpsertCapability(ctx, cap); err != nil {
		return nil, fmt.Errorf("activate capability: %w", err)
	}
	return cap, nil
}

func (s *Service) SuspendCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	cap, err := s.repo.GetCapability(ctx, merchantID, capType)
	if err != nil {
		return nil, err
	}

	cap.Suspend()
	if err := s.repo.UpsertCapability(ctx, cap); err != nil {
		return nil, fmt.Errorf("suspend capability: %w", err)
	}
	return cap, nil
}

func (s *Service) AddMember(ctx context.Context, merchantID uuid.UUID, subject string, permissions []string) (*MerchantMembership, error) {
	mem, err := NewMerchantMembership(merchantID, subject)
	if err != nil {
		return nil, err
	}

	if err := s.repo.CreateMembership(ctx, mem); err != nil {
		return nil, fmt.Errorf("create membership: %w", err)
	}

	if len(permissions) > 0 {
		if err := s.repo.GrantPermissions(ctx, mem.ID, permissions); err != nil {
			return nil, fmt.Errorf("grant permissions: %w", err)
		}
		mem.Permissions = permissions
	}

	return mem, nil
}

func (s *Service) ListCapabilities(ctx context.Context, merchantID uuid.UUID) ([]*MerchantCapability, error) {
	return s.repo.ListCapabilities(ctx, merchantID)
}

func (s *Service) ListMemberships(ctx context.Context, merchantID uuid.UUID) ([]*MerchantMembership, error) {
	return s.repo.ListMemberships(ctx, merchantID)
}
