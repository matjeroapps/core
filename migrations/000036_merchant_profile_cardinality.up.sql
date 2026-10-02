INSERT INTO merchant_migration_quarantine (id, source_type, source_seller_id, source_supplier_id, reason_code, details)
SELECT
    gen_random_uuid(),
    'SELLER_CARDINALITY',
    NULL,
    NULL,
    'MERCHANT_MULTIPLE_ACTIVE_SELLERS',
    jsonb_build_object(
        'merchant_id', merchant_id,
        'seller_ids', jsonb_agg(id ORDER BY id),
        'conflict_count', count(*)
    )
FROM sellers
WHERE merchant_id IS NOT NULL
  AND status = 'active'
GROUP BY merchant_id
HAVING count(*) > 1
ON CONFLICT DO NOTHING;

INSERT INTO merchant_migration_quarantine (id, source_type, source_seller_id, source_supplier_id, reason_code, details)
SELECT
    gen_random_uuid(),
    'SUPPLIER_CARDINALITY',
    NULL,
    NULL,
    'MERCHANT_MULTIPLE_ACTIVE_SUPPLIERS',
    jsonb_build_object(
        'merchant_id', merchant_id,
        'supplier_ids', jsonb_agg(id ORDER BY id),
        'conflict_count', count(*)
    )
FROM suppliers
WHERE merchant_id IS NOT NULL
  AND status = 'active'
GROUP BY merchant_id
HAVING count(*) > 1
ON CONFLICT DO NOTHING;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM sellers
        WHERE merchant_id IS NOT NULL
          AND status = 'active'
        GROUP BY merchant_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'merchant profile cardinality preflight failed: multiple active sellers linked to one merchant; conflicts recorded in merchant_migration_quarantine';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM suppliers
        WHERE merchant_id IS NOT NULL
          AND status = 'active'
        GROUP BY merchant_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'merchant profile cardinality preflight failed: multiple active suppliers linked to one merchant; conflicts recorded in merchant_migration_quarantine';
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uk_sellers_one_active_profile_per_merchant
    ON sellers (merchant_id)
    WHERE merchant_id IS NOT NULL
      AND status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS uk_suppliers_one_active_profile_per_merchant
    ON suppliers (merchant_id)
    WHERE merchant_id IS NOT NULL
      AND status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS uk_merchant_crosswalk_seller_source
    ON merchant_migration_crosswalk (source_seller_id)
    WHERE source_seller_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uk_merchant_crosswalk_supplier_source
    ON merchant_migration_crosswalk (source_supplier_id)
    WHERE source_supplier_id IS NOT NULL;
