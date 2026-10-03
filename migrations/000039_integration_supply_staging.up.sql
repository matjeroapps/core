-- Integration Track I2: Merchant-owned Supply pipeline staging, review,
-- mappings, cursors, fulfillment requests, tracking events, and webhook inbox.
-- Additive only: legacy Feature 021 integration tables are untouched.

CREATE TABLE IF NOT EXISTS merchant_integration_supply_import_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    provider VARCHAR(64) NOT NULL,
    batch_type VARCHAR(32) NOT NULL DEFAULT 'FIRST_IMPORT',
    status VARCHAR(32) NOT NULL DEFAULT 'STAGED',
    first_import BOOLEAN NOT NULL DEFAULT TRUE,
    cursor_token TEXT,
    record_count INT NOT NULL DEFAULT 0,
    approved_count INT NOT NULL DEFAULT 0,
    rejected_count INT NOT NULL DEFAULT 0,
    duplicate_count INT NOT NULL DEFAULT 0,
    idempotency_key VARCHAR(255) NOT NULL,
    correlation_id VARCHAR(128),
    causation_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_supply_batch_idempotency UNIQUE (connection_id, idempotency_key),
    CONSTRAINT ck_merchant_supply_batch_type CHECK (batch_type IN ('FIRST_IMPORT', 'FULL', 'INCREMENTAL')),
    CONSTRAINT ck_merchant_supply_batch_status CHECK (status IN ('STAGED', 'IN_REVIEW', 'APPROVED', 'REJECTED', 'PARTIALLY_APPROVED'))
);

CREATE INDEX IF NOT EXISTS idx_merchant_supply_batch_connection ON merchant_integration_supply_import_batches(connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_supply_batch_merchant ON merchant_integration_supply_import_batches(merchant_id);

CREATE TABLE IF NOT EXISTS merchant_integration_supply_import_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES merchant_integration_supply_import_batches(id) ON DELETE CASCADE,
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    entity_type VARCHAR(32) NOT NULL,
    external_product_id VARCHAR(255) NOT NULL,
    external_variant_id VARCHAR(255),
    sku VARCHAR(255),
    barcode VARCHAR(255),
    title VARCHAR(512),
    currency VARCHAR(8),
    external_version VARCHAR(255),
    content_digest VARCHAR(128) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'STAGED',
    duplicate_of_connection UUID REFERENCES merchant_integration_connections(id) ON DELETE SET NULL,
    review_case_id UUID,
    mapping_decision JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_merchant_supply_record_entity CHECK (entity_type IN ('PRODUCT', 'VARIANT', 'PRICE', 'INVENTORY')),
    CONSTRAINT ck_merchant_supply_record_status CHECK (status IN ('STAGED', 'DUPLICATE_REVIEW', 'APPROVED', 'REJECTED', 'REVIEW_REQUIRED'))
);

CREATE INDEX IF NOT EXISTS idx_merchant_supply_record_batch ON merchant_integration_supply_import_records(batch_id);
CREATE INDEX IF NOT EXISTS idx_merchant_supply_record_connection ON merchant_integration_supply_import_records(connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_supply_record_sku ON merchant_integration_supply_import_records(sku) WHERE sku IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_merchant_supply_record_barcode ON merchant_integration_supply_import_records(barcode) WHERE barcode IS NOT NULL;
-- One staged fact per external identity per connection; retries of the same
-- import page upsert instead of duplicating.
CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_supply_record_external
    ON merchant_integration_supply_import_records (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, ''));

CREATE TABLE IF NOT EXISTS merchant_integration_review_cases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    case_type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'OPEN',
    reason_code VARCHAR(64) NOT NULL,
    subject_batch_id UUID REFERENCES merchant_integration_supply_import_batches(id) ON DELETE SET NULL,
    subject_record_id UUID,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolution VARCHAR(64),
    resolved_by VARCHAR(255),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_merchant_review_case_status CHECK (status IN ('OPEN', 'RESOLVED', 'DISMISSED'))
);

