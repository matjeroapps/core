package merchants

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrValidationProvisioningDisabled = errors.New("local validation provisioning is disabled")
	ErrValidationProvisioningUnsafe   = errors.New("local validation provisioning requires a local runtime")
	ErrValidationProvisioningInvalid  = errors.New("invalid local validation provisioning request")
)

type ValidationProvisioner struct {
	pool    *pgxpool.Pool
	enabled bool
	// storefrontDomain is the base domain of the platform storefront fixture
	// (<store-code>.<storefrontDomain>) provisioned for every validation store.
	storefrontDomain string
}

func NewValidationProvisioner(pool *pgxpool.Pool, enabled bool, storefrontDomain string) *ValidationProvisioner {
	return &ValidationProvisioner{pool: pool, enabled: enabled, storefrontDomain: strings.ToLower(strings.TrimSpace(storefrontDomain))}
}

type ValidationProvisionRequest struct {
	LocalOnly      bool              `json:"local_only"`
	Issuer         string            `json:"issuer"`
	IdempotencyKey string            `json:"-"`
	Actors         map[string]string `json:"actors"`
}

type ValidationScenarioManifest struct {
	Version     string                        `json:"version"`
	Actors      map[string]ValidationActorRow `json:"actors"`
	Merchants   map[string]ValidationMerchant `json:"merchants"`
	Stores      map[string]ValidationStore    `json:"stores"`
	Supply      map[string]string             `json:"supply"`
	Denials     map[string]string             `json:"denials"`
	GeneratedBy string                        `json:"generated_by"`
}

type ValidationActorRow struct {
	Subject  string   `json:"subject"`
	Expected []string `json:"expected"`
}

type ValidationMerchant struct {
	ID           uuid.UUID `json:"id"`
	Code         string    `json:"code"`
	Capabilities []string  `json:"capabilities"`
}

type ValidationStore struct {
	ID       uuid.UUID `json:"id"`
	Merchant string    `json:"merchant"`
	Code     string    `json:"code"`
	// Host is the storefront host that resolves to this store through the
	// platform-generated domain fixture provisioned alongside the store.
	Host string `json:"host"`
}

type validationMerchantSpec struct {
	label        string
	code         string
	name         string
	retailStatus CapabilityStatus
	supplyStatus CapabilityStatus
}

var validationMerchants = []validationMerchantSpec{
	{label: "dual_merchant", code: "f026-dual", name: "Feature 026 Dual Merchant", retailStatus: CapabilityStatusActive, supplyStatus: CapabilityStatusActive},
	{label: "retail_merchant", code: "f026-retail", name: "Feature 026 Retail Merchant", retailStatus: CapabilityStatusActive, supplyStatus: CapabilityStatusInactive},
	{label: "supply_merchant", code: "f026-supply", name: "Feature 026 Supply Merchant", retailStatus: CapabilityStatusInactive, supplyStatus: CapabilityStatusActive},
	{label: "retail_suspended_merchant", code: "f026-retail-suspended", name: "Feature 026 Retail Suspended", retailStatus: CapabilityStatusSuspended, supplyStatus: CapabilityStatusInactive},
	{label: "supply_suspended_merchant", code: "f026-supply-suspended", name: "Feature 026 Supply Suspended", retailStatus: CapabilityStatusInactive, supplyStatus: CapabilityStatusSuspended},
	{label: "empty_supply_merchant", code: "f026-empty-supply", name: "Feature 026 Empty Supply", retailStatus: CapabilityStatusInactive, supplyStatus: CapabilityStatusActive},
	{label: "foreign_merchant", code: "f026-foreign", name: "Feature 026 Foreign Merchant", retailStatus: CapabilityStatusActive, supplyStatus: CapabilityStatusActive},
}

var validationActorLabels = []string{
	"dual_owner",
	"retail_owner",
	"supply_owner",
	"retail_limited_staff",
	"supply_limited_staff",
	"suspended_membership",
	"multi_merchant_actor",
	"no_membership_actor",
}

