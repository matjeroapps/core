package commerce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/packages/money"
)

type Repository struct {
	pool                      *pgxpool.Pool
	OrderConfirmationDuration time.Duration
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{pool: pool}
}

func (r Repository) Pool() *pgxpool.Pool {
	return r.pool
}

func (r Repository) withTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func (r Repository) CreateSupplier(ctx context.Context, code, name, status string, settings map[string]any) (Supplier, error) {
	if code == "" || name == "" || status == "" {
		return Supplier{}, ErrInvalidInput
	}

	var created Supplier
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO suppliers (id, code, name, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, code, name, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier")
		}

		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO supplier_settings (supplier_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (supplier_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, id, settings); err != nil {
			return err
		}

		created = Supplier{
			ID:        id,
			Code:      code,
			Name:      name,
			Status:    status,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) CreateSupplierMarket(ctx context.Context, supplierID, marketCode, status string, settings map[string]any) (SupplierMarket, error) {
	if supplierID == "" || marketCode == "" || status == "" {
		return SupplierMarket{}, ErrInvalidInput
	}

	var created SupplierMarket
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_markets (id, supplier_id, market_code, status, settings)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, supplierID, marketCode, status, jsonOrEmpty(settings)).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier market")
		}

		created = SupplierMarket{
			ID:         id,
			SupplierID: supplierID,
			MarketCode: marketCode,
			Status:     status,
			Settings:   normalizeSettings(settings),
			CreatedAt:  created.CreatedAt,
			UpdatedAt:  created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) CreateSupplierMember(ctx context.Context, supplierID, principalSubject, role, status string) (SupplierMember, error) {
	if supplierID == "" || principalSubject == "" || role == "" || status == "" {
		return SupplierMember{}, ErrInvalidInput
	}

	var created SupplierMember
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_members (id, supplier_id, principal_subject, role, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, supplierID, principalSubject, role, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier member")
		}
		created = SupplierMember{ID: id, SupplierID: supplierID, PrincipalSubject: principalSubject, Role: role, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) CreateSeller(ctx context.Context, code, name, status string, settings map[string]any) (Seller, error) {
	if code == "" || name == "" || status == "" {
		return Seller{}, ErrInvalidInput
	}

	var created Seller
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO sellers (id, code, name, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, code, name, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create seller")
		}

		if err := upsertJSONSettings(ctx, tx, `
			INSERT INTO seller_settings (seller_id, settings)
			VALUES ($1, $2)
			ON CONFLICT (seller_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
		`, id, settings); err != nil {
			return err
		}

		created = Seller{
			ID:        id,
			Code:      code,
			Name:      name,
			Status:    status,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) CreateSellerMember(ctx context.Context, sellerID, principalSubject, role, status string) (SellerMember, error) {
	if sellerID == "" || principalSubject == "" || role == "" || status == "" {
		return SellerMember{}, ErrInvalidInput
	}

	var created SellerMember
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_members (id, seller_id, principal_subject, role, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, sellerID, principalSubject, role, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create seller member")
		}
		created = SellerMember{ID: id, SellerID: sellerID, PrincipalSubject: principalSubject, Role: role, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) CreateStore(ctx context.Context, sellerID, marketCode, code, name, status string, settings map[string]any, maxActiveStores ...int) (Store, error) {
	if sellerID == "" || marketCode == "" || code == "" || name == "" {
		return Store{}, ErrInvalidInput
	}
	maxActive := 1
	if len(maxActiveStores) > 0 && maxActiveStores[0] > 0 {
		maxActive = maxActiveStores[0]
	}

	var created Store
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		s, err := createStoreInTx(ctx, tx, sellerID, marketCode, code, name, status, settings, maxActive)
		if err != nil {
			return err
		}
		created = s
		return nil
	})
	return created, err
}

// createStoreInTx inserts a store row and its settings within the provided
// transaction. It is the single store-creation primitive reused by both
// CreateStore and the atomic CreateStoreWithDomain.
func createStoreInTx(ctx context.Context, tx pgx.Tx, sellerID, marketCode, code, name, status string, settings map[string]any, maxActiveStores int) (Store, error) {
	if status == "" {
		status = "draft"
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "draft" && status != "active" && status != "inactive" {
		return Store{}, fmt.Errorf("%w: invalid store status %q", ErrInvalidInput, status)
	}

	if status == "active" {
		if maxActiveStores <= 0 {
			maxActiveStores = 1
		}
		var sellerDummy string
		if err := tx.QueryRow(ctx, `SELECT id FROM sellers WHERE id = $1 FOR UPDATE`, sellerID).Scan(&sellerDummy); err != nil {
			return Store{}, translatePGError(err, "lock seller for store entitlement")
		}
		var activeCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stores WHERE seller_id = $1 AND status = 'active'`, sellerID).Scan(&activeCount); err != nil {
			return Store{}, translatePGError(err, "count active stores")
		}
		if activeCount >= maxActiveStores {
			return Store{}, ErrStoreEntitlementExceeded
		}
	}

	id := uuid.NewString()
	var created Store
	if err := tx.QueryRow(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`, id, sellerID, marketCode, code, name, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
		return Store{}, translatePGError(err, "create store")
	}

	if err := upsertJSONSettings(ctx, tx, `
		INSERT INTO store_settings (store_id, settings)
		VALUES ($1, $2)
		ON CONFLICT (store_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
	`, id, settings); err != nil {
		return Store{}, err
	}

	if err := initStorefrontRevision(ctx, tx, id); err != nil {
		return Store{}, err
	}

	created.ID = id
	created.SellerID = sellerID
	created.MarketCode = marketCode
	created.Code = code
	created.Name = name
	created.Status = status
	return created, nil
}

// CreateStoreDomain persists a store domain using the shared canonicalization
// routine and returns the fully populated persisted record (including database
// defaults such as domain_type and lifecycle timestamps).
func (r Repository) CreateStoreDomain(ctx context.Context, storeID, domain, domainType, status string, isPrimary bool, verifiedAt *time.Time, verificationToken *string) (StoreDomain, error) {
	normalized, err := NormalizeDomain(domain)
	if err != nil {
		return StoreDomain{}, ErrInvalidInput
	}
	if storeID == "" || status == "" {
		return StoreDomain{}, ErrInvalidInput
	}

	var created StoreDomain
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := createStoreDomainInTx(ctx, tx, storeID, normalized, domainType, status, isPrimary, verifiedAt, verificationToken)
		if err != nil {
			return err
		}
		created = d
		return nil
	})
	return created, err
}

// createStoreDomainInTx inserts a store_domains row within the provided
// transaction and scans the complete persisted row (all lifecycle columns).
func createStoreDomainInTx(ctx context.Context, tx pgx.Tx, storeID, domain, domainType, status string, isPrimary bool, verifiedAt *time.Time, verificationToken *string) (StoreDomain, error) {
	id := uuid.NewString()
	var d StoreDomain
	if err := tx.QueryRow(ctx, `
		INSERT INTO store_domains (id, store_id, domain, is_primary, verified_at, status, domain_type, verification_token, last_checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL)
		RETURNING id, store_id, domain, is_primary, verified_at, status, domain_type, verification_token, last_checked_at, created_at, updated_at
	`, id, storeID, domain, isPrimary, verifiedAt, status, domainType, verificationToken).Scan(
		&d.ID, &d.StoreID, &d.Domain, &d.IsPrimary, &d.VerifiedAt, &d.Status, &d.DomainType, &d.VerificationToken, &d.LastCheckedAt, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return StoreDomain{}, translatePGError(err, "create store domain")
	}
	return d, nil
}

// CreateStoreWithDomain atomically creates a store, its settings, and its primary
// platform domain within a single PostgreSQL transaction. If any step fails the
// entire operation rolls back, leaving no partial Store, StoreSettings, or
// StoreDomain state. This is the cohesive transaction boundary used when a store
// is created together with its platform-generated subdomain.
func (r Repository) CreateStoreWithDomain(ctx context.Context, sellerID, marketCode, code, name, status string, settings map[string]any, domain, domainType, domainStatus string, isPrimary bool, verifiedAt *time.Time, verificationToken *string, maxActiveStores ...int) (Store, StoreDomain, error) {
	if sellerID == "" || marketCode == "" || code == "" || name == "" || domain == "" || domainStatus == "" {
		return Store{}, StoreDomain{}, ErrInvalidInput
	}
	normalizedDomain, err := NormalizeDomain(domain)
	if err != nil {
		return Store{}, StoreDomain{}, ErrInvalidInput
	}
	maxActive := 1
	if len(maxActiveStores) > 0 && maxActiveStores[0] > 0 {
		maxActive = maxActiveStores[0]
	}

	var store Store
	var storeDomain StoreDomain
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		s, err := createStoreInTx(ctx, tx, sellerID, marketCode, code, name, status, settings, maxActive)
		if err != nil {
			return err
		}
		store = s

		d, err := createStoreDomainInTx(ctx, tx, store.ID, normalizedDomain, domainType, domainStatus, isPrimary, verifiedAt, verificationToken)
		if err != nil {
			return err
		}
		storeDomain = d
		return nil
	})
	return store, storeDomain, err
}

