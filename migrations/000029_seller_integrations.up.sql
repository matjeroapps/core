CREATE TABLE IF NOT EXISTS seller_channel_sync_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    sync_type VARCHAR(64) NOT NULL DEFAULT 'catalog_import',
    status VARCHAR(32) NOT NULL DEFAULT 'queued',
    total_items INT NOT NULL DEFAULT 0,
    processed_items INT NOT NULL DEFAULT 0,
    failed_items INT NOT NULL DEFAULT 0,
    error_summary TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_seller_channel_sync_jobs_store_id ON seller_channel_sync_jobs(store_id, status);
CREATE INDEX IF NOT EXISTS idx_seller_channel_sync_jobs_connection_id ON seller_channel_sync_jobs(connection_id);
