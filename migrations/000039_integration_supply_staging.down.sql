-- Reversal of 000039: drop the trigger/function first, then the I2 supply
-- tables in dependency order. Legacy Feature 021 tables are untouched.

DROP TRIGGER IF EXISTS trg_merchant_connection_type_immutable ON merchant_integration_connections;
DROP FUNCTION IF EXISTS enforce_merchant_connection_type_immutable();

DROP TABLE IF EXISTS merchant_integration_webhook_inbox;
DROP TABLE IF EXISTS merchant_integration_supply_tracking_events;
DROP TABLE IF EXISTS merchant_integration_supply_fulfillment_requests;
DROP TABLE IF EXISTS merchant_integration_sync_cursors;
DROP TABLE IF EXISTS merchant_integration_entity_mappings;
DROP TABLE IF EXISTS merchant_integration_review_cases;
DROP TABLE IF EXISTS merchant_integration_supply_import_records;
DROP TABLE IF EXISTS merchant_integration_supply_import_batches;