// CreateCustomStoreDomain persists a seller-supplied custom domain in the
// PENDING lifecycle state with a cryptographically secure verification token.
// The domain is canonicalized before persistence. Ownership verification
// (promoting PENDING -> VERIFIED -> ACTIVE) is handled by later lifecycle steps.
func (r Repository) CreateCustomStoreDomain(ctx context.Context, storeID, domain string) (StoreDomain, error) {
	if storeID == "" || domain == "" {
		return StoreDomain{}, ErrInvalidInput
	}
	normalized, err := NormalizeDomain(domain)
	if err != nil {
		return StoreDomain{}, ErrInvalidInput
	}
	token, err := generateVerificationToken()
	if err != nil {
		return StoreDomain{}, fmt.Errorf("generate verification token: %w", err)
	}

	var created StoreDomain
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		d, err := createStoreDomainInTx(ctx, tx, storeID, normalized, "custom", "pending", false, nil, &token)
		if err != nil {
			return err
		}
		created = d
		return nil
	})
	return created, err
}

// generateVerificationToken returns a cryptographically secure, URL-safe token
// suitable for DNS ownership verification. It is not derived from any predictable
// identifier.
func generateVerificationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r Repository) CreateProduct(ctx context.Context, slug, status string) (Product, error) {
	if slug == "" || status == "" {
		return Product{}, ErrInvalidInput
	}
	if status == "inactive" {
		status = "draft"
	}

	var created Product
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO products (id, slug, status)
			VALUES ($1, $2, $3)
			RETURNING created_at, updated_at
		`, id, slug, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create product")
		}

		created = Product{
			ID:        id,
			Slug:      slug,
			Status:    status,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) UpsertProductTranslation(ctx context.Context, translation ProductTranslation) error {
	if translation.ProductID == "" || translation.Locale == "" || translation.Name == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_translations (product_id, locale, name, description)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (product_id, locale)
			DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
		`, translation.ProductID, translation.Locale, translation.Name, translation.Description); err != nil {
			return translatePGError(err, "upsert product translation")
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, translation.ProductID)
	})
}

