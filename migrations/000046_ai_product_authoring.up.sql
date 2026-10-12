CREATE TABLE IF NOT EXISTS ai_authoring_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
    created_by_subject TEXT NOT NULL,
    brief TEXT NOT NULL DEFAULT '',
    requested_image_count INTEGER NOT NULL CHECK (requested_image_count BETWEEN 1 AND 7),
    effective_image_limit INTEGER NOT NULL CHECK (effective_image_limit BETWEEN 1 AND 7),
    status TEXT NOT NULL CHECK (status IN ('pending','processing','partial','succeeded','failed')),
    text_status TEXT NOT NULL DEFAULT 'pending' CHECK (text_status IN ('pending','processing','succeeded','failed')),
    image_status TEXT NOT NULL DEFAULT 'pending' CHECK (image_status IN ('pending','processing','succeeded','failed')),
    idempotency_key TEXT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_authoring_jobs_store_status_created
    ON ai_authoring_jobs (store_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_authoring_jobs_store_idempotency_uq
    ON ai_authoring_jobs (store_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE TABLE IF NOT EXISTS ai_authoring_source_assets (
    job_id UUID NOT NULL REFERENCES ai_authoring_jobs(id) ON DELETE CASCADE,
    asset_id UUID NOT NULL,
    store_id UUID NOT NULL,
    source_order INTEGER NOT NULL DEFAULT 0,
    is_primary_reference BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_id, asset_id),
    FOREIGN KEY (asset_id, store_id) REFERENCES store_media_assets(id, store_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_authoring_source_assets_job_primary_uq
    ON ai_authoring_source_assets (job_id)
    WHERE is_primary_reference = true;

CREATE TABLE IF NOT EXISTS ai_authoring_field_proposals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES ai_authoring_jobs(id) ON DELETE CASCADE,
    store_id UUID NOT NULL,
    field_key TEXT NOT NULL CHECK (field_key IN ('name','description','keywords','category','attribute')),
    locale TEXT NOT NULL CHECK (locale IN ('ar','en')),
    value TEXT NOT NULL DEFAULT '',
    attribute_code TEXT NOT NULL DEFAULT '',
    category_id UUID NULL,
    status TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','accepted','rejected','failed')),
    edited_by_subject TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (category_id, store_id) REFERENCES store_categories(id, store_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_authoring_field_proposals_job_field_locale_uq
    ON ai_authoring_field_proposals (job_id, field_key, locale, attribute_code);
CREATE INDEX IF NOT EXISTS idx_ai_authoring_field_proposals_job_status
    ON ai_authoring_field_proposals (job_id, status);

CREATE TABLE IF NOT EXISTS ai_authoring_image_candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES ai_authoring_jobs(id) ON DELETE CASCADE,
    store_id UUID NOT NULL,
    source_asset_id UUID NOT NULL,
    scene_key TEXT NOT NULL DEFAULT '',
    storage_key TEXT NOT NULL,
    foreground_storage_key TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL,
    alt_text TEXT NOT NULL DEFAULT '',
    byte_size BIGINT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'hidden' CHECK (visibility IN ('hidden','visible')),
    status TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','accepted','rejected','failed')),
    asset_id UUID NULL,
    product_id UUID NULL REFERENCES products(id) ON DELETE RESTRICT,
    failure_code TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (job_id, source_asset_id) REFERENCES ai_authoring_source_assets(job_id, asset_id) ON DELETE RESTRICT,
    FOREIGN KEY (asset_id, store_id) REFERENCES store_media_assets(id, store_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_ai_authoring_image_candidates_job_status
    ON ai_authoring_image_candidates (job_id, status);
CREATE INDEX IF NOT EXISTS idx_ai_authoring_image_candidates_store_status_updated
    ON ai_authoring_image_candidates (store_id, status, updated_at);

ALTER TABLE product_media_references
    ADD COLUMN IF NOT EXISTS is_visible BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'merchant' CHECK (origin IN ('merchant','ai_generated'));

CREATE INDEX IF NOT EXISTS idx_product_media_references_product_visible
    ON product_media_references (product_id)
    WHERE is_visible = true;

ALTER TABLE store_media_assets
    ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'merchant' CHECK (origin IN ('merchant','ai_generated'));

ALTER TABLE product_translations
    ADD COLUMN IF NOT EXISTS meta_keywords TEXT NOT NULL DEFAULT '';
