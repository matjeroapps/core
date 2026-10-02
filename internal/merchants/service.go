package merchants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	repo   Repository
	pgRepo *PostgresRepository
	outbox *OutboxPublisher
}

func NewService(repo Repository) *Service {
	s := &Service{repo: repo}
	if pgRepo, ok := repo.(*PostgresRepository); ok {
		s.pgRepo = pgRepo
		s.outbox = NewOutboxPublisher()
	}
	return s
}

func (s *Service) CreateMerchant(ctx context.Context, code, legalName string, initialCap CapabilityType) (*Merchant, error) {
	return s.CreateMerchantWithMetadata(ctx, code, legalName, initialCap, MutationMetadata{})
}

func (s *Service) CreateMerchantWithMetadata(ctx context.Context, code, legalName string, initialCap CapabilityType, meta MutationMetadata) (*Merchant, error) {
	merchant, err := NewMerchant(code, legalName)
	if err != nil {
		return nil, err
	}

	retailCap, supplyCap, err := buildInitialCapabilities(merchant.ID, initialCap)
	if err != nil {
		return nil, err
	}

	if s.pgRepo != nil {
		if err := s.pgRepo.withTx(ctx, func(tx pgx.Tx) error {
			if err := s.pgRepo.createMerchantTx(ctx, tx, merchant); err != nil {
				return fmt.Errorf("create merchant: %w", err)
			}
			if err := s.pgRepo.upsertCapabilityTx(ctx, tx, retailCap); err != nil {
				return fmt.Errorf("upsert retail capability: %w", err)
			}
			if err := s.pgRepo.upsertCapabilityTx(ctx, tx, supplyCap); err != nil {
				return fmt.Errorf("upsert supply capability: %w", err)
			}
			if err := s.outbox.PublishMerchantCreated(ctx, tx, merchant, correlationID(meta), causationID(meta)); err != nil {
				return err
			}
			switch initialCap {
			case CapabilityTypeRetail:
				return s.outbox.PublishCapabilityActivated(ctx, tx, retailCap, correlationID(meta), causationID(meta))
			case CapabilityTypeSupply:
				return s.outbox.PublishCapabilityActivated(ctx, tx, supplyCap, correlationID(meta), causationID(meta))
			default:
				return nil
			}
		}); err != nil {
			return nil, err
		}
		return merchant, nil
	}

	if err := s.repo.CreateMerchant(ctx, merchant); err != nil {
		return nil, fmt.Errorf("create merchant: %w", err)
	}
	if err := s.repo.UpsertCapability(ctx, retailCap); err != nil {
		return nil, fmt.Errorf("upsert retail capability: %w", err)
	}
	if err := s.repo.UpsertCapability(ctx, supplyCap); err != nil {
		return nil, fmt.Errorf("upsert supply capability: %w", err)
	}

	return merchant, nil
}

func buildInitialCapabilities(merchantID uuid.UUID, initialCap CapabilityType) (*MerchantCapability, *MerchantCapability, error) {
	retailCap, err := NewMerchantCapability(merchantID, CapabilityTypeRetail)
	if err != nil {
		return nil, nil, err
	}
	if initialCap == CapabilityTypeRetail {
		retailCap.Activate()
	}

	supplyCap, err := NewMerchantCapability(merchantID, CapabilityTypeSupply)
	if err != nil {
		return nil, nil, err
	}
	if initialCap == CapabilityTypeSupply {
		supplyCap.Activate()
	}

	return retailCap, supplyCap, nil
}

func (s *Service) GetMerchant(ctx context.Context, id uuid.UUID) (*Merchant, error) {
	return s.repo.GetMerchantByID(ctx, id)
}

func (s *Service) ActivateCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	return s.ActivateCapabilityWithMetadata(ctx, merchantID, capType, MutationMetadata{})
}

func (s *Service) ActivateCapabilityWithMetadata(ctx context.Context, merchantID uuid.UUID, capType CapabilityType, meta MutationMetadata) (*MerchantCapability, error) {
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
	if s.pgRepo != nil {
		if err := s.pgRepo.withTx(ctx, func(tx pgx.Tx) error {
			if err := s.pgRepo.upsertCapabilityTx(ctx, tx, cap); err != nil {
				return fmt.Errorf("activate capability: %w", err)
			}
			return s.outbox.PublishCapabilityActivated(ctx, tx, cap, correlationID(meta), causationID(meta))
		}); err != nil {
			return nil, err
		}
		return cap, nil
	}

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
	return s.AddMemberWithMetadata(ctx, merchantID, subject, permissions, MutationMetadata{})
}

func (s *Service) AddMemberWithMetadata(ctx context.Context, merchantID uuid.UUID, subject string, permissions []string, meta MutationMetadata) (*MerchantMembership, error) {
	mem, err := NewMerchantMembership(merchantID, subject)
	if err != nil {
		return nil, err
	}

	if s.pgRepo != nil {
		if err := s.pgRepo.withTx(ctx, func(tx pgx.Tx) error {
			if err := s.pgRepo.createMembershipTx(ctx, tx, mem); err != nil {
				return fmt.Errorf("create membership: %w", err)
			}
			if len(permissions) > 0 {
				if err := s.pgRepo.grantPermissionsTx(ctx, tx, mem.ID, permissions); err != nil {
					return fmt.Errorf("grant permissions: %w", err)
				}
				mem.Permissions = permissions
			}
			return s.outbox.PublishMembershipUpdated(ctx, tx, mem, correlationID(meta), causationID(meta))
		}); err != nil {
			return nil, err
		}
		return mem, nil
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

func correlationID(meta MutationMetadata) string {
	if meta.CorrelationID != "" {
		return meta.CorrelationID
	}
	return uuid.NewString()
}

func causationID(meta MutationMetadata) string {
	if meta.CausationID != "" {
		return meta.CausationID
	}
	return meta.IdempotencyKey
}
