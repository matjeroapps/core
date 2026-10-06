package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Repository methods for store-scoped categories. These deliberately never
// touch the platform-global categories tables: seller-facing category data
// lives only in store_categories / store_category_translations /
// store_product_categories, so tenant isolation is structural.

// storeCategoryAncestorDepthLimit bounds the recursive ancestor walk. Well
// formed hierarchies are acyclic (the service rejects cycles), so this only
// guards against unbounded recursion on corrupted data.
const storeCategoryAncestorDepthLimit = 100

const storeCategoryColumns = `id, store_id, parent_category_id, slug, status, sort_order, created_at, updated_at`

const storeCategoryCountsSubquery = `,
	(SELECT count(*) FROM store_product_categories spc WHERE spc.store_category_id = c.id) AS product_count,
	(SELECT count(*) FROM store_categories ch WHERE ch.parent_category_id = c.id) AS child_count`

func scanStoreCategoryWithMeta(row pgx.Row) (StoreCategoryWithMeta, error) {
	var item StoreCategoryWithMeta
	err := row.Scan(&item.ID, &item.StoreID, &item.ParentCategoryID, &item.Slug, &item.Status,
		&item.SortOrder, &item.CreatedAt, &item.UpdatedAt, &item.ProductCount, &item.ChildCount)
	if err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return item, nil
}

func (r Repository) CreateStoreCategory(ctx context.Context, storeID string, slug string, parentCategoryID *string, sortOrder int, translations []StoreCategoryTranslation) (StoreCategoryWithMeta, error) {
	if storeID == "" || slug == "" {
		return StoreCategoryWithMeta{}, ErrInvalidInput
	}

	created := StoreCategoryWithMeta{}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		query := `
			INSERT INTO store_categories (id, store_id, parent_category_id, slug, status, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING ` + storeCategoryColumns
		row := tx.QueryRow(ctx, query, id, storeID, parentCategoryID, slug, StoreCategoryStatusActive, sortOrder)
		if err := row.Scan(&created.ID, &created.StoreID, &created.ParentCategoryID, &created.Slug,
			&created.Status, &created.SortOrder, &created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create store category")
		}

		for _, tr := range translations {
			if _, err := tx.Exec(ctx, `
				INSERT INTO store_category_translations (category_id, locale, name, description)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (category_id, locale)
				DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
			`, id, tr.Locale, tr.Name, tr.Description); err != nil {
				return translatePGError(err, "upsert store category translation")
			}
		}
		created.Translations = append([]StoreCategoryTranslation(nil), translations...)
		return nil
	})
	if err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return created, nil
}

func (r Repository) GetStoreCategory(ctx context.Context, storeID, categoryID string) (StoreCategoryWithMeta, error) {
	if storeID == "" || categoryID == "" {
		return StoreCategoryWithMeta{}, ErrInvalidInput
	}

	query := `
		SELECT c.id, c.store_id, c.parent_category_id, c.slug, c.status, c.sort_order, c.created_at, c.updated_at` +
		storeCategoryCountsSubquery + `
		FROM store_categories c
		WHERE c.store_id = $1 AND c.id = $2
	`
	row := r.pool.QueryRow(ctx, query, storeID, categoryID)
	item, err := scanStoreCategoryWithMeta(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoreCategoryWithMeta{}, ErrNotFound
		}
		return StoreCategoryWithMeta{}, fmt.Errorf("get store category: %w", err)
	}

	translations, err := r.getStoreCategoryTranslations(ctx, []string{item.ID})
	if err != nil {
		return StoreCategoryWithMeta{}, err
	}
	item.Translations = translations[item.ID]
	return item, nil
}

