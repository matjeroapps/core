package payments

import (
	"encoding/json"
	"time"
)

type Payment struct {
	ID            string           `json:"id"`
	OrderID       string           `json:"order_id"`
	AmountMinor   int64            `json:"amount_minor"`
	Currency      string           `json:"currency"`
	PaymentMethod string           `json:"payment_method"`
	Status        Status           `json:"status"`
	Attempts      []PaymentAttempt `json:"attempts,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type PaymentAttempt struct {
	ID                string    `json:"id"`
	PaymentID         string    `json:"payment_id"`
	Provider          string    `json:"provider"`
	ProviderReference string    `json:"provider_reference,omitempty"`
	Status            string    `json:"status"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type WebhookInbox struct {
	ID                string          `json:"id"`
	Provider          string          `json:"provider"`
	ConnectionID      string          `json:"connection_id,omitempty"`
	ProviderEventID   string          `json:"provider_event_id"`
	EventType         string          `json:"event_type"`
	PayloadJSON       json.RawMessage `json:"payload_json"`
	SignatureVerified bool            `json:"signature_verified"`
	Status            string          `json:"status"`
	AttemptCount      int             `json:"attempt_count"`
	ReceivedAt        time.Time       `json:"received_at"`
	ProcessedAt       *time.Time      `json:"processed_at,omitempty"`
}

type InitializePaymentParams struct {
	OrderID           string `json:"order_id"`
	AmountMinor       int64  `json:"amount_minor"`
	Currency          string `json:"currency"`
	PaymentMethod     string `json:"payment_method"`
	Provider          string `json:"provider,omitempty"`
	ProviderReference string `json:"provider_reference,omitempty"`
	CorrelationID     string `json:"-"`
	CausationID       string `json:"-"`
}

type UpdateStatusParams struct {
	PaymentID         string `json:"payment_id"`
	NewStatus         Status `json:"new_status"`
	Provider          string `json:"provider,omitempty"`
	ProviderReference string `json:"provider_reference,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	CorrelationID     string `json:"-"`
	CausationID       string `json:"-"`
}

type PersistWebhookInboxParams struct {
	Provider          string          `json:"provider"`
	ConnectionID      string          `json:"connection_id,omitempty"`
	ProviderEventID   string          `json:"provider_event_id"`
	EventType         string          `json:"event_type"`
	PayloadJSON       json.RawMessage `json:"payload_json"`
	SignatureVerified bool            `json:"signature_verified"`
}
