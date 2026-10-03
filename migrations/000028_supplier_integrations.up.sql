CREATE TABLE IF NOT EXISTS supplier_catalog_sync_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id VARCHAR(64) NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    supplier_id UUID NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
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

CREATE INDEX IF NOT EXISTS idx_supplier_sync_jobs_connection ON supplier_catalog_sync_jobs(connection_id);
CREATE INDEX IF NOT EXISTS idx_supplier_sync_jobs_supplier ON supplier_catalog_sync_jobs(supplier_id);
CREATE INDEX IF NOT EXISTS idx_supplier_sync_jobs_status ON supplier_catalog_sync_jobs(status);
