DROP TRIGGER IF EXISTS trg_immutable_finalized_settlement_allocations ON settlement_allocations;
DROP FUNCTION IF EXISTS prevent_finalized_settlement_allocation_modification();
DROP TABLE IF EXISTS settlement_allocations;
DROP TABLE IF EXISTS financial_rules;
