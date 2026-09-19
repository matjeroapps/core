DROP TABLE IF EXISTS webhook_outbox CASCADE;
DROP TABLE IF EXISTS api_key_audit_logs CASCADE;

ALTER TABLE api_keys DROP COLUMN IF EXISTS last_used_at;
ALTER TABLE api_keys DROP COLUMN IF EXISTS rotated_at;
ALTER TABLE api_keys DROP COLUMN IF EXISTS revocation_reason;

DROP TABLE IF EXISTS idempotency_records CASCADE;
