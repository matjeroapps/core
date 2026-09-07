-- Phase 7: Payments Aggregate, Payment Attempts, and Webhook Inbox Schema

CREATE TABLE payments (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders (id) ON DELETE RESTRICT,
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    currency CHAR(3) NOT NULL REFERENCES currencies (code),
    payment_method TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT payments_status_check
        CHECK (status IN (
            'CREATED',
            'PENDING',
            'AUTHORIZED',
            'CAPTURED',
            'FAILED',
            'CANCELLED',
            'REFUNDED'
        ))
);

CREATE INDEX payments_order_id_idx ON payments (order_id);
CREATE INDEX payments_status_idx ON payments (status);

CREATE TABLE payment_attempts (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_reference TEXT,
    status TEXT NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX payment_attempts_payment_id_idx ON payment_attempts (payment_id);

CREATE TABLE webhook_inbox (
    id UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    connection_id TEXT,
    provider_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_json JSONB NOT NULL,
    signature_verified BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count INT NOT NULL DEFAULT 0,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT webhook_inbox_provider_event_uidx UNIQUE (provider, provider_event_id)
);

CREATE INDEX webhook_inbox_status_idx ON webhook_inbox (status, received_at);
