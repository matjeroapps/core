package integration_test

import (
	"context"
	"testing"

	"core/internal/integration"
)

func TestAPIKeysAndWebhooks(t *testing.T) {
	mockRepo := &mockIntegrationRepo{}
	svc := integration.NewService(mockRepo)
	ctx := context.Background()

	t.Run("create, authenticate, list, and revoke API key", func(t *testing.T) {
		out, err := svc.CreateAPIKey(ctx, integration.CreateAPIKeyInput{
			ActorType: integration.ActorTypeSeller,
			ActorID:   "store-test-1",
			Name:      "Test Store API Key",
			Scopes:    []string{"products:read", "inventory:write"},
			Live:      false,
		})
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		if out.RawAPIKey == "" {
			t.Fatal("expected non-empty RawAPIKey")
		}
		if out.Record.KeyPrefix == "" {
			t.Fatal("expected non-empty KeyPrefix")
		}
		if len(out.Record.Scopes) != 2 {
			t.Fatalf("expected 2 scopes, got %d", len(out.Record.Scopes))
		}

		// Authenticate valid key
		authKey, err := svc.AuthenticateAPIKey(ctx, out.RawAPIKey)
		if err != nil {
			t.Fatalf("AuthenticateAPIKey failed: %v", err)
		}
		if authKey.ID != out.Record.ID {
			t.Errorf("got key ID %s, want %s", authKey.ID, out.Record.ID)
		}

		// Authenticate invalid key
		_, err = svc.AuthenticateAPIKey(ctx, "invalid_key_value")
		if err != integration.ErrInvalidAPIKey {
			t.Errorf("got error %v, want ErrInvalidAPIKey", err)
		}

		// List API keys
		keys, err := svc.ListAPIKeys(ctx, integration.ActorTypeSeller, "store-test-1")
		if err != nil {
			t.Fatalf("ListAPIKeys failed: %v", err)
		}
		if len(keys) == 0 {
			t.Fatal("expected at least 1 key")
		}

		// Revoke API key
		err = svc.RevokeAPIKey(ctx, out.Record.ID, "store-test-1")
		if err != nil {
			t.Fatalf("RevokeAPIKey failed: %v", err)
		}

		// Authenticate revoked key should fail
		_, err = svc.AuthenticateAPIKey(ctx, out.RawAPIKey)
		if err != integration.ErrInvalidAPIKey {
			t.Errorf("got error %v for revoked key, want ErrInvalidAPIKey", err)
		}
	})

	t.Run("create, list, and delete webhook subscription", func(t *testing.T) {
		sub, err := svc.CreateWebhookSubscription(ctx, integration.CreateWebhookSubscriptionInput{
			ActorType:        integration.ActorTypeSeller,
			ActorID:          "store-test-1",
			TargetURL:        "https://example.com/webhooks",
			Secret:           "secret123",
			SubscribedEvents: []string{"order.created", "inventory.updated"},
		})
		if err != nil {
			t.Fatalf("CreateWebhookSubscription failed: %v", err)
		}

		if sub.ID == "" {
			t.Fatal("expected non-empty subscription ID")
		}

		subs, err := svc.ListWebhookSubscriptions(ctx, integration.ActorTypeSeller, "store-test-1")
		if err != nil {
			t.Fatalf("ListWebhookSubscriptions failed: %v", err)
		}
		if len(subs) == 0 {
			t.Fatal("expected at least 1 subscription")
		}

		err = svc.DeleteWebhookSubscription(ctx, sub.ID, "store-test-1")
		if err != nil {
			t.Fatalf("DeleteWebhookSubscription failed: %v", err)
		}
	})
}