func (r Repository) CreateCategory(ctx context.Context, slug string, parentCategoryID *string, status string) (Category, error) {
	if slug == "" || status == "" {
		return Category{}, ErrInvalidInput
	}

	var created Category
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (id, parent_category_id, slug, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, parentCategoryID, slug, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create category")
		}

		created = Category{
			ID:               id,
			ParentCategoryID: parentCategoryID,
			Slug:             slug,
			Status:           status,
			CreatedAt:        created.CreatedAt,
			UpdatedAt:        created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) UpsertCategoryTranslation(ctx context.Context, translation CategoryTranslation) error {
	if translation.CategoryID == "" || translation.Locale == "" || translation.Name == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO category_translations (category_id, locale, name, description)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (category_id, locale)
			DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
		`, translation.CategoryID, translation.Locale, translation.Name, translation.Description); err != nil {
			return translatePGError(err, "upsert category translation")
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByCategorySubtree, translation.CategoryID)
	})
}

func (r Repository) CreateVariant(ctx context.Context, productID, code, status string) (Variant, error) {
	if productID == "" || code == "" || status == "" {
		return Variant{}, ErrInvalidInput
	}

	var created Variant
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO variants (id, product_id, code, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, productID, code, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create variant")
		}

		created = Variant{ID: id, ProductID: productID, Code: code, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, productID)
	})
	return created, err
}

func (r Repository) CreateSKU(ctx context.Context, variantID, code, barcode, status string) (SKU, error) {
	if variantID == "" || code == "" || status == "" {
		return SKU{}, ErrInvalidInput
	}

	var created SKU
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO skus (id, variant_id, code, barcode, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, variantID, code, nullString(barcode), status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create sku")
		}

		created = SKU{ID: id, VariantID: variantID, Code: code, Barcode: barcode, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByVariant, variantID)
	})
	return created, err
}

func (r Repository) CreateAttribute(ctx context.Context, code, status string) (Attribute, error) {
	if code == "" || status == "" {
		return Attribute{}, ErrInvalidInput
	}

	var created Attribute
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO attributes (id, code, status)
			VALUES ($1, $2, $3)
			RETURNING created_at, updated_at
		`, id, code, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create attribute")
		}
		created = Attribute{ID: id, Code: code, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) UpsertAttributeTranslation(ctx context.Context, translation AttributeTranslation) error {
	if translation.AttributeID == "" || translation.Locale == "" || translation.Name == "" {
		return ErrInvalidInput
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO attribute_translations (attribute_id, locale, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (attribute_id, locale)
		DO UPDATE SET name = EXCLUDED.name
	`, translation.AttributeID, translation.Locale, translation.Name)
	return translatePGError(err, "upsert attribute translation")
}

func (r Repository) CreateAttributeValue(ctx context.Context, attributeID, code, status string) (AttributeValue, error) {
	if attributeID == "" || code == "" || status == "" {
		return AttributeValue{}, ErrInvalidInput
	}

	var created AttributeValue
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO attribute_values (id, attribute_id, code, status)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, attributeID, code, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create attribute value")
		}
		created = AttributeValue{ID: id, AttributeID: attributeID, Code: code, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) UpsertAttributeValueTranslation(ctx context.Context, translation AttributeValueTranslation) error {
	if translation.AttributeValueID == "" || translation.Locale == "" || translation.Name == "" {
		return ErrInvalidInput
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO attribute_value_translations (attribute_value_id, locale, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (attribute_value_id, locale)
		DO UPDATE SET name = EXCLUDED.name
	`, translation.AttributeValueID, translation.Locale, translation.Name)
	return translatePGError(err, "upsert attribute value translation")
}

func (r Repository) CreateSupplierProduct(ctx context.Context, supplierID, productID, supplierCode, status string) (SupplierProduct, error) {
	if supplierID == "" || productID == "" || supplierCode == "" || status == "" {
		return SupplierProduct{}, ErrInvalidInput
	}

	var created SupplierProduct
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, supplierID, productID, supplierCode, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier product")
		}
		created = SupplierProduct{ID: id, SupplierID: supplierID, ProductID: productID, SupplierCode: supplierCode, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) CreateSupplierOffer(ctx context.Context, supplierID, supplierProductID, supplierMarketID, marketCode, status string) (SupplierOffer, error) {
	if supplierID == "" || supplierProductID == "" || supplierMarketID == "" || marketCode == "" || status == "" {
		return SupplierOffer{}, ErrInvalidInput
	}

	var created SupplierOffer
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING created_at, updated_at
		`, id, supplierID, supplierProductID, supplierMarketID, marketCode, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier offer")
		}
		created = SupplierOffer{ID: id, SupplierID: supplierID, SupplierProductID: supplierProductID, SupplierMarketID: supplierMarketID, MarketCode: marketCode, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) SetSupplierOfferPrice(ctx context.Context, supplierOfferID string, price money.Money) (SupplierOfferPrice, error) {
	if supplierOfferID == "" {
		return SupplierOfferPrice{}, ErrInvalidInput
	}
	if err := price.Validate(); err != nil {
		return SupplierOfferPrice{}, err
	}

	var created SupplierOfferPrice
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (supplier_offer_id)
			DO UPDATE SET amount_minor = EXCLUDED.amount_minor, currency_code = EXCLUDED.currency_code, is_current = true, updated_at = now()
			RETURNING id, created_at, updated_at, is_current
		`, id, supplierOfferID, price.AmountMinor, price.Currency).Scan(&created.ID, &created.CreatedAt, &created.UpdatedAt, &created.IsCurrent); err != nil {
			return translatePGError(err, "set supplier offer price")
		}
		created = SupplierOfferPrice{ID: created.ID, SupplierOfferID: supplierOfferID, Price: price, IsCurrent: created.IsCurrent, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

func (r Repository) SetSupplierOfferAvailability(ctx context.Context, supplierOfferID string, isAvailable bool, availableQty *int64) (SupplierOfferAvailability, error) {
	if supplierOfferID == "" {
		return SupplierOfferAvailability{}, ErrInvalidInput
	}
	if availableQty != nil && *availableQty < 0 {
		return SupplierOfferAvailability{}, ErrInvalidInput
	}

	var created SupplierOfferAvailability
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_offer_availability (id, supplier_offer_id, is_available, available_qty)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (supplier_offer_id)
			DO UPDATE SET is_available = EXCLUDED.is_available, available_qty = EXCLUDED.available_qty, updated_at = now()
			RETURNING id, created_at, updated_at
		`, id, supplierOfferID, isAvailable, availableQty).Scan(&created.ID, &created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "set supplier offer availability")
		}
		created = SupplierOfferAvailability{ID: created.ID, SupplierOfferID: supplierOfferID, IsAvailable: isAvailable, AvailableQty: availableQty, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresBySupplierOffer, supplierOfferID)
	})
	return created, err
}

func (r Repository) CreateSellerListing(ctx context.Context, storeID, productID string, supplierOfferID *string, marketCode, status string) (SellerListing, error) {
	if storeID == "" || productID == "" || marketCode == "" || status == "" {
		return SellerListing{}, ErrInvalidInput
	}
	if status == "active" {
		status = "published"
	} else if status == "inactive" {
		status = "draft"
	}

	var created SellerListing
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING created_at, updated_at
		`, id, storeID, productID, supplierOfferID, marketCode, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create seller listing")
		}
		created = SellerListing{ID: id, StoreID: storeID, ProductID: productID, SupplierOfferID: supplierOfferID, MarketCode: marketCode, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
	return created, err
}

func (r Repository) SetSellerListingPrice(ctx context.Context, sellerListingID string, price money.Money) (SellerListingPrice, error) {
	if sellerListingID == "" {
		return SellerListingPrice{}, ErrInvalidInput
	}
	if err := price.Validate(); err != nil {
		return SellerListingPrice{}, err
	}

	var created SellerListingPrice
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current)
			VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (seller_listing_id)
			DO UPDATE SET amount_minor = EXCLUDED.amount_minor, currency_code = EXCLUDED.currency_code, is_current = true, updated_at = now()
			RETURNING id, created_at, updated_at, is_current
		`, id, sellerListingID, price.AmountMinor, price.Currency).Scan(&created.ID, &created.CreatedAt, &created.UpdatedAt, &created.IsCurrent); err != nil {
			return translatePGError(err, "set seller listing price")
		}
		created = SellerListingPrice{ID: created.ID, SellerListingID: sellerListingID, Price: price, IsCurrent: created.IsCurrent, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByListing, sellerListingID)
	})
	return created, err
}

func (r Repository) CreateFulfillmentLocation(ctx context.Context, supplierID, supplierMarketID, marketCode, code, name, locationType, status string) (FulfillmentLocation, error) {
	if supplierID == "" || supplierMarketID == "" || marketCode == "" || code == "" || name == "" || locationType == "" || status == "" {
		return FulfillmentLocation{}, ErrInvalidInput
	}

	var created FulfillmentLocation
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO fulfillment_locations (id, supplier_id, supplier_market_id, market_code, code, name, location_type, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING created_at, updated_at
		`, id, supplierID, supplierMarketID, marketCode, code, name, locationType, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create fulfillment location")
		}
		created = FulfillmentLocation{ID: id, SupplierID: supplierID, SupplierMarketID: supplierMarketID, MarketCode: marketCode, Code: code, Name: name, LocationType: locationType, Status: status, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return nil
	})
	return created, err
}

