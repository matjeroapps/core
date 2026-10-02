ALTER TABLE sellers ADD COLUMN IF NOT EXISTS merchant_id UUID REFERENCES merchants(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_sellers_merchant_id ON sellers(merchant_id);

ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS merchant_id UUID REFERENCES merchants(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_suppliers_merchant_id ON suppliers(merchant_id);
