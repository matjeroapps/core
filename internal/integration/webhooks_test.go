package integration

import (
	"testing"
	"time"
)

func TestWebhookSignatureAndReplayProtection(t *testing.T) {
	secret := "whsec_test_sample_key_99999" // gitleaks:allow
	payload := []byte(`{"event":"order.created","id":"ord_999","amount":15000}`)
	now := time.Now().Unix()

	// Sign payload
	sigHeader := SignWebhookPayload(payload, secret, now)

	// Verify valid signature within maxAge
	err := VerifyWebhookSignature(payload, sigHeader, secret, 5*time.Minute)
	if err != nil {
		t.Fatalf("expected signature to be valid, got: %v", err)
	}

	// Verify rejected with wrong secret
	errWrongSecret := VerifyWebhookSignature(payload, sigHeader, "wrong_secret", 5*time.Minute)
	if errWrongSecret != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for wrong secret, got: %v", errWrongSecret)
	}

	// Verify rejected if payload tampered
	tamperedPayload := []byte(`{"event":"order.created","id":"ord_999","amount":99999}`)
	errTampered := VerifyWebhookSignature(tamperedPayload, sigHeader, secret, 5*time.Minute)
	if errTampered != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature for tampered payload, got: %v", errTampered)
	}

	// Replay protection test: Timestamp 10 minutes ago when maxAge is 5 minutes
	oldTimestamp := now - 600
	oldSigHeader := SignWebhookPayload(payload, secret, oldTimestamp)
	errReplay := VerifyWebhookSignature(payload, oldSigHeader, secret, 5*time.Minute)
	if errReplay != ErrReplayAttack {
		t.Errorf("expected ErrReplayAttack for old timestamp, got: %v", errReplay)
	}
}