func (r Repository) ListStoreCategories(ctx context.Context, storeID, statusFilter string, limit, offset int) ([]StoreCategoryWithMeta, int, error) {
	if storeID == "" {
		return nil, 0, ErrInvalidInput
	}

	whereClause := "WHERE c.store_id = $1"
	args := []any{storeID}
	if statusFilter != "" {
		if !IsValidStoreCategoryStatus(statusFilter) {
			return nil, 0, fmt.Errorf("%w: unknown status filter %q", ErrInvalidInput, statusFilter)
		}
		whereClause += " AND c.status = $2"
		args = append(args, statusFilter)
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM store_categories c `+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count store categories: %w", err)
	}

	args = append(args, limit, offset)
	query := `
		SELECT c.id, c.store_id, c.parent_category_id, c.slug, c.status, c.sort_order, c.created_at, c.updated_at` +
		storeCategoryCountsSubquery + `
		FROM store_categories c
		` + whereClause + `
		ORDER BY c.sort_order, c.created_at, c.id
		LIMIT $` + fmt.Sprintf("%d", len(args)-1) + ` OFFSET $` + fmt.Sprintf("%d", len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list store categories: %w", err)
	}
	defer rows.Close()

	items := make([]StoreCategoryWithMeta, 0, total)
	ids := make([]string, 0, total)
	for rows.Next() {
		item, err := scanStoreCategoryWithMeta(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan store category: %w", err)
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate store categories: %w", err)
	}

	translations, err := r.getStoreCategoryTranslations(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Translations = translations[items[i].ID]
	}
	return items, total, nil
}

func (r Repository) getStoreCategoryTranslations(ctx context.Context, categoryIDs []string) (map[string][]StoreCategoryTranslation, error) {
	out := make(map[string][]StoreCategoryTranslation, len(categoryIDs))
	if len(categoryIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT category_id, locale, name, description
		FROM store_category_translations
		WHERE category_id = ANY($1)
		ORDER BY locale
	`, categoryIDs)
	if err != nil {
		return nil, fmt.Errorf("list store category translations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tr StoreCategoryTranslation
		if err := rows.Scan(&tr.CategoryID, &tr.Locale, &tr.Name, &tr.Description); err != nil {
			return nil, fmt.Errorf("scan store category translation: %w", err)
		}
		out[tr.CategoryID] = append(out[tr.CategoryID], tr)
	}
	return out, rows.Err()
}

func (r Repository) UpdateStoreCategory(ctx context.Context, storeID, categoryID string, patch StoreCategoryPatch) (StoreCategoryWithMeta, error) {
	if storeID == "" || categoryID == "" {
		return StoreCategoryWithMeta{}, ErrInvalidInput
	}

	var updated StoreCategoryWithMeta
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		sets := []string{"updated_at = now()"}
		args := []any{}
		addSet := func(expr string, value any) {
			args = append(args, value)
			sets = append(sets, fmt.Sprintf("%s = $%d", expr, len(args)))
		}

		if patch.Slug != nil {
			addSet("slug", *patch.Slug)
		}
		if patch.ClearParent {
			addSet("parent_category_id", nil)
		} else if patch.ParentCategoryID != nil {
			addSet("parent_category_id", *patch.ParentCategoryID)
		}
		if patch.SortOrder != nil {
			addSet("sort_order", *patch.SortOrder)
		}
		args = append(args, storeID, categoryID)

		query := `
			UPDATE store_categories SET ` + strings.Join(sets, ", ") + `
			WHERE store_id = $` + fmt.Sprintf("%d", len(args)-1) + ` AND id = $` + fmt.Sprintf("%d", len(args)) + `
			RETURNING ` + storeCategoryColumns
		row := tx.QueryRow(ctx, query, args...)
		if err := row.Scan(&updated.ID, &updated.StoreID, &updated.ParentCategoryID, &updated.Slug,
			&updated.Status, &updated.SortOrder, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update store category")
		}

		if patch.Translations != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM store_category_translations WHERE category_id = $1`, categoryID); err != nil {
				return translatePGError(err, "clear store category translations")
			}
			for _, tr := range patch.Translations {
				if _, err := tx.Exec(ctx, `
					INSERT INTO store_category_translations (category_id, locale, name, description)
					VALUES ($1, $2, $3, $4)
				`, categoryID, tr.Locale, tr.Name, tr.Description); err != nil {
					return translatePGError(err, "insert store category translation")
				}
			}
			updated.Translations = append([]StoreCategoryTranslation(nil), patch.Translations...)
		}
		return nil
	})
	if err != nil {
		return StoreCategoryWithMeta{}, err
	}
	return updated, nil
}

func (r Repository) UpdateStoreCategoryStatus(ctx context.Context, storeID, categoryID, status string) (StoreCategoryWithMeta, error) {
	if storeID == "" || categoryID == "" || status == "" {
		return StoreCategoryWithMeta{}, ErrInvalidInput
	}

	query := `
		UPDATE store_categories SET status = $3, updated_at = now()
		WHERE store_id = $1 AND id = $2
		RETURNING ` + storeCategoryColumns
	row := r.pool.QueryRow(ctx, query, storeID, categoryID, status)
	var item StoreCategoryWithMeta
	if err := row.Scan(&item.ID, &item.StoreID, &item.ParentCategoryID, &item.Slug,
		&item.Status, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoreCategoryWithMeta{}, ErrNotFound
		}
		return StoreCategoryWithMeta{}, translatePGError(err, "update store category status")
	}
	return item, nil
}

func (r Repository) DeleteStoreCategory(ctx context.Context, storeID, categoryID string) error {
	if storeID == "" || categoryID == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var productCount int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM store_product_categories WHERE store_category_id = $1
		`, categoryID).Scan(&productCount); err != nil {
			return translatePGError(err, "count store category assignments")
		}
		if productCount > 0 {
			return fmt.Errorf("%w: category has %d product assignment(s); archive it instead", ErrInvalidInput, productCount)
		}
		var childCount int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM store_categories WHERE parent_category_id = $1
		`, categoryID).Scan(&childCount); err != nil {
			return translatePGError(err, "count store category children")
		}
		if childCount > 0 {
			return fmt.Errorf("%w: category has %d child categorie(s); archive it instead", ErrInvalidInput, childCount)
		}

		tag, err := tx.Exec(ctx, `DELETE FROM store_categories WHERE store_id = $1 AND id = $2`, storeID, categoryID)
		if err != nil {
			return translatePGError(err, "delete store category")
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r Repository) ReorderStoreCategories(ctx context.Context, storeID string, order []StoreCategoryOrder) error {
	if storeID == "" || len(order) == 0 {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ids := make([]string, len(order))
		for i, o := range order {
			if o.ID == "" {
				return ErrInvalidInput
			}
			ids[i] = o.ID
		}
		var matching int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM store_categories WHERE store_id = $1 AND id = ANY($2)
		`, storeID, ids).Scan(&matching); err != nil {
			return translatePGError(err, "validate store category reorder ids")
		}
		if matching != len(order) {
			return ErrNotFound
		}
		for _, o := range order {
			if _, err := tx.Exec(ctx, `
				UPDATE store_categories SET sort_order = $3, updated_at = now()
				WHERE store_id = $1 AND id = $2
			`, storeID, o.ID, o.SortOrder); err != nil {
				return translatePGError(err, "reorder store category")
			}
		}
		return nil
	})
}