// CreateStoreFulfillmentLocation creates the Seller-owned branch of a
// fulfillment location. The Store/Market composite FK and ownership CHECK are
// enforced by PostgreSQL; this method intentionally does not accept supplier
// identity.
func (r Repository) CreateStoreFulfillmentLocation(ctx context.Context, storeID, marketCode, code, name, locationType, status string) (FulfillmentLocation, error) {
	if storeID == "" || marketCode == "" || code == "" || name == "" || locationType == "" || status == "" {
		return FulfillmentLocation{}, ErrInvalidInput
	}

	var created FulfillmentLocation
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, store_id, market_code, code, name, location_type, status, created_at, updated_at
		`, id, storeID, marketCode, code, name, locationType, status).Scan(
			&created.ID, &created.StoreID, &created.MarketCode, &created.Code,
			&created.Name, &created.LocationType, &created.Status, &created.CreatedAt, &created.UpdatedAt,
		); err != nil {
			return translatePGError(err, "create store fulfillment location")
		}
		return nil
	})
	return created, err
}

func (r Repository) CreateInventorySnapshot(ctx context.Context, fulfillmentLocationID, skuID string, onHandQty int64) (InventorySnapshot, error) {
	if fulfillmentLocationID == "" || skuID == "" || onHandQty < 0 {
		return InventorySnapshot{}, ErrInvalidInput
	}

	var created InventorySnapshot
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO inventory_snapshots (id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version)
			VALUES ($1, $2, $3, $4, 0, 0)
			RETURNING created_at, updated_at
		`, id, fulfillmentLocationID, skuID, onHandQty).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create inventory snapshot")
		}
		created = InventorySnapshot{ID: id, FulfillmentLocationID: fulfillmentLocationID, SKUID: skuID, OnHandQty: onHandQty, ReservedQty: 0, Version: 0, CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresBySKU, skuID)
	})
	return created, err
}