var fullPermissions = []string{
	PermissionMerchantManage,
	PermissionTeamManage,
	PermissionRetailStoresManage,
	PermissionRetailCatalogManage,
	PermissionRetailOrdersManage,
	PermissionRetailInventoryManage,
	PermissionSupplyCatalogManage,
	PermissionSupplyInventoryManage,
	PermissionSupplyFulfillment,
	PermissionSupplyShippingManage,
}

func (p *ValidationProvisioner) ProvisionValidationScenario(ctx context.Context, req ValidationProvisionRequest) (*ValidationScenarioManifest, error) {
	if p == nil || !p.enabled {
		return nil, ErrValidationProvisioningDisabled
	}
	if !req.LocalOnly || !localIssuer(req.Issuer) || strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, ErrValidationProvisioningUnsafe
	}
	if p.pool == nil || strings.TrimSpace(p.storefrontDomain) == "" {
		return nil, fmt.Errorf("%w: missing storefront platform domain", ErrValidationProvisioningInvalid)
	}
	for _, label := range validationActorLabels {
		if strings.TrimSpace(req.Actors[label]) == "" {
			return nil, fmt.Errorf("%w: missing actor subject %s", ErrValidationProvisioningInvalid, label)
		}
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	manifest := &ValidationScenarioManifest{
		Version:     "1",
		Actors:      map[string]ValidationActorRow{},
		Merchants:   map[string]ValidationMerchant{},
		Stores:      map[string]ValidationStore{},
		Supply:      map[string]string{},
		Denials:     map[string]string{},
		GeneratedBy: "core.local-validation-provisioner",
	}

	merchantsByLabel := map[string]uuid.UUID{}
	for _, spec := range validationMerchants {
		id, err := upsertValidationMerchant(ctx, tx, spec)
		if err != nil {
			return nil, err
		}
		merchantsByLabel[spec.label] = id
		if err := upsertValidationCapability(ctx, tx, id, CapabilityTypeRetail, spec.retailStatus); err != nil {
			return nil, err
		}
		if err := upsertValidationCapability(ctx, tx, id, CapabilityTypeSupply, spec.supplyStatus); err != nil {
			return nil, err
		}
		caps := make([]string, 0, 2)
		if spec.retailStatus == CapabilityStatusActive {
			caps = append(caps, "retail")
		}
		if spec.supplyStatus == CapabilityStatusActive {
			caps = append(caps, "supply")
		}
		manifest.Merchants[spec.label] = ValidationMerchant{ID: id, Code: spec.code, Capabilities: caps}
	}

	dualSubject := req.Actors["dual_owner"]
	retailSubject := req.Actors["retail_owner"]
	supplySubject := req.Actors["supply_owner"]
	retailStaff := req.Actors["retail_limited_staff"]
	supplyStaff := req.Actors["supply_limited_staff"]
	suspendedSubject := req.Actors["suspended_membership"]
	multiSubject := req.Actors["multi_merchant_actor"]
	noMembershipSubject := req.Actors["no_membership_actor"]

	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["dual_merchant"], dualSubject, MembershipStatusActive, fullPermissions); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["retail_merchant"], retailSubject, MembershipStatusActive, []string{PermissionRetailStoresManage, PermissionRetailCatalogManage}); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["supply_merchant"], supplySubject, MembershipStatusActive, []string{PermissionSupplyCatalogManage, PermissionSupplyFulfillment}); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["dual_merchant"], retailStaff, MembershipStatusActive, []string{PermissionRetailCatalogManage}); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["dual_merchant"], supplyStaff, MembershipStatusActive, []string{PermissionSupplyCatalogManage}); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["dual_merchant"], suspendedSubject, MembershipStatusSuspended, fullPermissions); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["dual_merchant"], multiSubject, MembershipStatusActive, fullPermissions); err != nil {
		return nil, err
	}
	if err := upsertValidationMembership(ctx, tx, merchantsByLabel["foreign_merchant"], multiSubject, MembershipStatusActive, fullPermissions); err != nil {
		return nil, err
	}

	for label, subject := range req.Actors {
		expected := []string{}
		switch label {
		case "dual_owner", "multi_merchant_actor":
			expected = []string{"retail", "supply"}
		case "retail_owner", "retail_limited_staff":
			expected = []string{"retail"}
		case "supply_owner", "supply_limited_staff":
			expected = []string{"supply"}
		case "suspended_membership", "no_membership_actor":
			expected = []string{}
		}
		manifest.Actors[label] = ValidationActorRow{Subject: subject, Expected: expected}
	}
	manifest.Actors["no_membership_actor"] = ValidationActorRow{Subject: noMembershipSubject, Expected: []string{}}

	sellerID, err := upsertValidationSeller(ctx, tx, "f026-dual-seller", "Feature 026 Dual Seller", dualSubject, "owner", "active", merchantsByLabel["dual_merchant"])
	if err != nil {
		return nil, err
	}
	if _, err := upsertValidationSeller(ctx, tx, "f026-retail-seller", "Feature 026 Retail Seller", retailSubject, "owner", "active", merchantsByLabel["retail_merchant"]); err != nil {
		return nil, err
	}
	supplierID, err := upsertValidationSupplier(ctx, tx, "f026-dual-supplier", "Feature 026 Dual Supplier", supplySubject, "owner", "active", merchantsByLabel["dual_merchant"])
	if err != nil {
		return nil, err
	}
	if _, err := upsertValidationSupplier(ctx, tx, "f026-supply-supplier", "Feature 026 Supply Supplier", supplySubject, "owner", "active", merchantsByLabel["supply_merchant"]); err != nil {
		return nil, err
	}

	storeA, err := upsertValidationStore(ctx, tx, sellerID, "EG", "f026-store-a", "Feature 026 Store A", "active")
	if err != nil {
		return nil, err
	}
	storeB, err := upsertValidationStore(ctx, tx, sellerID, "EG", "f026-store-b", "Feature 026 Store B", "active")
	if err != nil {
		return nil, err
	}
	hostA, err := upsertValidationStoreDomain(ctx, tx, storeA, "f026-store-a", p.storefrontDomain)
	if err != nil {
		return nil, err
	}
	hostB, err := upsertValidationStoreDomain(ctx, tx, storeB, "f026-store-b", p.storefrontDomain)
	if err != nil {
		return nil, err
	}
	manifest.Stores["dual_store_a"] = ValidationStore{ID: storeA, Merchant: "dual_merchant", Code: "f026-store-a", Host: hostA}
	manifest.Stores["dual_store_b"] = ValidationStore{ID: storeB, Merchant: "dual_merchant", Code: "f026-store-b", Host: hostB}

	connectionID, err := upsertValidationConnection(ctx, tx, merchantsByLabel["dual_merchant"], storeA, "feature-026-supply", supplierID.String())
	if err != nil {
		return nil, err
	}
	batchID, recordID, reviewID, mappingID, cursorID, fulfillmentID, trackingID, err := upsertValidationSupply(ctx, tx, merchantsByLabel["dual_merchant"], connectionID)
	if err != nil {
		return nil, err
	}
	manifest.Supply["connection"] = connectionID.String()
	manifest.Supply["import_batch"] = batchID.String()
	manifest.Supply["staged_record"] = recordID.String()
	manifest.Supply["review_case"] = reviewID.String()
	manifest.Supply["mapping"] = mappingID.String()
	manifest.Supply["sync_cursor"] = cursorID.String()
	manifest.Supply["fulfillment_request"] = fulfillmentID.String()
	manifest.Supply["tracking_event"] = trackingID.String()
	manifest.Denials["foreign_merchant"] = merchantsByLabel["foreign_merchant"].String()
	manifest.Denials["foreign_store"] = storeB.String()

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return manifest, nil
}

