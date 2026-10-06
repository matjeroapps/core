-- Migration 000042: Inventory low-stock threshold and shipment carrier details

ALTER TABLE inventory_snapshots
    ADD COLUMN IF NOT EXISTS low_stock_threshold INTEGER NOT NULL DEFAULT 5 CHECK (low_stock_threshold >= 0);

ALTER TABLE shipments
    ADD COLUMN IF NOT EXISTS carrier_name TEXT;

CREATE INDEX IF NOT EXISTS idx_shipments_tracking_number ON shipments (tracking_number);