func (r Repository) GetInventorySnapshot(ctx context.Context, snapshotID string) (InventorySnapshot, error) {
	if snapshotID == "" {
		return InventorySnapshot{}, ErrInvalidInput
	}

	var snapshot InventorySnapshot
	err := r.pool.QueryRow(ctx, `
		SELECT id, fulfillment_location_id, sku_id, on_hand_qty, reserved_qty, version, created_at, updated_at
		FROM inventory_snapshots
		WHERE id = $1
	`, snapshotID).Scan(
		&snapshot.ID,
		&snapshot.FulfillmentLocationID,
		&snapshot.SKUID,
		&snapshot.OnHandQty,
		&snapshot.ReservedQty,
		&snapshot.Version,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		return InventorySnapshot{}, translatePGError(err, "get inventory snapshot")
	}
	return snapshot, nil
}

func (r Repository) ReserveInventory(ctx context.Context, snapshotID string, quantity int64, reservationToken string, expiresAt *time.Time) (InventoryReservation, error) {
	if snapshotID == "" || reservationToken == "" || quantity <= 0 {
		return InventoryReservation{}, ErrInvalidInput
	}

	var created InventoryReservation
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var inventorySnapshotID string
		if err := tx.QueryRow(ctx, `
			UPDATE inventory_snapshots
			SET reserved_qty = reserved_qty + $2,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1 AND (on_hand_qty - reserved_qty) >= $2
			RETURNING id
		`, snapshotID, quantity).Scan(&inventorySnapshotID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInsufficientInventory
			}
			return translatePGError(err, "reserve inventory")
		}

		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO inventory_reservations (id, inventory_snapshot_id, quantity, status, reservation_token, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING created_at, updated_at
		`, id, inventorySnapshotID, quantity, "held", reservationToken, expiresAt).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create inventory reservation")
		}

		created = InventoryReservation{
			ID:                  id,
			InventorySnapshotID: inventorySnapshotID,
			Quantity:            quantity,
			Status:              "held",
			ReservationToken:    reservationToken,
			ExpiresAt:           expiresAt,
			CreatedAt:           created.CreatedAt,
			UpdatedAt:           created.UpdatedAt,
		}
		// A hold consumes available stock, so it can flip a product to
		// out of stock on every storefront that sells it.
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByInventorySnapshot, inventorySnapshotID)
	})
	return created, err
}

func (r Repository) GetStore(ctx context.Context, storeID string) (Store, error) {
	if storeID == "" {
		return Store{}, ErrInvalidInput
	}

	var store Store
	err := r.pool.QueryRow(ctx, `
		SELECT id, seller_id, market_code, code, name, status, created_at, updated_at
		FROM stores
		WHERE id = $1
	`, storeID).Scan(
		&store.ID,
		&store.SellerID,
		&store.MarketCode,
		&store.Code,
		&store.Name,
		&store.Status,
		&store.CreatedAt,
		&store.UpdatedAt,
	)
	if err != nil {
		return Store{}, translatePGError(err, "get store")
	}
	return store, nil
}

// StoreSellerID resolves the owning seller of a store. It satisfies the
// themes.StoreLookup interface so the Theme Engine can enforce resource-level
// authorization without importing commerce internals.
func (r Repository) StoreSellerID(ctx context.Context, storeID string) (string, error) {
	if storeID == "" {
		return "", ErrInvalidInput
	}
	var sellerID string
	err := r.pool.QueryRow(ctx, `SELECT seller_id FROM stores WHERE id = $1`, storeID).Scan(&sellerID)
	if err != nil {
		return "", translatePGError(err, "store seller")
	}
	return sellerID, nil
}

func (r Repository) GetSupplierOffer(ctx context.Context, offerID string) (SupplierOffer, error) {
	if offerID == "" {
		return SupplierOffer{}, ErrInvalidInput
	}

	var offer SupplierOffer
	err := r.pool.QueryRow(ctx, `
		SELECT id, supplier_id, supplier_product_id, supplier_market_id, market_code, status, created_at, updated_at
		FROM supplier_offers
		WHERE id = $1
	`, offerID).Scan(
		&offer.ID,
		&offer.SupplierID,
		&offer.SupplierProductID,
		&offer.SupplierMarketID,
		&offer.MarketCode,
		&offer.Status,
		&offer.CreatedAt,
		&offer.UpdatedAt,
	)
	if err != nil {
		return SupplierOffer{}, translatePGError(err, "get supplier offer")
	}
	return offer, nil
}

func jsonOrEmpty(settings map[string]any) []byte {
	if len(settings) == 0 {
		return []byte(`{}`)
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}

func normalizeSettings(settings map[string]any) map[string]any {
	if len(settings) == 0 {
		return map[string]any{}
	}
	clone := make(map[string]any, len(settings))
	for key, value := range settings {
		clone[key] = value
	}
	return clone
}

func upsertJSONSettings(ctx context.Context, tx pgx.Tx, query string, id string, settings map[string]any) error {
	_, err := tx.Exec(ctx, query, id, jsonOrEmpty(settings))
	return translatePGError(err, "upsert settings")
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func translatePGError(err error, action string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrConflict, action)
		case "23503", "23514":
			return fmt.Errorf("%w: %s", ErrInvalidInput, action)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
