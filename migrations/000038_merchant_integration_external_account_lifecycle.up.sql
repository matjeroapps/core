DO $$
DECLARE
    invalid_connection_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO invalid_connection_count
    FROM merchant_integration_connections
    WHERE NULLIF(BTRIM(external_account_id), '') IS NULL
      AND status NOT IN ('DRAFT', 'AUTHORIZING', 'CONFIGURING');

    IF invalid_connection_count > 0 THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = format(
                'cannot enforce merchant integration external-account lifecycle: %s invalid connection(s)',
                invalid_connection_count
            );
    END IF;
END $$;

ALTER TABLE merchant_integration_connections
    ADD CONSTRAINT ck_merchant_integration_conn_operational_account
    CHECK (
        NULLIF(BTRIM(external_account_id), '') IS NOT NULL
        OR status IN ('DRAFT', 'AUTHORIZING', 'CONFIGURING')
    );
