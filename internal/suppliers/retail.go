package suppliers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrForbidden     = errors.New("supplier owner authorization required")
	ErrAlreadyExists = errors.New("supplier already has an affiliated seller profile")
	ErrInvalidInput  = errors.New("invalid input parameters")
	ErrNotFound      = errors.New("supplier retail affiliation not found")
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

type SupplierSellerAffiliation struct {
	SupplierID string    `json:"supplier_id"`
	SellerID   string    `json:"seller_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type SellerProfile struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProvisionRetailParams struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings,omitempty"`
}

// Service provides domain logic for supplier retail operations.
type Service struct {
	pool DBPool
}

// NewService creates a new supplier domain service.
func NewService(pool DBPool) Service {
	return Service{pool: pool}
}

// ProvisionRetailCapability provisions a retail capability for a supplier.
//
// The transaction atomically creates:
// 1. A new `sellers` record.
// 2. `seller_settings` for the seller.
// 3. `seller_members` inserting ONLY the authenticated Supplier Owner as Seller Owner.
// 4. The 1:1 `supplier_seller_affiliations` link.
//
// If any insertion fails, the entire operation is rolled back leaving no orphaned seller.
func (s Service) ProvisionRetailCapability(ctx context.Context, supplierID, subject string, params ProvisionRetailParams) (*SellerProfile, *SupplierSellerAffiliation, error) {
	return ProvisionRetailCapabilityTx(ctx, s.pool, supplierID, subject, params)
}

// ProvisionRetailCapabilityTx executes the atomic transaction for retail capability provisioning.
func ProvisionRetailCapabilityTx(ctx context.Context, pool DBPool, supplierID, subject string, params ProvisionRetailParams) (*SellerProfile, *SupplierSellerAffiliation, error) {
	if strings.TrimSpace(supplierID) == "" || strings.TrimSpace(subject) == "" {
		return nil, nil, fmt.Errorf("%w: supplierID and subject are required", ErrInvalidInput)
	}
	code := strings.TrimSpace(params.Code)
	name := strings.TrimSpace(params.Name)
	if code == "" || name == "" {
		return nil, nil, fmt.Errorf("%w: code and name are required", ErrInvalidInput)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// 1. Verify that the subject is an active owner member of the supplier
	var role string
	var memberStatus string
	err = tx.QueryRow(ctx, `
		SELECT role, status
		FROM supplier_members
		WHERE supplier_id = $1 AND principal_subject = $2
	`, supplierID, subject).Scan(&role, &memberStatus)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrForbidden
		}
		return nil, nil, fmt.Errorf("check supplier member role: %w", err)
	}

	if memberStatus != "active" || role != "owner" {
		return nil, nil, ErrForbidden
	}

	// Check if supplier already has an affiliation (strictly 1:1 rule)
	var existingSellerID string
	err = tx.QueryRow(ctx, `
		SELECT seller_id FROM supplier_seller_affiliations WHERE supplier_id = $1
	`, supplierID).Scan(&existingSellerID)
	if err == nil {
		return nil, nil, ErrAlreadyExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("check existing affiliation: %w", err)
	}

	// 2. Insert new seller record
	sellerID := uuid.NewString()
	sellerStatus := "active"
	seller := &SellerProfile{
		ID:     sellerID,
		Code:   code,
		Name:   name,
		Status: sellerStatus,
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO sellers (id, code, name, status)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at
	`, sellerID, code, name, sellerStatus).Scan(&seller.CreatedAt, &seller.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil, fmt.Errorf("%w: seller code '%s' already exists", ErrAlreadyExists, code)
		}
		return nil, nil, fmt.Errorf("create seller: %w", err)
	}

	// 3. Insert seller_settings
	settingsJSON, err := json.Marshal(params.Settings)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid settings format", ErrInvalidInput)
	}
	if params.Settings == nil {
		settingsJSON = []byte("{}")
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO seller_settings (seller_id, settings)
		VALUES ($1, $2)
		ON CONFLICT (seller_id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()
	`, sellerID, settingsJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("create seller settings: %w", err)
	}

	// 4. Insert seller_members (ONLY executing Supplier Owner becomes Seller Owner)
	sellerMemberID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO seller_members (id, seller_id, principal_subject, role, status)
		VALUES ($1, $2, $3, 'owner', 'active')
	`, sellerMemberID, sellerID, subject)
	if err != nil {
		return nil, nil, fmt.Errorf("create seller member: %w", err)
	}

	// 5. Insert supplier_seller_affiliations link
	affiliation := &SupplierSellerAffiliation{
		SupplierID: supplierID,
		SellerID:   sellerID,
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO supplier_seller_affiliations (supplier_id, seller_id)
		VALUES ($1, $2)
		RETURNING created_at
	`, supplierID, sellerID).Scan(&affiliation.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil, fmt.Errorf("%w: supplier retail affiliation already exists", ErrAlreadyExists)
		}
		return nil, nil, fmt.Errorf("create supplier seller affiliation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	return seller, affiliation, nil
}

// GetSupplierSellerAffiliation retrieves the affiliation record for a given supplier ID.
func GetSupplierSellerAffiliation(ctx context.Context, exec DBExecutor, supplierID string) (*SupplierSellerAffiliation, error) {
	if strings.TrimSpace(supplierID) == "" {
		return nil, fmt.Errorf("%w: supplierID is required", ErrInvalidInput)
	}

	var aff SupplierSellerAffiliation
	err := exec.QueryRow(ctx, `
		SELECT supplier_id, seller_id, created_at
		FROM supplier_seller_affiliations
		WHERE supplier_id = $1
	`, supplierID).Scan(&aff.SupplierID, &aff.SellerID, &aff.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get supplier seller affiliation: %w", err)
	}

	return &aff, nil
}
