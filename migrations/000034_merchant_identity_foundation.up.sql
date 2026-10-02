CREATE TABLE IF NOT EXISTS merchants (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    legal_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS merchant_capabilities (
    id UUID PRIMARY KEY,
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    capability_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'inactive',
    activated_at TIMESTAMPTZ,
    suspended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_merchant_capabilities UNIQUE (merchant_id, capability_type)
);

CREATE TABLE IF NOT EXISTS merchant_memberships (
    id UUID PRIMARY KEY,
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    principal_subject TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_merchant_memberships UNIQUE (merchant_id, principal_subject)
);

CREATE TABLE IF NOT EXISTS merchant_membership_permissions (
    id UUID PRIMARY KEY,
    membership_id UUID NOT NULL REFERENCES merchant_memberships(id) ON DELETE CASCADE,
    permission_code TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uk_membership_permissions UNIQUE (membership_id, permission_code)
);

CREATE TABLE IF NOT EXISTS merchant_migration_crosswalk (
    id UUID PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_seller_id UUID REFERENCES sellers(id) ON DELETE SET NULL,
    source_supplier_id UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    migrated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS merchant_migration_quarantine (
    id UUID PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_seller_id UUID REFERENCES sellers(id) ON DELETE SET NULL,
    source_supplier_id UUID REFERENCES suppliers(id) ON DELETE SET NULL,
    reason_code TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