func localIssuer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "zitadel"
}

func upsertValidationMerchant(ctx context.Context, tx pgx.Tx, spec validationMerchantSpec) (uuid.UUID, error) {
	id := uuid.New()
	err := tx.QueryRow(ctx, `
		INSERT INTO merchants (id, code, legal_name, status)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (code) DO UPDATE SET legal_name = EXCLUDED.legal_name, status = 'active', updated_at = now()
		RETURNING id
	`, id, spec.code, spec.name).Scan(&id)
	return id, err
}

func upsertValidationCapability(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, capType CapabilityType, status CapabilityStatus) error {
	id := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO merchant_capabilities (id, merchant_id, capability_type, status, activated_at, suspended_at)
		VALUES ($1, $2, $3, $4,
			CASE WHEN $4 = 'active' THEN now() ELSE NULL END,
			CASE WHEN $4 = 'suspended' THEN now() ELSE NULL END)
		ON CONFLICT (merchant_id, capability_type) DO UPDATE SET
			status = EXCLUDED.status,
			activated_at = EXCLUDED.activated_at,
			suspended_at = EXCLUDED.suspended_at,
			updated_at = now()
	`, id, merchantID, string(capType), string(status))
	return err
}

func upsertValidationMembership(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, subject string, status MembershipStatus, permissions []string) error {
	id := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_memberships (id, merchant_id, principal_subject, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (merchant_id, principal_subject) DO UPDATE SET status = EXCLUDED.status, updated_at = now()
		RETURNING id
	`, id, merchantID, subject, string(status)).Scan(&id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM merchant_membership_permissions WHERE membership_id = $1`, id); err != nil {
		return err
	}
	for _, perm := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO merchant_membership_permissions (id, membership_id, permission_code) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, uuid.New(), id, perm); err != nil {
			return err
		}
	}
	return nil
}

func upsertValidationSeller(ctx context.Context, tx pgx.Tx, code, name, subject, role, status string, merchantID uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO sellers (id, code, name, status, merchant_id)
		VALUES ($1, $2, $3, 'active', $4)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, status = 'active', merchant_id = EXCLUDED.merchant_id, updated_at = now()
		RETURNING id
	`, id, code, name, merchantID).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO seller_members (id, seller_id, principal_subject, role, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (seller_id, principal_subject) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status, updated_at = now()
	`, uuid.New(), id, subject, role, status); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func upsertValidationSupplier(ctx context.Context, tx pgx.Tx, code, name, subject, role, status string, merchantID uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO suppliers (id, code, name, status, merchant_id)
		VALUES ($1, $2, $3, 'active', $4)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, status = 'active', merchant_id = EXCLUDED.merchant_id, updated_at = now()
		RETURNING id
	`, id, code, name, merchantID).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO supplier_members (id, supplier_id, principal_subject, role, status)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (supplier_id, principal_subject) DO UPDATE SET role = EXCLUDED.role, status = EXCLUDED.status, updated_at = now()
	`, uuid.New(), id, subject, role, status); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO supplier_markets (id, supplier_id, market_code, status)
		VALUES ($1, $2, 'EG', 'active')
		ON CONFLICT (supplier_id, market_code) DO UPDATE SET status = 'active', updated_at = now()
	`, uuid.New(), id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func upsertValidationStore(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, marketCode, code, name, status string) (uuid.UUID, error) {
	id := uuid.New()
	err := tx.QueryRow(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (seller_id, code) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = now()
		RETURNING id
	`, id, sellerID, marketCode, code, name, status).Scan(&id)
	return id, err
}

// upsertValidationStoreDomain provisions the active platform storefront domain
// fixture that a public storefront host resolves through. It mirrors the
// production platform subdomain lifecycle (type "platform", active, primary,
// verified at creation) without bypassing any runtime authorization rule.
func upsertValidationStoreDomain(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, storeCode, storefrontDomain string) (string, error) {
	if strings.TrimSpace(storefrontDomain) == "" {
		return "", fmt.Errorf("%w: missing storefront platform domain", ErrValidationProvisioningInvalid)
	}
	domain := strings.ToLower(strings.TrimSpace(storeCode)) + "." + strings.ToLower(strings.TrimSpace(storefrontDomain))
	id := uuid.New()
	err := tx.QueryRow(ctx, `
		INSERT INTO store_domains (id, store_id, domain, is_primary, verified_at, status, domain_type, verification_token)
		VALUES ($1, $2, $3, true, now(), 'active', 'platform', NULL)
		ON CONFLICT (lower(domain)) DO UPDATE SET
			store_id = EXCLUDED.store_id,
			is_primary = EXCLUDED.is_primary,
			verified_at = EXCLUDED.verified_at,
			status = 'active',
			domain_type = 'platform',
			updated_at = now()
		RETURNING domain
	`, id, storeID, domain).Scan(&domain)
	if err != nil {
		return "", fmt.Errorf("provision validation storefront domain: %w", err)
	}
	return domain, nil
}

func upsertValidationConnection(ctx context.Context, tx pgx.Tx, merchantID, storeID uuid.UUID, externalAccount, supplierID string) (uuid.UUID, error) {
	id := uuid.New()
	err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_connections
			(id, merchant_id, connection_type, provider, external_account_id, name, store_id, status, granted_scopes, health_status, legacy_actor_type, legacy_actor_id)
		VALUES ($1, $2, 'SUPPLY_SOURCE', 'feature-026-local', $3, 'Feature 026 Local Supply', $4, 'ACTIVE', '["catalog","fulfillment"]'::jsonb, 'healthy', 'supplier', $5)
		ON CONFLICT (merchant_id, provider, external_account_id) DO UPDATE SET
			name = EXCLUDED.name,
			store_id = EXCLUDED.store_id,
			status = 'ACTIVE',
			health_status = 'healthy',
			updated_at = now()
		RETURNING id
	`, id, merchantID, externalAccount, storeID, supplierID).Scan(&id)
	return id, err
}

