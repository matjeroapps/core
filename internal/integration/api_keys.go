package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ScopeFullAccess     = "full_access"
	ScopeProductsRead   = "read:products"
	ScopeProductsWrite  = "write:products"
	ScopeOrdersRead     = "read:orders"
	ScopeOrdersWrite    = "write:orders"
	ScopeInventoryRead  = "read:inventory"
	ScopeInventoryWrite = "write:inventory"
	ScopeWebhooksManage = "manage:webhooks"
)

// HasScope checks if the provided scopes grant the required permission.
func HasScope(keyScopes []string, requiredScope string) bool {
	if strings.TrimSpace(requiredScope) == "" {
		return true
	}
	for _, s := range keyScopes {
		s = strings.TrimSpace(s)
		if s == ScopeFullAccess || s == requiredScope {
			return true
		}
	}
	return false
}

// GenerateRawAPIKey creates a secure random API key token with a readable prefix.
func GenerateRawAPIKey(live bool) (raw string, prefix string, hash string, err error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("read random bytes: %w", err)
	}
	randomHex := hex.EncodeToString(bytes)
	envPrefix := "mj_test_"
	if live {
		envPrefix = "mj_live_"
	}
	raw = envPrefix + randomHex
	prefix = envPrefix + randomHex[:6]
	hash = HashAPIKey(raw)
	return raw, prefix, hash, nil
}

// HashAPIKey computes the SHA-256 digest of a raw API key string.
func HashAPIKey(raw string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(h[:])
}

// CreateAPIKeyInput contains arguments for creating an API key.
type CreateAPIKeyInput struct {
	ActorType ActorType
	ActorID   string
	Name      string
	Scopes    []string
	Live      bool
	ExpiresAt *time.Time
}

// CreateAPIKeyOutput contains the created API key record and the raw key string (shown once).
type CreateAPIKeyOutput struct {
	Record    APIKey
	RawAPIKey string
}

func (s *service) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreateAPIKeyOutput, error) {
	if strings.TrimSpace(input.ActorID) == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}

	raw, prefix, hash, err := GenerateRawAPIKey(input.Live)
	if err != nil {
		return nil, err
	}

	keyID := "key_" + uuid.New().String()
	now := time.Now().UTC()

	key := APIKey{
		ID:        keyID,
		ActorType: input.ActorType,
		ActorID:   input.ActorID,
		Name:      input.Name,
		KeyPrefix: prefix,
		KeyHash:   hash,
		Scopes:    input.Scopes,
		Status:    "active",
		ExpiresAt: input.ExpiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateAPIKey(ctx, nil, key); err != nil {
		return nil, err
	}

	auditLog := APIKeyAuditLog{
		ID:        "audit_" + uuid.New().String(),
		APIKeyID:  keyID,
		ActorType: input.ActorType,
		ActorID:   input.ActorID,
		Action:    "created",
		Details:   json.RawMessage(fmt.Sprintf(`{"name":%q,"prefix":%q}`, input.Name, prefix)),
		CreatedAt: now,
	}
	_ = s.repo.RecordAPIKeyAuditLog(ctx, nil, auditLog)

	return &CreateAPIKeyOutput{
		Record:    key,
		RawAPIKey: raw,
	}, nil
}

func (s *service) AuthenticateAPIKey(ctx context.Context, rawKey string) (*APIKey, error) {
	if strings.TrimSpace(rawKey) == "" {
		return nil, ErrInvalidAPIKey
	}
	hash := HashAPIKey(rawKey)

	key, err := s.repo.GetAPIKeyByHash(ctx, nil, hash)
	if err != nil {
		return nil, ErrInvalidAPIKey
	}

	if key.Status != "active" {
		return nil, ErrInvalidAPIKey
	}

	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now().UTC()) {
		return nil, ErrInvalidAPIKey
	}

	now := time.Now().UTC()
	key.LastUsedAt = &now
	_ = s.repo.UpdateAPIKeyLastUsed(ctx, nil, key.ID, now)

	return key, nil
}

func (s *service) ListAPIKeys(ctx context.Context, actorType ActorType, actorID string) ([]APIKey, error) {
	return s.repo.ListAPIKeysByActor(ctx, nil, actorType, actorID)
}

func (s *service) RevokeAPIKey(ctx context.Context, keyID, actorID string) error {
	return s.RevokeAPIKeyWithReason(ctx, keyID, actorID, "user_requested")
}

func (s *service) RevokeAPIKeyWithReason(ctx context.Context, keyID, actorID, reason string) error {
	if err := s.repo.RevokeAPIKeyWithReason(ctx, nil, keyID, actorID, reason); err != nil {
		return err
	}

	auditLog := APIKeyAuditLog{
		ID:        "audit_" + uuid.New().String(),
		APIKeyID:  keyID,
		ActorType: ActorTypeSeller,
		ActorID:   actorID,
		Action:    "revoked",
		Details:   json.RawMessage(fmt.Sprintf(`{"reason":%q}`, reason)),
		CreatedAt: time.Now().UTC(),
	}
	_ = s.repo.RecordAPIKeyAuditLog(ctx, nil, auditLog)
	return nil
}

func (s *service) RotateAPIKey(ctx context.Context, keyID, actorID string, gracePeriod time.Duration) (*CreateAPIKeyOutput, error) {
	keys, err := s.repo.ListAPIKeysByActor(ctx, nil, ActorTypeSeller, actorID)
	if err != nil {
		return nil, err
	}
	var existing *APIKey
	for _, k := range keys {
		if k.ID == keyID {
			existing = &k
			break
		}
	}
	if existing == nil {
		return nil, ErrAPIKeyNotFound
	}

	now := time.Now().UTC()
	var newExp *time.Time
	if gracePeriod > 0 {
		exp := now.Add(gracePeriod)
		newExp = &exp
	} else {
		newExp = &now
	}

	if err := s.repo.MarkAPIKeyRotated(ctx, nil, keyID, actorID, now, newExp); err != nil {
		return nil, err
	}

	out, err := s.CreateAPIKey(ctx, CreateAPIKeyInput{
		ActorType: existing.ActorType,
		ActorID:   existing.ActorID,
		Name:      existing.Name + " (Rotated)",
		Scopes:    existing.Scopes,
		Live:      strings.HasPrefix(existing.KeyPrefix, "mj_live_"),
	})
	if err != nil {
		return nil, err
	}

	auditLog := APIKeyAuditLog{
		ID:        "audit_" + uuid.New().String(),
		APIKeyID:  keyID,
		ActorType: existing.ActorType,
		ActorID:   actorID,
		Action:    "rotated",
		Details:   json.RawMessage(fmt.Sprintf(`{"new_key_id":%q}`, out.Record.ID)),
		CreatedAt: now,
	}
	_ = s.repo.RecordAPIKeyAuditLog(ctx, nil, auditLog)

	return out, nil
}
