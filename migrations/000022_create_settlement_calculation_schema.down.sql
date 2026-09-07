DROP TRIGGER IF EXISTS trg_immutable_finalized_settlements ON settlements;
DROP FUNCTION IF EXISTS prevent_finalized_settlement_modification();
DROP TABLE IF EXISTS settlements;
ALTER TABLE settlement_periods DROP CONSTRAINT IF EXISTS settlement_periods_status_check;
ALTER TABLE settlement_periods ADD CONSTRAINT settlement_periods_status_check CHECK (status IN ('OPEN', 'CLOSED'));
