package merchants

import (
	"context"
	"time"

	"core/packages/events"
	"core/packages/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	EventTypeMerchantCreated             = "merchant.created.v1"
	EventTypeMerchantCapabilityActivated = "merchant.capability.activated.v1"
	EventTypeMerchantMembershipUpdated   = "merchant.membership.updated.v1"
	EventTypeMerchantProfileLinked       = "merchant.profile.linked.v1"
)

type MutationMetadata struct {
	CorrelationID  string
	CausationID    string
	IdempotencyKey string
}

type OutboxPublisher struct {
	store outbox.Store
}

func NewOutboxPublisher() *OutboxPublisher {
	return &OutboxPublisher{
		store: outbox.NewStore(),
	}
}

func (p *OutboxPublisher) PublishMerchantCreated(ctx context.Context, tx pgx.Tx, merchant *Merchant, correlationID, causationID string) error {
	payload := map[string]any{
		"merchant_id": merchant.ID.String(),
		"code":        merchant.Code,
		"legal_name":  merchant.LegalName,
		"status":      string(merchant.Status),
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantCreated,
		SchemaVersion:    1,
		AggregateType:    "merchant",
		AggregateID:      merchant.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	return p.store.Enqueue(ctx, tx, envelope)
}

func (p *OutboxPublisher) PublishCapabilityActivated(ctx context.Context, tx pgx.Tx, cap *MerchantCapability, correlationID, causationID string) error {
	payload := map[string]any{
		"capability_id":   cap.ID.String(),
		"merchant_id":     cap.MerchantID.String(),
		"capability_type": string(cap.CapabilityType),
		"status":          string(cap.Status),
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantCapabilityActivated,
		SchemaVersion:    1,
		AggregateType:    "merchant",
		AggregateID:      cap.MerchantID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	return p.store.Enqueue(ctx, tx, envelope)
}

func (p *OutboxPublisher) PublishMembershipUpdated(ctx context.Context, tx pgx.Tx, mem *MerchantMembership, correlationID, causationID string) error {
	payload := map[string]any{
		"membership_id":     mem.ID.String(),
		"merchant_id":       mem.MerchantID.String(),
		"principal_subject": mem.PrincipalSubject,
		"status":            string(mem.Status),
		"permissions":       mem.Permissions,
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantMembershipUpdated,
		SchemaVersion:    1,
		AggregateType:    "merchant",
		AggregateID:      mem.MerchantID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	return p.store.Enqueue(ctx, tx, envelope)
}

func (p *OutboxPublisher) PublishProfileLinked(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, profileType string, profileID uuid.UUID, correlationID, causationID string) error {
	payload := map[string]any{
		"merchant_id":  merchantID.String(),
		"profile_type": profileType,
		"profile_id":   profileID.String(),
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantProfileLinked,
		SchemaVersion:    1,
		AggregateType:    "merchant",
		AggregateID:      merchantID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	return p.store.Enqueue(ctx, tx, envelope)
}