// StoreCategoriesBelongToStore reports whether every id resolves to a category
// of the given store. Foreign and unknown ids are indistinguishable.
func (r Repository) StoreCategoriesBelongToStore(ctx context.Context, storeID string, categoryIDs []string) (bool, error) {
	if len(categoryIDs) == 0 {
		return true, nil
	}
	var matching int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM store_categories WHERE store_id = $1 AND id = ANY($2)
	`, storeID, categoryIDs).Scan(&matching)
	if err != nil {
		return false, fmt.Errorf("validate store categories for store: %w", err)
	}
	return matching == len(categoryIDs), nil
}

// SetStoreProductCategories replaces a product's store-category assignments.
// Same-store validity is enforced by the service in the same transaction.
func (r Repository) SetStoreProductCategories(ctx context.Context, productID, storeID string, categoryIDs []string) error {
	if productID == "" || storeID == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM store_product_categories WHERE product_id = $1`, productID); err != nil {
			return translatePGError(err, "clear store product categories")
		}
		for _, categoryID := range categoryIDs {
			if categoryID == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO store_product_categories (product_id, store_category_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, productID, categoryID); err != nil {
				return translatePGError(err, "insert store product category")
			}
		}
		return nil
	})
}

// GetStoreProductCategoryRefs returns readable references for every store
// category assigned to a product. The English name is always present; NameAr
// is set when the Arabic translation exists.
func (r Repository) GetStoreProductCategoryRefs(ctx context.Context, productID string) ([]StoreCategoryRef, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT sc.id, sc.slug, sc.status, sc.sort_order,
		       COALESCE(en.name, ''), COALESCE(ar.name, '')
		FROM store_product_categories spc
		JOIN store_categories sc ON sc.id = spc.store_category_id
		LEFT JOIN store_category_translations en ON en.category_id = sc.id AND en.locale = 'en'
		LEFT JOIN store_category_translations ar ON ar.category_id = sc.id AND ar.locale = 'ar'
		WHERE spc.product_id = $1
		ORDER BY spc.sort_order, sc.created_at, sc.id
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("list store product category refs: %w", err)
	}
	defer rows.Close()

	refs := make([]StoreCategoryRef, 0)
	for rows.Next() {
		var ref StoreCategoryRef
		var sortOrder int
		if err := rows.Scan(&ref.ID, &ref.Slug, &ref.Status, &sortOrder, &ref.Name, &ref.NameAr); err != nil {
			return nil, fmt.Errorf("scan store product category ref: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// GetStoreCategoryAncestorIDs returns the category itself plus every
// transitive parent id, bounded by storeCategoryAncestorDepthLimit. Used by the
// service to reject circular reparenting.
func (r Repository) GetStoreCategoryAncestorIDs(ctx context.Context, storeID, categoryID string) ([]string, error) {
	if storeID == "" || categoryID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id, parent_category_id, 1 AS depth
			FROM store_categories
			WHERE store_id = $1 AND id = $2
			UNION ALL
			SELECT c.id, c.parent_category_id, a.depth + 1
			FROM store_categories c
			JOIN ancestors a ON c.id = a.parent_category_id
			WHERE a.depth < $3
		)
		SELECT id FROM ancestors
	`, storeID, categoryID, storeCategoryAncestorDepthLimit)
	if err != nil {
		return nil, fmt.Errorf("walk store category ancestors: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0, 8)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan store category ancestor: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
