DROP INDEX IF EXISTS idx_suppliers_merchant_id;
ALTER TABLE suppliers DROP COLUMN IF EXISTS merchant_id;

DROP INDEX IF EXISTS idx_sellers_merchant_id;
ALTER TABLE sellers DROP COLUMN IF EXISTS merchant_id;
