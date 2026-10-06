-- Migration 000042 Down: Revert inventory threshold and shipment carrier details

DROP INDEX IF EXISTS idx_shipments_tracking_number;

ALTER TABLE shipments
    DROP COLUMN IF EXISTS carrier_name;

ALTER TABLE inventory_snapshots
    DROP COLUMN IF EXISTS low_stock_threshold;
