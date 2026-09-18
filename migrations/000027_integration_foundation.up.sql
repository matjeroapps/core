CREATE TABLE IF NOT EXISTS integration_connections (
    id VARCHAR(64) PRIMARY KEY,
    actor_type VARCHAR(32) NOT NULL,
    actor_id VARCHAR(64) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    credentials_vault_ref VARCHAR(255),
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_connections_actor ON integration_connections(actor_type, actor_id);
CREATE INDEX IF NOT EXISTS idx_integration_connections_provider ON integration_connections(provider);

CREATE TABLE IF NOT EXISTS external_entity_mappings (
    id VARCHAR(64) PRIMARY KEY,
    connection_id VARCHAR(64) NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    entity_type VARCHAR(64) NOT NULL,
    internal_id VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL,
    external_version VARCHAR(64),
    mapping_status VARCHAR(32) NOT NULL DEFAULT 'synced',
    sync_direction VARCHAR(32) NOT NULL DEFAULT 'bidirectional',
    conflict_status VARCHAR(64),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_external_entity_mappings_external UNIQUE (connection_id, entity_type, external_id),
    CONSTRAINT uq_external_entity_mappings_internal UNIQUE (connection_id, entity_type, internal_id)
);

CREATE INDEX IF NOT EXISTS idx_external_entity_mappings_connection ON external_entity_mappings(connection_id);
CREATE INDEX IF NOT EXISTS idx_external_entity_mappings_internal ON external_entity_mappings(entity_type, internal_id);

CREATE TABLE IF NOT EXISTS integration_sync_cursors (
    id VARCHAR(64) PRIMARY KEY,
    connection_id VARCHAR(64) NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    entity_type VARCHAR(64) NOT NULL,
    cursor_token TEXT,
    last_successful_sync TIMESTAMPTZ,
    last_reconciled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_integration_sync_cursors UNIQUE (connection_id, entity_type)
);

CREATE TABLE IF NOT EXISTS integration_webhook_inbox (
    id VARCHAR(64) PRIMARY KEY,
    connection_id VARCHAR(64) REFERENCES integration_connections(id) ON DELETE SET NULL,
    provider VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'received',
    error_message TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT uq_integration_webhook_inbox_idempotency UNIQUE (provider, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_integration_webhook_inbox_status ON integration_webhook_inbox(status);
