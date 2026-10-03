CREATE TABLE IF NOT EXISTS merchant_integration_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_type VARCHAR(32) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    external_account_id VARCHAR(255),
    name VARCHAR(255) NOT NULL,
    store_id UUID REFERENCES stores(id) ON DELETE SET NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    granted_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    health_status VARCHAR(32) NOT NULL DEFAULT 'healthy',
    last_health_check_at TIMESTAMPTZ,
    first_successful_sync_at TIMESTAMPTZ,
    legacy_actor_type VARCHAR(32),
    legacy_actor_id VARCHAR(64),
    legacy_connection_id VARCHAR(64),
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_integration_conn_account UNIQUE (merchant_id, provider, external_account_id),
    CONSTRAINT ck_merchant_integration_conn_active_account CHECK (status <> 'ACTIVE' OR external_account_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_merchant_integration_conn_merchant ON merchant_integration_connections(merchant_id);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_conn_type ON merchant_integration_connections(merchant_id, connection_type);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_conn_store ON merchant_integration_connections(store_id) WHERE store_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_merchant_integration_conn_status ON merchant_integration_connections(status);

-- At most one in-flight setup row per (merchant, provider) while the external
-- account is still unknown; operational rows are covered by
-- uq_merchant_integration_conn_account.
CREATE UNIQUE INDEX IF NOT EXISTS uq_merchant_integration_conn_pending_account
    ON merchant_integration_connections (merchant_id, provider)
    WHERE external_account_id IS NULL;

CREATE TABLE IF NOT EXISTS merchant_integration_job_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES merchant_integration_connections(id) ON DELETE CASCADE,
    pipeline_type VARCHAR(32) NOT NULL,
    job_type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'REQUESTED',
    idempotency_key VARCHAR(255) NOT NULL,
    cursor_or_version VARCHAR(255),
    correlation_id VARCHAR(128),
    causation_id VARCHAR(128),
    total_items INT NOT NULL DEFAULT 0,
    processed_items INT NOT NULL DEFAULT 0,
    failed_items INT NOT NULL DEFAULT 0,
    error_summary TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_integration_job_idempotency UNIQUE (connection_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_merchant_integration_jobs_merchant ON merchant_integration_job_intents(merchant_id);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_jobs_connection ON merchant_integration_job_intents(connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_jobs_status ON merchant_integration_job_intents(status);

CREATE TABLE IF NOT EXISTS merchant_integration_migration_crosswalk (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legacy_connection_id VARCHAR(64) NOT NULL,
    legacy_actor_type VARCHAR(32) NOT NULL,
    legacy_actor_id VARCHAR(64) NOT NULL,
    target_connection_id UUID REFERENCES merchant_integration_connections(id) ON DELETE SET NULL,
    merchant_id UUID REFERENCES merchants(id) ON DELETE SET NULL,
    classification VARCHAR(32) NOT NULL,
    reused_existing_connection BOOLEAN NOT NULL DEFAULT FALSE,
    quarantine_reason_code VARCHAR(64),
    quarantine_details JSONB NOT NULL DEFAULT '{}'::jsonb,
    run_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_merchant_integration_crosswalk_legacy ON merchant_integration_migration_crosswalk(legacy_connection_id);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_crosswalk_merchant ON merchant_integration_migration_crosswalk(merchant_id);
CREATE INDEX IF NOT EXISTS idx_merchant_integration_crosswalk_run ON merchant_integration_migration_crosswalk(run_id);