func upsertValidationSupply(ctx context.Context, tx pgx.Tx, merchantID, connectionID uuid.UUID) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, error) {
	batchID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_supply_import_batches
			(id, merchant_id, connection_id, provider, batch_type, status, record_count, idempotency_key)
		VALUES ($1, $2, $3, 'feature-026-local', 'FIRST_IMPORT', 'IN_REVIEW', 1, 'feature-026-batch')
		ON CONFLICT (connection_id, idempotency_key) DO UPDATE SET status = 'IN_REVIEW', record_count = 1, updated_at = now()
		RETURNING id
	`, batchID, merchantID, connectionID).Scan(&batchID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	recordID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_supply_import_records
			(id, batch_id, merchant_id, connection_id, entity_type, external_product_id, sku, title, currency, content_digest, payload, status)
		VALUES ($1, $2, $3, $4, 'PRODUCT', 'feature-026-product', 'F026-SKU', 'Feature 026 Live Supply Product', 'EGP', 'feature-026-digest', '{"source":"feature-026"}'::jsonb, 'REVIEW_REQUIRED')
		ON CONFLICT (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, '')) DO UPDATE SET
			batch_id = EXCLUDED.batch_id,
			status = 'REVIEW_REQUIRED',
			updated_at = now()
		RETURNING id
	`, recordID, batchID, merchantID, connectionID).Scan(&recordID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	reviewID, err := upsertValidationReviewCase(ctx, tx, merchantID, connectionID, batchID, recordID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merchant_integration_supply_import_records
		SET review_case_id = $1, updated_at = now()
		WHERE id = $2
	`, reviewID, recordID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	mappingID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_entity_mappings
			(id, merchant_id, connection_id, entity_type, internal_id, external_product_id, authority_source, provenance, status, approved_batch_id)
		VALUES ($1, $2, $3, 'PRODUCT', $4, 'feature-026-product', 'EXTERNAL', '{"source":"feature-026"}'::jsonb, 'ACTIVE', $5)
		ON CONFLICT (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, '')) DO UPDATE SET
			internal_id = EXCLUDED.internal_id,
			status = 'ACTIVE',
			approved_batch_id = EXCLUDED.approved_batch_id,
			updated_at = now()
		RETURNING id
	`, mappingID, merchantID, connectionID, recordID.String(), batchID).Scan(&mappingID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	cursorID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_sync_cursors (id, connection_id, entity_type, cursor_token, last_successful_sync)
		VALUES ($1, $2, 'PRODUCT', 'feature-026-cursor', now())
		ON CONFLICT (connection_id, entity_type) DO UPDATE SET cursor_token = EXCLUDED.cursor_token, last_successful_sync = now(), updated_at = now()
		RETURNING id
	`, cursorID, connectionID).Scan(&cursorID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	fulfillmentID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_supply_fulfillment_requests
			(id, merchant_id, connection_id, idempotency_key, status, payload, provider, external_fulfillment_id)
		VALUES ($1, $2, $3, 'feature-026-fulfillment', 'ACKNOWLEDGED', '{"source":"feature-026"}'::jsonb, 'feature-026-local', 'f026-fulfillment')
		ON CONFLICT (connection_id, idempotency_key) DO UPDATE SET status = 'ACKNOWLEDGED', updated_at = now()
		RETURNING id
	`, fulfillmentID, merchantID, connectionID).Scan(&fulfillmentID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	trackingID := uuid.New()
	if err := tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_supply_tracking_events
			(id, request_id, connection_id, external_event_id, status, carrier, tracking_number, occurred_at, payload)
		VALUES ($1, $2, $3, 'feature-026-tracking', 'IN_TRANSIT', 'FeatureCarrier', 'F026TRACK', now(), '{"source":"feature-026"}'::jsonb)
		ON CONFLICT (connection_id, external_event_id) DO UPDATE SET status = 'IN_TRANSIT'
		RETURNING id
	`, trackingID, fulfillmentID, connectionID).Scan(&trackingID); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	return batchID, recordID, reviewID, mappingID, cursorID, fulfillmentID, trackingID, nil
}

func upsertValidationReviewCase(ctx context.Context, tx pgx.Tx, merchantID, connectionID, batchID, recordID uuid.UUID) (uuid.UUID, error) {
	var reviewID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM merchant_integration_review_cases
		WHERE connection_id = $1
			AND reason_code = 'FEATURE_026_REVIEW'
			AND subject_record_id = $2
		ORDER BY created_at, id
		LIMIT 1
	`, connectionID, recordID).Scan(&reviewID)
	if err == nil {
		_, updateErr := tx.Exec(ctx, `
			UPDATE merchant_integration_review_cases
			SET merchant_id = $1,
				subject_batch_id = $2,
				status = 'OPEN',
				details = '{"source":"feature-026"}'::jsonb,
				updated_at = now()
			WHERE id = $3
		`, merchantID, batchID, reviewID)
		return reviewID, updateErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	reviewID = uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO merchant_integration_review_cases
			(id, merchant_id, connection_id, case_type, status, reason_code, subject_batch_id, subject_record_id, details)
		VALUES ($1, $2, $3, 'PRODUCT_MAPPING', 'OPEN', 'FEATURE_026_REVIEW', $4, $5, '{"source":"feature-026"}'::jsonb)
		RETURNING id
	`, reviewID, merchantID, connectionID, batchID, recordID).Scan(&reviewID)
	return reviewID, err
}
