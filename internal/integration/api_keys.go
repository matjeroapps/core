package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

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

	return key, nil
}

func (s *service) ListAPIKeys(ctx context.Context, actorType ActorType, actorID string) ([]APIKey, error) {
	return s.repo.ListAPIKeysByActor(ctx, nil, actorType, actorID)
}

func (s *service) RevokeAPIKey(ctx context.Context, keyID, actorID string) error {
	return s.repo.RevokeAPIKey(ctx, nil, keyID, actorID)
}
