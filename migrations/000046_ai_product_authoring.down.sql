DROP INDEX IF EXISTS idx_product_media_references_product_visible;
DROP INDEX IF EXISTS idx_ai_authoring_image_candidates_store_status_updated;
DROP INDEX IF EXISTS idx_ai_authoring_image_candidates_job_status;
DROP INDEX IF EXISTS idx_ai_authoring_field_proposals_job_status;
DROP INDEX IF EXISTS idx_ai_authoring_field_proposals_job_field_locale_uq;
DROP INDEX IF EXISTS idx_ai_authoring_source_assets_job_primary_uq;
DROP INDEX IF EXISTS idx_ai_authoring_jobs_store_idempotency_uq;
DROP INDEX IF EXISTS idx_ai_authoring_jobs_store_status_created;

DROP TABLE IF EXISTS ai_authoring_image_candidates;
DROP TABLE IF EXISTS ai_authoring_field_proposals;
DROP TABLE IF EXISTS ai_authoring_source_assets;
DROP TABLE IF EXISTS ai_authoring_jobs;

ALTER TABLE product_translations
    DROP COLUMN IF EXISTS meta_keywords;

ALTER TABLE store_media_assets
    DROP COLUMN IF EXISTS origin;

ALTER TABLE product_media_references
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS is_visible;
