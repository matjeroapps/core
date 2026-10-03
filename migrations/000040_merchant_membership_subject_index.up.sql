-- Subject-oriented merchant workspace resolution (Feature 025) resolves
-- memberships by principal_subject alone; the unique constraint on
-- (merchant_id, principal_subject) cannot serve that lookup.
CREATE INDEX IF NOT EXISTS idx_merchant_memberships_principal_subject
    ON merchant_memberships(principal_subject);
