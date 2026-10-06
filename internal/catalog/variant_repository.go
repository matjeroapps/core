package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"core/modules/commerce"
)

type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type DBPool interface {
	DBExecutor
	Begin(ctx context.Context) (pgx.Tx, error)
}

type VariantAttributeValueDetail struct {
	VariantID          string `json:"variant_id"`
	AttributeID        string `json:"attribute_id"`
	AttributeName      string `json:"attribute_name"`
	AttributeValueID   string `json:"attribute_value_id"`
	AttributeValueName string `json:"attribute_value_name"`
}

type VariantRepository struct {
	pool DBPool
}

func NewVariantRepository(pool DBPool) *VariantRepository {
	return &VariantRepository{pool: pool}
}

func (r *VariantRepository) AttachVariantAttributeValue(ctx context.Context, variantID, attributeID, attributeValueID string) error {
	query := `
		INSERT INTO variant_attribute_values (variant_id, attribute_id, attribute_value_id, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (variant_id, attribute_id)
		DO UPDATE SET attribute_value_id = EXCLUDED.attribute_value_id, created_at = now()
	`
	_, err := r.pool.Exec(ctx, query, variantID, attributeID, attributeValueID)
	if err != nil {
		return fmt.Errorf("attach variant attribute value: %w", err)
	}
	return nil
}

func (r *VariantRepository) ListVariantAttributeValues(ctx context.Context, variantID string) ([]VariantAttributeValueDetail, error) {
	query := `
		SELECT 
			vav.variant_id,
			vav.attribute_id,
			COALESCE(at.name, a.code) as attribute_name,
			vav.attribute_value_id,
			COALESCE(avt.name, av.code) as attribute_value_name
		FROM variant_attribute_values vav
		JOIN attributes a ON a.id = vav.attribute_id
		LEFT JOIN attribute_translations at ON at.attribute_id = a.id AND at.locale = 'en'
		JOIN attribute_values av ON av.id = vav.attribute_value_id
		LEFT JOIN attribute_value_translations avt ON avt.attribute_value_id = av.id AND avt.locale = 'en'
		WHERE vav.variant_id = $1
		ORDER BY a.code ASC
	`
	rows, err := r.pool.Query(ctx, query, variantID)
	if err != nil {
		return nil, fmt.Errorf("query variant attribute values: %w", err)
	}
	defer rows.Close()

	var details []VariantAttributeValueDetail
	for rows.Next() {
		var d VariantAttributeValueDetail
		if err := rows.Scan(&d.VariantID, &d.AttributeID, &d.AttributeName, &d.AttributeValueID, &d.AttributeValueName); err != nil {
			return nil, fmt.Errorf("scan variant attribute value: %w", err)
		}
		details = append(details, d)
	}
	return details, nil
}

func (r *VariantRepository) UpdateSKUPhysicalSpecs(
	ctx context.Context,
	skuID string,
	barcode *string,
	weightGrams, lengthMM, widthMM, heightMM *int,
	priceMinorUnits *int64,
) error {
	query := `
		UPDATE skus
		SET 
			barcode = COALESCE($2, barcode),
			weight_grams = COALESCE($3, weight_grams),
			length_mm = COALESCE($4, length_mm),
			width_mm = COALESCE($5, width_mm),
			height_mm = COALESCE($6, height_mm),
			price_minor_units = COALESCE($7, price_minor_units),
			updated_at = now()
		WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, query, skuID, barcode, weightGrams, lengthMM, widthMM, heightMM, priceMinorUnits)
	if err != nil {
		return fmt.Errorf("update sku physical specs: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("sku not found")
	}
	return nil
}

func (r *VariantRepository) GetSKU(ctx context.Context, skuID string) (*commerce.SKU, error) {
	query := `
		SELECT 
			id, variant_id, code, barcode, status,
			weight_grams, length_mm, width_mm, height_mm, price_minor_units,
			created_at, updated_at
		FROM skus
		WHERE id = $1
	`
	var s commerce.SKU
	var bc sql.NullString
	var wg, l, w, h sql.NullInt32
	var pmu sql.NullInt64
	err := r.pool.QueryRow(ctx, query, skuID).Scan(
		&s.ID, &s.VariantID, &s.Code, &bc, &s.Status,
		&wg, &l, &w, &h, &pmu,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("sku not found")
		}
		return nil, fmt.Errorf("scan sku: %w", err)
	}
	if bc.Valid {
		s.Barcode = bc.String
	}
	if wg.Valid {
		val := int(wg.Int32)
		s.WeightGrams = &val
	}
	if l.Valid {
		val := int(l.Int32)
		s.LengthMM = &val
	}
	if w.Valid {
		val := int(w.Int32)
		s.WidthMM = &val
	}
	if h.Valid {
		val := int(h.Int32)
		s.HeightMM = &val
	}
	if pmu.Valid {
		val := pmu.Int64
		s.PriceMinorUnits = &val
	}
	return &s, nil
}
