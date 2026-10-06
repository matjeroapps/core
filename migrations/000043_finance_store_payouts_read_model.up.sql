-- Migration 000043: Store payouts read model and persistence

CREATE TABLE IF NOT EXISTS store_payouts (
    id UUID PRIMARY KEY,
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL,
    amount_minor_units BIGINT NOT NULL CHECK (amount_minor_units > 0),
    currency CHAR(3) NOT NULL REFERENCES currencies(code),
    status TEXT NOT NULL,
    destination_bank TEXT NOT NULL,
    reference_number TEXT UNIQUE,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT store_payouts_status_check CHECK (status IN (
        'REQUESTED',
        'PROCESSING',
        'PAID',
        'REJECTED',
        'FAILED'
    ))
);

CREATE INDEX IF NOT EXISTS idx_store_payouts_store_created
    ON store_payouts (store_id, created_at DESC);
