DROP INDEX IF EXISTS idx_stores_seller_status;
ALTER TABLE stores DROP CONSTRAINT IF EXISTS stores_status_check;
