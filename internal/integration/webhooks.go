package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrReplayAttack     = errors.New("webhook timestamp outside tolerance window")
	ErrMalformedHeader  = errors.New("malformed webhook signature header")
)

type CreateWebhookSubscriptionInput struct {
	ActorType        ActorType
	ActorID          string
	TargetURL        string
	Secret           string
	SubscribedEvents []string
}

// SignWebhookPayload generates an HMAC-SHA256 signature for a payload given a secret and timestamp.
// Signature format: t=<timestamp>,v1=<hex_signature>
func SignWebhookPayload(payload []byte, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", timestamp, string(payload))))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, sig)
}

// VerifyWebhookSignature verifies the HMAC signature and checks timestamp tolerance (replay protection).
func VerifyWebhookSignature(payload []byte, signatureHeader string, secret string, maxAge time.Duration) error {
	if strings.TrimSpace(signatureHeader) == "" || strings.TrimSpace(secret) == "" {
		return ErrInvalidSignature
	}

	parts := strings.Split(signatureHeader, ",")
	var timestampStr, signature string
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			if kv[0] == "t" {
				timestampStr = kv[1]
			} else if kv[0] == "v1" {
				signature = kv[1]
			}
		}
	}

	if timestampStr == "" || signature == "" {
		return ErrMalformedHeader
	}

	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return ErrMalformedHeader
	}

	if maxAge > 0 {
		now := time.Now().Unix()
		diff := now - ts
		if diff < 0 {
			diff = -diff
		}
		if time.Duration(diff)*time.Second > maxAge {
			return ErrReplayAttack
		}
	}

	expectedSig := SignWebhookPayload(payload, secret, ts)
	expParts := strings.Split(expectedSig, ",")
	var expectedV1 string
	for _, p := range expParts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 && kv[0] == "v1" {
			expectedV1 = kv[1]
		}
	}

	sigBytes, err1 := hex.DecodeString(signature)
	expBytes, err2 := hex.DecodeString(expectedV1)
	if err1 != nil || err2 != nil || !hmac.Equal(sigBytes, expBytes) {
		return ErrInvalidSignature
	}

	return nil
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

func (s *service) DispatchWebhookEvent(ctx context.Context, actorType ActorType, actorID, eventType string, payload json.RawMessage) ([]WebhookOutboxItem, error) {
	subs, err := s.repo.ListWebhookSubscriptionsByActor(ctx, nil, actorType, actorID)
	if err != nil {
		return nil, err
	}

	var enqueued []WebhookOutboxItem
	now := time.Now().UTC()

	for _, sub := range subs {
		if sub.Status != "active" {
			continue
		}

		matches := false
		for _, e := range sub.SubscribedEvents {
			if e == "*" || e == eventType {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}

		item := WebhookOutboxItem{
			ID:             "whout_" + uuid.New().String(),
			SubscriptionID: sub.ID,
			EventType:      eventType,
			Payload:        payload,
			Status:         "pending",
			RetryCount:     0,
			MaxRetries:     5,
			NextAttemptAt:  now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := s.repo.EnqueueWebhookOutbox(ctx, nil, item); err == nil {
			enqueued = append(enqueued, item)
		}
	}

	return enqueued, nil
}

func (s *service) DeliverWebhookOutboxItem(ctx context.Context, item WebhookOutboxItem, secret string, client *http.Client) error {
	sub, err := s.repo.GetWebhookSubscriptionByID(ctx, nil, item.SubscriptionID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	timestamp := now.Unix()
	signatureHeader := SignWebhookPayload(item.Payload, secret, timestamp)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.TargetURL, bytes.NewBuffer(item.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signatureHeader)
	req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(timestamp, 10))

	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	statusCode := 0
	var deliveryErr error

	if err != nil {
		deliveryErr = err
	} else {
		statusCode = resp.StatusCode
		_ = resp.Body.Close()
		if statusCode < 200 || statusCode >= 300 {
			deliveryErr = fmt.Errorf("HTTP status %d", statusCode)
		}
	}

	statusPtr := &statusCode
	if statusCode == 0 {
		statusPtr = nil
	}

	var nextRetry *time.Time
	deliveryStatus := "delivered"
	if deliveryErr != nil {
		deliveryStatus = "failed"
		retryCount := item.RetryCount + 1
		if retryCount < item.MaxRetries {
			backoff := time.Duration(1<<retryCount) * 10 * time.Second
			nr := now.Add(backoff)
			nextRetry = &nr
			_ = s.repo.UpdateWebhookOutboxStatus(ctx, nil, item.ID, "pending", retryCount, nr, deliveryErr.Error())
		} else {
			_ = s.repo.UpdateWebhookOutboxStatus(ctx, nil, item.ID, "failed", retryCount, now, deliveryErr.Error())
		}
	} else {
		_ = s.repo.UpdateWebhookOutboxStatus(ctx, nil, item.ID, "delivered", item.RetryCount, now, "")
	}

	logEntry := WebhookDeliveryLog{
		ID:                 "deliv_" + uuid.New().String(),
		SubscriptionID:     item.SubscriptionID,
		EventType:          item.EventType,
		Payload:            item.Payload,
		ResponseStatusCode: statusPtr,
		Attempts:           item.RetryCount + 1,
		Status:             deliveryStatus,
		NextRetryAt:        nextRetry,
		DeliveredAt:        &now,
		CreatedAt:          now,
	}

	_ = s.repo.RecordWebhookDeliveryLog(ctx, nil, logEntry)

	return deliveryErr
}
