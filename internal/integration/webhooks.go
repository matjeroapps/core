package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CreateWebhookSubscriptionInput struct {
	ActorType        ActorType
	ActorID          string
	TargetURL        string
	Secret           string
	SubscribedEvents []string
}

func (s *service) CreateWebhookSubscription(ctx context.Context, input CreateWebhookSubscriptionInput) (*WebhookSubscription, error) {
	if strings.TrimSpace(input.ActorID) == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	if strings.TrimSpace(input.TargetURL) == "" {
		return nil, fmt.Errorf("target_url is required")
	}

	secretHash := ""
	if input.Secret != "" {
		h := sha256.Sum256([]byte(input.Secret))
		secretHash = hex.EncodeToString(h[:])
	}

	subID := "sub_" + uuid.New().String()
	now := time.Now().UTC()

	sub := WebhookSubscription{
		ID:               subID,
		ActorType:        input.ActorType,
		ActorID:          input.ActorID,
		TargetURL:        input.TargetURL,
		SecretHash:       secretHash,
		SubscribedEvents: input.SubscribedEvents,
		Status:           "active",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.CreateWebhookSubscription(ctx, nil, sub); err != nil {
		return nil, err
	}

	return &sub, nil
}

func (s *service) ListWebhookSubscriptions(ctx context.Context, actorType ActorType, actorID string) ([]WebhookSubscription, error) {
	return s.repo.ListWebhookSubscriptionsByActor(ctx, nil, actorType, actorID)
}

func (s *service) DeleteWebhookSubscription(ctx context.Context, subID, actorID string) error {
	return s.repo.DeleteWebhookSubscription(ctx, nil, subID, actorID)
}

func (s *service) GetWebhookSubscription(ctx context.Context, subID string) (*WebhookSubscription, error) {
	return s.repo.GetWebhookSubscriptionByID(ctx, nil, subID)
}
