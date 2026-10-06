-- Migration 000044: Store-scoped categories
--
-- Seller-managed category trees, isolated per store. Structurally separate from
-- the platform-global `categories`/`category_translations` tables, which remain
-- untouched for admin and storefront compatibility.
--
-- Integrity:
--   - UNIQUE (store_id, slug): slugs are unique within a store, reusable across stores.
--   - UNIQUE (store_id, id) backs the composite self-FK so a parent category can
--     never belong to a different store than its child.
--   - ON DELETE RESTRICT on the self-FK blocks deleting a parent with children.
--   - Circular hierarchies are rejected by the commerce service (ancestor walk);
--     PostgreSQL constraints cannot express acyclicity.

CREATE TABLE IF NOT EXISTS store_categories (
    id UUID PRIMARY KEY,
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    parent_category_id UUID,
    slug TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT store_categories_status_check CHECK (status IN ('active', 'inactive', 'archived')),
    CONSTRAINT store_categories_slug_per_store_uidx UNIQUE (store_id, slug),
    CONSTRAINT store_categories_store_id_id_uidx UNIQUE (store_id, id),
    CONSTRAINT store_categories_parent_same_store_fk FOREIGN KEY (store_id, parent_category_id)
        REFERENCES store_categories (store_id, id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS store_category_translations (
    category_id UUID NOT NULL REFERENCES store_categories(id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (category_id, locale)
);

CREATE TABLE IF NOT EXISTS store_product_categories (
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    store_category_id UUID NOT NULL REFERENCES store_categories(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (product_id, store_category_id)
);

CREATE INDEX IF NOT EXISTS idx_store_categories_store_parent
    ON store_categories (store_id, parent_category_id);
CREATE INDEX IF NOT EXISTS idx_store_categories_store_status
    ON store_categories (store_id, status);
CREATE INDEX IF NOT EXISTS idx_store_product_categories_category
    ON store_product_categories (store_category_id);