CREATE INDEX IF NOT EXISTS idx_merchant_review_case_merchant ON merchant_integration_review_cases(merchant_id);
CREATE INDEX IF NOT EXISTS idx_merchant_review_case_connection ON merchant_integration_review_cases(connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_review_case_status ON merchant_integration_review_cases(status);

CREATE TABLE IF NOT EXISTS merchant_integration_entity_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    entity_type VARCHAR(32) NOT NULL,
    internal_id VARCHAR(255) NOT NULL,
    external_product_id VARCHAR(255) NOT NULL,
    external_variant_id VARCHAR(255),
    external_version VARCHAR(255),
    content_digest VARCHAR(128),
    authority_source VARCHAR(32) NOT NULL DEFAULT 'EXTERNAL',
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    approved_batch_id UUID REFERENCES merchant_integration_supply_import_batches(id) ON DELETE SET NULL,
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_merchant_entity_mapping_authority CHECK (authority_source IN ('EXTERNAL', 'MATJERHUB', 'MANUAL')),
    CONSTRAINT ck_merchant_entity_mapping_status CHECK (status IN ('ACTIVE', 'REVIEW_REQUIRED', 'RETIRED'))
);

-- Expression index: one approved mapping per external identity per connection.
CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_entity_mapping_external
    ON merchant_integration_entity_mappings (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, ''));

CREATE INDEX IF NOT EXISTS idx_merchant_entity_mapping_merchant ON merchant_integration_entity_mappings(merchant_id);
CREATE INDEX IF NOT EXISTS idx_merchant_entity_mapping_internal ON merchant_integration_entity_mappings(internal_id);

CREATE TABLE IF NOT EXISTS merchant_integration_sync_cursors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    entity_type VARCHAR(32) NOT NULL,
    cursor_token TEXT,
    last_successful_sync TIMESTAMPTZ,
    last_reconciled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_integration_sync_cursors UNIQUE (connection_id, entity_type)
);

CREATE TABLE IF NOT EXISTS merchant_integration_supply_fulfillment_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'REQUESTED',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    external_fulfillment_id VARCHAR(255),
    provider VARCHAR(64) NOT NULL,
    correlation_id VARCHAR(128),
    causation_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_supply_fulfillment_idempotency UNIQUE (connection_id, idempotency_key),
    CONSTRAINT ck_merchant_supply_fulfillment_status CHECK (status IN ('REQUESTED', 'SENT_TO_PROVIDER', 'ACKNOWLEDGED', 'IN_PREPARATION', 'SHIPPED', 'DELIVERED', 'REJECTED', 'CANCELLED', 'FAILED'))
);

CREATE INDEX IF NOT EXISTS idx_merchant_supply_fulfillment_connection ON merchant_integration_supply_fulfillment_requests(connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_supply_fulfillment_merchant ON merchant_integration_supply_fulfillment_requests(merchant_id);

CREATE TABLE IF NOT EXISTS merchant_integration_supply_tracking_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES merchant_integration_supply_fulfillment_requests(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    external_event_id VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL,
    carrier VARCHAR(128),
    tracking_number VARCHAR(255),
    occurred_at TIMESTAMPTZ,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_supply_tracking_external UNIQUE (connection_id, external_event_id)
);

CREATE INDEX IF NOT EXISTS idx_merchant_supply_tracking_request ON merchant_integration_supply_tracking_events(request_id);

CREATE TABLE IF NOT EXISTS merchant_integration_webhook_inbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID REFERENCES merchant_integration_connections(id) ON DELETE SET NULL,
    provider VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'received',
    error_message TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT uq_merchant_integration_webhook_inbox_idempotency UNIQUE (provider, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_merchant_integration_webhook_inbox_status ON merchant_integration_webhook_inbox(status);

-- A synchronized connection's type is immutable: once the first import batch
-- exists for a connection the connection type may no longer be changed. The
-- merchant-owned connection API never mutates type in place; this trigger
-- guards every writer at the database boundary.
CREATE OR REPLACE FUNCTION enforce_merchant_connection_type_immutable() RETURNS trigger AS $$
BEGIN
    IF OLD.connection_type IS DISTINCT FROM NEW.connection_type
       AND EXISTS (SELECT 1 FROM merchant_integration_supply_import_batches b WHERE b.connection_id = NEW.id) THEN
        RAISE EXCEPTION 'connection_type is immutable after first successful synchronization'
            USING ERRCODE = 'raise_exception';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_merchant_connection_type_immutable ON merchant_integration_connections;
CREATE TRIGGER trg_merchant_connection_type_immutable
    BEFORE UPDATE ON merchant_integration_connections
    FOR EACH ROW EXECUTE FUNCTION enforce_merchant_connection_type_immutable();
