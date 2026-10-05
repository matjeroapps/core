package merchants

import (
	"context"
	"errors"
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

// EnsureRetailWorkspace returns the caller's existing active Merchant
// workspace, or creates one with an active Retail capability and owner
// permissions when this is the first Seller onboarding for the subject.
//
// This is intentionally idempotent per subject. Seller registration creates an
// identity before Core has a tenant row, so the first authenticated onboarding
// request is the authoritative place to bridge that identity into the
// canonical Merchant model.
func (s *Service) EnsureRetailWorkspace(ctx context.Context, subject, code, legalName string) (*Merchant, error) {
	return s.EnsureRetailWorkspaceWithMetadata(ctx, subject, code, legalName, MutationMetadata{})
}

func (s *Service) EnsureRetailWorkspaceWithMetadata(ctx context.Context, subject, code, legalName string, meta MutationMetadata) (*Merchant, error) {
	if subject == "" || code == "" || legalName == "" {
		return nil, fmt.Errorf("subject, code and legal name are required")
	}

	if s.pgRepo != nil {
		var result *Merchant
		if err := s.pgRepo.withTx(ctx, func(tx pgx.Tx) error {
			// Serialize first-workspace creation for one identity without
			// imposing a global lock across unrelated merchants.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, subject); err != nil {
				return err
			}

			var existing Merchant
			var status string
			err := tx.QueryRow(ctx, `
				SELECT m.id, m.code, m.legal_name, m.status, m.created_at, m.updated_at
				FROM merchant_memberships mm
				JOIN merchants m ON m.id = mm.merchant_id
				WHERE mm.principal_subject = $1
				  AND mm.status = 'active'
				  AND m.status = 'active'
				ORDER BY mm.created_at, mm.id
				LIMIT 1
			`, subject).Scan(&existing.ID, &existing.Code, &existing.LegalName, &status, &existing.CreatedAt, &existing.UpdatedAt)
			if err == nil {
				existing.Status = MerchantStatus(status)
				result = &existing
				return nil
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			merchant, err := NewMerchant(code, legalName)
			if err != nil {
				return err
			}
			retailCap, supplyCap, err := buildInitialCapabilities(merchant.ID, CapabilityTypeRetail)
			if err != nil {
				return err
			}
			mem, err := NewMerchantMembership(merchant.ID, subject)
			if err != nil {
				return err
			}
			permissions := []string{
				PermissionMerchantManage,
				PermissionTeamManage,
				PermissionRetailStoresManage,
				PermissionRetailCatalogManage,
				PermissionRetailOrdersManage,
				PermissionRetailInventoryManage,
			}
			mem.Permissions = permissions

			if err := s.pgRepo.createMerchantTx(ctx, tx, merchant); err != nil {
				return fmt.Errorf("create retail workspace merchant: %w", err)
			}
			if err := s.pgRepo.upsertCapabilityTx(ctx, tx, retailCap); err != nil {
				return fmt.Errorf("activate retail workspace capability: %w", err)
			}
			if err := s.pgRepo.upsertCapabilityTx(ctx, tx, supplyCap); err != nil {
				return fmt.Errorf("create supply workspace capability: %w", err)
			}
			if err := s.pgRepo.createMembershipTx(ctx, tx, mem); err != nil {
				return fmt.Errorf("create retail workspace membership: %w", err)
			}
			if err := s.pgRepo.grantPermissionsTx(ctx, tx, mem.ID, permissions); err != nil {
				return fmt.Errorf("grant retail workspace permissions: %w", err)
			}
			if err := s.outbox.PublishMerchantCreated(ctx, tx, merchant, correlationID(meta), causationID(meta)); err != nil {
				return err
			}
			if err := s.outbox.PublishCapabilityActivated(ctx, tx, retailCap, correlationID(meta), causationID(meta)); err != nil {
				return err
			}
			if err := s.outbox.PublishMembershipUpdated(ctx, tx, mem, correlationID(meta), causationID(meta)); err != nil {
				return err
			}
			result = merchant
			return nil
		}); err != nil {
			return nil, err
		}
		return result, nil
	}

	// Keep non-Postgres test repositories useful without weakening the
	// production transaction above.
	memberships, err := s.repo.ListMembershipsBySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		if membership.IsActive() {
			return s.repo.GetMerchantByID(ctx, membership.MerchantID)
		}
	}
	merchant, err := s.CreateMerchantWithMetadata(ctx, code, legalName, CapabilityTypeRetail, meta)
	if err != nil {
		return nil, err
	}
	if _, err := s.AddMemberWithMetadata(ctx, merchant.ID, subject, []string{
		PermissionMerchantManage,
		PermissionTeamManage,
		PermissionRetailStoresManage,
		PermissionRetailCatalogManage,
		PermissionRetailOrdersManage,
		PermissionRetailInventoryManage,
	}, meta); err != nil {
		return nil, err
	}
	return merchant, nil
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
