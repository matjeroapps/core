package merchants

import (
	"context"
	"time"

	"core/packages/events"
	"core/packages/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
		EventType:        "merchant.created",
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
		EventType:        "merchant.capability.activated",
		SchemaVersion:    1,
		AggregateType:    "merchant_capability",
		AggregateID:      cap.ID.String(),
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
		EventType:        "merchant.membership.updated",
		SchemaVersion:    1,
		AggregateType:    "merchant_membership",
		AggregateID:      mem.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	return p.store.Enqueue(ctx, tx, envelope)
}
