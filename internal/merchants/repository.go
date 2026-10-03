package merchants

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	CreateMerchant(ctx context.Context, m *Merchant) error
	GetMerchantByID(ctx context.Context, id uuid.UUID) (*Merchant, error)
	GetMerchantByCode(ctx context.Context, code string) (*Merchant, error)
	UpdateMerchant(ctx context.Context, m *Merchant) error

	UpsertCapability(ctx context.Context, cap *MerchantCapability) error
	GetCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error)
	ListCapabilities(ctx context.Context, merchantID uuid.UUID) ([]*MerchantCapability, error)

	CreateMembership(ctx context.Context, mem *MerchantMembership) error
	GetMembership(ctx context.Context, merchantID uuid.UUID, subject string) (*MerchantMembership, error)
	ListMemberships(ctx context.Context, merchantID uuid.UUID) ([]*MerchantMembership, error)
	ListMembershipsBySubject(ctx context.Context, subject string) ([]*MerchantMembership, error)

	GrantPermissions(ctx context.Context, membershipID uuid.UUID, permissions []string) error
	GetMembershipPermissions(ctx context.Context, membershipID uuid.UUID) ([]string, error)

	LinkSellerProfile(ctx context.Context, sellerID, merchantID uuid.UUID) error
	LinkSupplierProfile(ctx context.Context, supplierID, merchantID uuid.UUID) error

	RecordCrosswalk(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, merchantID uuid.UUID) error
	RecordQuarantine(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, reason string, details map[string]any) error
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) createMerchantTx(ctx context.Context, tx pgx.Tx, m *Merchant) error {
	query := `INSERT INTO merchants (id, code, legal_name, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := tx.Exec(ctx, query, m.ID, m.Code, m.LegalName, string(m.Status), m.CreatedAt, m.UpdatedAt)
	return err
}

func (r *PostgresRepository) upsertCapabilityTx(ctx context.Context, tx pgx.Tx, cap *MerchantCapability) error {
	query := `
		INSERT INTO merchant_capabilities (id, merchant_id, capability_type, status, activated_at, suspended_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (merchant_id, capability_type) DO UPDATE SET
			status = EXCLUDED.status,
			activated_at = EXCLUDED.activated_at,
			suspended_at = EXCLUDED.suspended_at,
			updated_at = EXCLUDED.updated_at
	`
	_, err := tx.Exec(ctx, query, cap.ID, cap.MerchantID, string(cap.CapabilityType), string(cap.Status), cap.ActivatedAt, cap.SuspendedAt, cap.CreatedAt, cap.UpdatedAt)
	return err
}

func (r *PostgresRepository) createMembershipTx(ctx context.Context, tx pgx.Tx, mem *MerchantMembership) error {
	query := `INSERT INTO merchant_memberships (id, merchant_id, principal_subject, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := tx.Exec(ctx, query, mem.ID, mem.MerchantID, mem.PrincipalSubject, string(mem.Status), mem.CreatedAt, mem.UpdatedAt)
	return err
}

func (r *PostgresRepository) grantPermissionsTx(ctx context.Context, tx pgx.Tx, membershipID uuid.UUID, permissions []string) error {
	for _, perm := range permissions {
		query := `INSERT INTO merchant_membership_permissions (id, membership_id, permission_code, granted_at) VALUES ($1, $2, $3, now()) ON CONFLICT DO NOTHING`
		if _, err := tx.Exec(ctx, query, uuid.New(), membershipID, perm); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresRepository) CreateMerchant(ctx context.Context, m *Merchant) error {
	query := `INSERT INTO merchants (id, code, legal_name, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.pool.Exec(ctx, query, m.ID, m.Code, m.LegalName, string(m.Status), m.CreatedAt, m.UpdatedAt)
	return err
}

func (r *PostgresRepository) GetMerchantByID(ctx context.Context, id uuid.UUID) (*Merchant, error) {
	query := `SELECT id, code, legal_name, status, created_at, updated_at FROM merchants WHERE id = $1`
	var m Merchant
	var statusStr string
	err := r.pool.QueryRow(ctx, query, id).Scan(&m.ID, &m.Code, &m.LegalName, &statusStr, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	m.Status = MerchantStatus(statusStr)
	return &m, nil
}

func (r *PostgresRepository) GetMerchantByCode(ctx context.Context, code string) (*Merchant, error) {
	query := `SELECT id, code, legal_name, status, created_at, updated_at FROM merchants WHERE code = $1`
	var m Merchant
	var statusStr string
	err := r.pool.QueryRow(ctx, query, code).Scan(&m.ID, &m.Code, &m.LegalName, &statusStr, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	m.Status = MerchantStatus(statusStr)
	return &m, nil
}

func (r *PostgresRepository) UpdateMerchant(ctx context.Context, m *Merchant) error {
	query := `UPDATE merchants SET code = $2, legal_name = $3, status = $4, updated_at = $5 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, m.ID, m.Code, m.LegalName, string(m.Status), m.UpdatedAt)
	return err
}

func (r *PostgresRepository) UpsertCapability(ctx context.Context, cap *MerchantCapability) error {
	query := `
		INSERT INTO merchant_capabilities (id, merchant_id, capability_type, status, activated_at, suspended_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (merchant_id, capability_type) DO UPDATE SET
			status = EXCLUDED.status,
			activated_at = EXCLUDED.activated_at,
			suspended_at = EXCLUDED.suspended_at,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.pool.Exec(ctx, query, cap.ID, cap.MerchantID, string(cap.CapabilityType), string(cap.Status), cap.ActivatedAt, cap.SuspendedAt, cap.CreatedAt, cap.UpdatedAt)
	return err
}

func (r *PostgresRepository) GetCapability(ctx context.Context, merchantID uuid.UUID, capType CapabilityType) (*MerchantCapability, error) {
	query := `SELECT id, merchant_id, capability_type, status, activated_at, suspended_at, created_at, updated_at FROM merchant_capabilities WHERE merchant_id = $1 AND capability_type = $2`
	var c MerchantCapability
	var capTypeStr, statusStr string
	err := r.pool.QueryRow(ctx, query, merchantID, string(capType)).Scan(&c.ID, &c.MerchantID, &capTypeStr, &statusStr, &c.ActivatedAt, &c.SuspendedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCapabilityNotFound
		}
		return nil, err
	}
	c.CapabilityType = CapabilityType(capTypeStr)
	c.Status = CapabilityStatus(statusStr)
	return &c, nil
}

func (r *PostgresRepository) ListCapabilities(ctx context.Context, merchantID uuid.UUID) ([]*MerchantCapability, error) {
	query := `SELECT id, merchant_id, capability_type, status, activated_at, suspended_at, created_at, updated_at FROM merchant_capabilities WHERE merchant_id = $1`
	rows, err := r.pool.Query(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var caps []*MerchantCapability
	for rows.Next() {
		var c MerchantCapability
		var capTypeStr, statusStr string
		if err := rows.Scan(&c.ID, &c.MerchantID, &capTypeStr, &statusStr, &c.ActivatedAt, &c.SuspendedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.CapabilityType = CapabilityType(capTypeStr)
		c.Status = CapabilityStatus(statusStr)
		caps = append(caps, &c)
	}
	return caps, nil
}

func (r *PostgresRepository) CreateMembership(ctx context.Context, mem *MerchantMembership) error {
	query := `INSERT INTO merchant_memberships (id, merchant_id, principal_subject, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.pool.Exec(ctx, query, mem.ID, mem.MerchantID, mem.PrincipalSubject, string(mem.Status), mem.CreatedAt, mem.UpdatedAt)
	return err
}

func (r *PostgresRepository) GetMembership(ctx context.Context, merchantID uuid.UUID, subject string) (*MerchantMembership, error) {
	query := `SELECT id, merchant_id, principal_subject, status, created_at, updated_at FROM merchant_memberships WHERE merchant_id = $1 AND principal_subject = $2`
	var m MerchantMembership
	var statusStr string
	err := r.pool.QueryRow(ctx, query, merchantID, subject).Scan(&m.ID, &m.MerchantID, &m.PrincipalSubject, &statusStr, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	m.Status = MembershipStatus(statusStr)
	perms, err := r.GetMembershipPermissions(ctx, m.ID)
	if err == nil {
		m.Permissions = perms
	}
	return &m, nil
}

func (r *PostgresRepository) ListMemberships(ctx context.Context, merchantID uuid.UUID) ([]*MerchantMembership, error) {
	query := `SELECT id, merchant_id, principal_subject, status, created_at, updated_at FROM merchant_memberships WHERE merchant_id = $1`
	rows, err := r.pool.Query(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mems []*MerchantMembership
	for rows.Next() {
		var m MerchantMembership
		var statusStr string
		if err := rows.Scan(&m.ID, &m.MerchantID, &m.PrincipalSubject, &statusStr, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Status = MembershipStatus(statusStr)
		perms, err := r.GetMembershipPermissions(ctx, m.ID)
		if err == nil {
			m.Permissions = perms
		}
		mems = append(mems, &m)
	}
	return mems, nil
}

// maxMembershipsPerSubject bounds the subject-oriented resolution read.
const maxMembershipsPerSubject = 100

// ListMembershipsBySubject resolves every canonical membership recorded for a
// principal subject, across all statuses and merchants. Merchant ownership is
// never inferred here: only explicit membership rows are returned. Invited and
// suspended memberships carry no permission data.
func (r *PostgresRepository) ListMembershipsBySubject(ctx context.Context, subject string) ([]*MerchantMembership, error) {
	query := `
		SELECT id, merchant_id, principal_subject, status, created_at, updated_at
		FROM merchant_memberships
		WHERE principal_subject = $1
		ORDER BY created_at, id
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, subject, maxMembershipsPerSubject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mems []*MerchantMembership
	for rows.Next() {
		var m MerchantMembership
		var statusStr string
		if err := rows.Scan(&m.ID, &m.MerchantID, &m.PrincipalSubject, &statusStr, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Status = MembershipStatus(statusStr)
		mems = append(mems, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(mems) == 0 {
		return mems, nil
	}

	ids := make([]uuid.UUID, 0, len(mems))
	for _, mem := range mems {
		ids = append(ids, mem.ID)
	}
	permRows, err := r.pool.Query(ctx, `SELECT membership_id, permission_code FROM merchant_membership_permissions WHERE membership_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer permRows.Close()

	permsByMembership := make(map[uuid.UUID][]string, len(mems))
	for permRows.Next() {
		var membershipID uuid.UUID
		var code string
		if err := permRows.Scan(&membershipID, &code); err != nil {
			return nil, err
		}
		permsByMembership[membershipID] = append(permsByMembership[membershipID], code)
	}
	if err := permRows.Err(); err != nil {
		return nil, err
	}
	for _, mem := range mems {
		if mem.Status == MembershipStatusActive {
			mem.Permissions = permsByMembership[mem.ID]
		}
	}
	return mems, nil
}

func (r *PostgresRepository) GrantPermissions(ctx context.Context, membershipID uuid.UUID, permissions []string) error {
	for _, perm := range permissions {
		id := uuid.New()
		query := `INSERT INTO merchant_membership_permissions (id, membership_id, permission_code, granted_at) VALUES ($1, $2, $3, now()) ON CONFLICT DO NOTHING`
		if _, err := r.pool.Exec(ctx, query, id, membershipID, perm); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresRepository) GetMembershipPermissions(ctx context.Context, membershipID uuid.UUID) ([]string, error) {
	query := `SELECT permission_code FROM merchant_membership_permissions WHERE membership_id = $1`
	rows, err := r.pool.Query(ctx, query, membershipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, nil
}

func (r *PostgresRepository) LinkSellerProfile(ctx context.Context, sellerID, merchantID uuid.UUID) error {
	query := `UPDATE sellers SET merchant_id = $2 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, sellerID, merchantID)
	return err
}

func (r *PostgresRepository) LinkSupplierProfile(ctx context.Context, supplierID, merchantID uuid.UUID) error {
	query := `UPDATE suppliers SET merchant_id = $2 WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, supplierID, merchantID)
	return err
}

func (r *PostgresRepository) RecordCrosswalk(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, merchantID uuid.UUID) error {
	query := `INSERT INTO merchant_migration_crosswalk (id, source_type, source_seller_id, source_supplier_id, merchant_id, migrated_at) VALUES ($1, $2, $3, $4, $5, now())`
	_, err := r.pool.Exec(ctx, query, uuid.New(), sourceType, sellerID, supplierID, merchantID)
	return err
}

func (r *PostgresRepository) RecordQuarantine(ctx context.Context, sourceType string, sellerID, supplierID *uuid.UUID, reason string, details map[string]any) error {
	query := `INSERT INTO merchant_migration_quarantine (id, source_type, source_seller_id, source_supplier_id, reason_code, details, quarantined_at) VALUES ($1, $2, $3, $4, $5, $6, now())`
	_, err := r.pool.Exec(ctx, query, uuid.New(), sourceType, sellerID, supplierID, reason, details)
	return err
}
