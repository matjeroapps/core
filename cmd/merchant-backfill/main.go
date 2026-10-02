package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"core/internal/merchants"
	"core/packages/events"
	"core/packages/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sourceSeller      = "SELLER"
	sourceSupplier    = "SUPPLIER"
	sourceAffiliation = "AFFILIATION"
)

var merchantNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("matjerhub:merchant-backfill"))

type BackfillMetrics struct {
	TotalScanned        int `json:"total_scanned"`
	MappedProfiles      int `json:"mapped_profiles"`
	QuarantinedProfiles int `json:"quarantined_profiles"`
	DeferredProfiles    int `json:"deferred_profiles"`
	FailedProfiles      int `json:"failed_profiles"`
	RetriedProfiles     int `json:"retried_profiles"`
}

type AuditEntry struct {
	Action           string     `json:"action"`
	SourceType       string     `json:"source_type"`
	SourceSellerID   *uuid.UUID `json:"source_seller_id,omitempty"`
	SourceSupplierID *uuid.UUID `json:"source_supplier_id,omitempty"`
	MerchantID       uuid.UUID  `json:"merchant_id,omitempty"`
	ReasonCode       string     `json:"reason_code,omitempty"`
	DryRun           bool       `json:"dry_run"`
}

type BackfillReport struct {
	DryRun       bool            `json:"dry_run"`
	BatchSize    int             `json:"batch_size"`
	Limit        int             `json:"limit"`
	Metrics      BackfillMetrics `json:"metrics"`
	AuditEntries []AuditEntry    `json:"audit_entries"`
	GeneratedAt  time.Time       `json:"generated_at"`
}

type profile struct {
	ID         uuid.UUID
	Code       string
	Name       string
	Status     string
	MerchantID *uuid.UUID
}

type affiliation struct {
	SupplierID uuid.UUID
	SellerID   uuid.UUID
}

type workItem struct {
	SourceType     string
	Seller         *profile
	Supplier       *profile
	SellerOwners   []string
	SupplierOwners []string
	ReasonCode     string
}

func (m *BackfillMetrics) AssertIdentityEquation() error {
	expected := m.MappedProfiles + m.QuarantinedProfiles + m.DeferredProfiles
	if m.TotalScanned != expected {
		return fmt.Errorf("inventory accounting identity equation failure: scanned (%d) != mapped (%d) + quarantined (%d) + deferred (%d)",
			m.TotalScanned, m.MappedProfiles, m.QuarantinedProfiles, m.DeferredProfiles)
	}
	return nil
}

func main() {
	dryRun := flag.Bool("dry-run", true, "classify and validate without modifying database")
	batchSize := flag.Int("batch-size", 100, "number of source records to process per batch")
	limit := flag.Int("limit", 0, "maximum work items to classify; 0 means no limit")
	flag.Parse()

	if *batchSize <= 0 {
		log.Fatal("[ERROR] --batch-size must be greater than zero")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("[ERROR] DATABASE_URL is required; fake simulation output has been removed")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("[ERROR] connect database: %v", err)
	}
	defer pool.Close()

	report, err := executeBackfill(ctx, pool, BackfillOptions{
		DryRun:    *dryRun,
		BatchSize: *batchSize,
		Limit:     *limit,
	})
	if err != nil {
		log.Fatalf("[ERROR] backfill failed: %v", err)
	}
	if err := report.Metrics.AssertIdentityEquation(); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		log.Fatalf("[ERROR] encode audit report: %v", err)
	}
}

type BackfillOptions struct {
	DryRun    bool
	BatchSize int
	Limit     int
}

func executeBackfill(ctx context.Context, pool *pgxpool.Pool, opts BackfillOptions) (*BackfillReport, error) {
	sellers, err := loadProfiles(ctx, pool, "sellers")
	if err != nil {
		return nil, err
	}
	suppliers, err := loadProfiles(ctx, pool, "suppliers")
	if err != nil {
		return nil, err
	}
	affiliations, err := loadAffiliations(ctx, pool)
	if err != nil {
		return nil, err
	}
	sellerOwners, err := loadOwners(ctx, pool, "seller_members", "seller_id")
	if err != nil {
		return nil, err
	}
	supplierOwners, err := loadOwners(ctx, pool, "supplier_members", "supplier_id")
	if err != nil {
		return nil, err
	}

	items := classify(sellers, suppliers, affiliations, sellerOwners, supplierOwners)
	if opts.Limit > 0 && opts.Limit < len(items) {
		items = items[:opts.Limit]
	}

	report := &BackfillReport{
		DryRun:      opts.DryRun,
		BatchSize:   opts.BatchSize,
		Limit:       opts.Limit,
		GeneratedAt: time.Now().UTC(),
	}

	for start := 0; start < len(items); start += opts.BatchSize {
		end := start + opts.BatchSize
		if end > len(items) {
			end = len(items)
		}
		if err := processBatch(ctx, pool, opts.DryRun, items[start:end], report); err != nil {
			return nil, err
		}
	}

	return report, nil
}

func loadProfiles(ctx context.Context, pool *pgxpool.Pool, table string) (map[uuid.UUID]profile, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf("SELECT id, code, name, status, merchant_id FROM %s ORDER BY id", table))
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", table, err)
	}
	defer rows.Close()

	out := map[uuid.UUID]profile{}
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Status, &p.MerchantID); err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func loadAffiliations(ctx context.Context, pool *pgxpool.Pool) ([]affiliation, error) {
	rows, err := pool.Query(ctx, "SELECT supplier_id, seller_id FROM supplier_seller_affiliations ORDER BY supplier_id, seller_id")
	if err != nil {
		return nil, fmt.Errorf("load affiliations: %w", err)
	}
	defer rows.Close()

	var out []affiliation
	for rows.Next() {
		var a affiliation
		if err := rows.Scan(&a.SupplierID, &a.SellerID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func loadOwners(ctx context.Context, pool *pgxpool.Pool, table, profileColumn string) (map[uuid.UUID][]string, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		SELECT %s, principal_subject
		FROM %s
		WHERE status = 'active' AND lower(role) LIKE '%%owner%%'
		ORDER BY %s, principal_subject
	`, profileColumn, table, profileColumn))
	if err != nil {
		return nil, fmt.Errorf("load %s owners: %w", table, err)
	}
	defer rows.Close()

	out := map[uuid.UUID][]string{}
	for rows.Next() {
		var profileID uuid.UUID
		var subject string
		if err := rows.Scan(&profileID, &subject); err != nil {
			return nil, err
		}
		out[profileID] = append(out[profileID], subject)
	}
	return out, rows.Err()
}

func classify(sellers, suppliers map[uuid.UUID]profile, affiliations []affiliation, sellerOwners, supplierOwners map[uuid.UUID][]string) []workItem {
	var items []workItem
	seenSellers := map[uuid.UUID]bool{}
	seenSuppliers := map[uuid.UUID]bool{}
	sellerAffCounts := map[uuid.UUID]int{}
	supplierAffCounts := map[uuid.UUID]int{}
	for _, a := range affiliations {
		sellerAffCounts[a.SellerID]++
		supplierAffCounts[a.SupplierID]++
	}

	for _, a := range affiliations {
		seller, sellerOK := sellers[a.SellerID]
		supplier, supplierOK := suppliers[a.SupplierID]
		item := workItem{SourceType: sourceAffiliation}
		if sellerOK {
			item.Seller = &seller
			item.SellerOwners = normalizedSubjects(sellerOwners[seller.ID])
			seenSellers[seller.ID] = true
		}
		if supplierOK {
			item.Supplier = &supplier
			item.SupplierOwners = normalizedSubjects(supplierOwners[supplier.ID])
			seenSuppliers[supplier.ID] = true
		}
		switch {
		case !sellerOK || !supplierOK:
			item.ReasonCode = "ORPHANED_AFFILIATION"
		case sellerAffCounts[a.SellerID] > 1 || supplierAffCounts[a.SupplierID] > 1:
			item.ReasonCode = "MULTIPLE_AFFILIATIONS"
		case !isActive(seller.Status) || !isActive(supplier.Status):
			item.ReasonCode = "INVALID_PROFILE_STATUS"
		case len(item.SellerOwners) == 0 || len(item.SupplierOwners) == 0:
			item.ReasonCode = "MISSING_OWNER"
		case !sameSubjects(item.SellerOwners, item.SupplierOwners):
			item.ReasonCode = "OWNER_SUBJECT_MISMATCH"
		}
		items = append(items, item)
	}

	for _, seller := range sortedProfiles(sellers) {
		if seenSellers[seller.ID] {
			continue
		}
		item := workItem{SourceType: sourceSeller, Seller: &seller, SellerOwners: normalizedSubjects(sellerOwners[seller.ID])}
		if !isActive(seller.Status) {
			item.ReasonCode = "INVALID_PROFILE_STATUS"
		} else if len(item.SellerOwners) == 0 {
			item.ReasonCode = "MISSING_OWNER"
		}
		items = append(items, item)
	}

	for _, supplier := range sortedProfiles(suppliers) {
		if seenSuppliers[supplier.ID] {
			continue
		}
		item := workItem{SourceType: sourceSupplier, Supplier: &supplier, SupplierOwners: normalizedSubjects(supplierOwners[supplier.ID])}
		if !isActive(supplier.Status) {
			item.ReasonCode = "INVALID_PROFILE_STATUS"
		} else if len(item.SupplierOwners) == 0 {
			item.ReasonCode = "MISSING_OWNER"
		}
		items = append(items, item)
	}

	return items
}

func processBatch(ctx context.Context, pool *pgxpool.Pool, dryRun bool, items []workItem, report *BackfillReport) error {
	if dryRun {
		for _, item := range items {
			classifyOnly(item, report)
		}
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	for _, item := range items {
		if err := applyItem(ctx, tx, item, report); err != nil {
			report.Metrics.FailedProfiles += item.profileCount()
			return err
		}
	}

	return tx.Commit(ctx)
}

func classifyOnly(item workItem, report *BackfillReport) {
	report.Metrics.TotalScanned += item.profileCount()
	if item.ReasonCode != "" {
		report.Metrics.QuarantinedProfiles += item.profileCount()
		report.AuditEntries = append(report.AuditEntries, quarantineAudit(item, true))
		return
	}
	report.Metrics.MappedProfiles += item.profileCount()
	report.AuditEntries = append(report.AuditEntries, mappedAudit(item, deterministicMerchantID(item), true))
}

func applyItem(ctx context.Context, tx pgx.Tx, item workItem, report *BackfillReport) error {
	report.Metrics.TotalScanned += item.profileCount()
	if item.alreadyLinked() {
		report.Metrics.MappedProfiles += item.profileCount()
		report.Metrics.RetriedProfiles += item.profileCount()
		report.AuditEntries = append(report.AuditEntries, mappedAudit(item, deterministicMerchantID(item), false))
		return nil
	}
	if item.ReasonCode != "" {
		if err := recordQuarantine(ctx, tx, item); err != nil {
			return err
		}
		report.Metrics.QuarantinedProfiles += item.profileCount()
		report.AuditEntries = append(report.AuditEntries, quarantineAudit(item, false))
		return nil
	}

	merchantID := deterministicMerchantID(item)
	if err := upsertMerchant(ctx, tx, merchantID, item); err != nil {
		return err
	}
	if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantCreated, "merchant", merchantID.String(), merchantID, map[string]any{
		"merchant_id": merchantID.String(),
		"source_type": item.SourceType,
	}); err != nil {
		return err
	}
	if err := upsertCapabilities(ctx, tx, merchantID, item); err != nil {
		return err
	}
	if item.Seller != nil {
		if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantCapabilityActivated, "merchant", merchantID.String(), merchantID, map[string]any{
			"merchant_id":       merchantID.String(),
			"capability_type":   "RETAIL",
			"capability_status": "active",
		}); err != nil {
			return err
		}
	}
	if item.Supplier != nil {
		if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantCapabilityActivated, "merchant", merchantID.String(), merchantID, map[string]any{
			"merchant_id":       merchantID.String(),
			"capability_type":   "SUPPLY",
			"capability_status": "active",
		}); err != nil {
			return err
		}
	}
	if item.Seller != nil {
		if _, err := tx.Exec(ctx, "UPDATE sellers SET merchant_id = $2 WHERE id = $1 AND merchant_id IS NULL", item.Seller.ID, merchantID); err != nil {
			return err
		}
		if err := recordCrosswalk(ctx, tx, sourceSeller, &item.Seller.ID, nil, merchantID); err != nil {
			return err
		}
		if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantProfileLinked, "merchant", merchantID.String(), merchantID, map[string]any{
			"merchant_id":  merchantID.String(),
			"profile_type": "SELLER",
			"profile_id":   item.Seller.ID.String(),
		}); err != nil {
			return err
		}
	}
	if item.Supplier != nil {
		if _, err := tx.Exec(ctx, "UPDATE suppliers SET merchant_id = $2 WHERE id = $1 AND merchant_id IS NULL", item.Supplier.ID, merchantID); err != nil {
			return err
		}
		if err := recordCrosswalk(ctx, tx, sourceSupplier, nil, &item.Supplier.ID, merchantID); err != nil {
			return err
		}
		if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantProfileLinked, "merchant", merchantID.String(), merchantID, map[string]any{
			"merchant_id":  merchantID.String(),
			"profile_type": "SUPPLIER",
			"profile_id":   item.Supplier.ID.String(),
		}); err != nil {
			return err
		}
	}
	if err := upsertMemberships(ctx, tx, merchantID, item); err != nil {
		return err
	}

	report.Metrics.MappedProfiles += item.profileCount()
	report.AuditEntries = append(report.AuditEntries, mappedAudit(item, merchantID, false))
	return nil
}

func enqueueBackfillEvent(ctx context.Context, tx pgx.Tx, eventType, aggregateType, aggregateID string, merchantID uuid.UUID, payload map[string]any) error {
	eventID := deterministicUUID("event:" + eventType + ":" + aggregateID + ":" + fmt.Sprint(payload))
	payload["merchant_scope"] = merchantID.String()
	return outbox.NewStore().Enqueue(ctx, tx, events.EventEnvelope{
		EventID:          eventID.String(),
		EventType:        eventType,
		SchemaVersion:    1,
		AggregateType:    aggregateType,
		AggregateID:      aggregateID,
		AggregateVersion: 1,
		CorrelationID:    "merchant-backfill",
		CausationID:      "merchant-backfill",
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	})
}

func upsertMerchant(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, item workItem) error {
	code := "mer_" + strings.ReplaceAll(merchantID.String()[:13], "-", "")
	name := "Backfilled Merchant"
	if item.Seller != nil {
		name = item.Seller.Name
	} else if item.Supplier != nil {
		name = item.Supplier.Name
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO merchants (id, code, legal_name, status)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (id) DO NOTHING
	`, merchantID, code, name)
	return err
}

func upsertCapabilities(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, item workItem) error {
	if item.Seller != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO merchant_capabilities (id, merchant_id, capability_type, status, activated_at)
			VALUES ($1, $2, 'RETAIL', 'active', now())
			ON CONFLICT (merchant_id, capability_type) DO UPDATE SET status = 'active', activated_at = COALESCE(merchant_capabilities.activated_at, now()), updated_at = now()
		`, deterministicUUID("capability:retail:"+merchantID.String()), merchantID); err != nil {
			return err
		}
	}
	if item.Supplier != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO merchant_capabilities (id, merchant_id, capability_type, status, activated_at)
			VALUES ($1, $2, 'SUPPLY', 'active', now())
			ON CONFLICT (merchant_id, capability_type) DO UPDATE SET status = 'active', activated_at = COALESCE(merchant_capabilities.activated_at, now()), updated_at = now()
		`, deterministicUUID("capability:supply:"+merchantID.String()), merchantID); err != nil {
			return err
		}
	}
	return nil
}

func upsertMemberships(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, item workItem) error {
	grants := membershipGrants(item)
	subjects := sortedGrantSubjects(grants)
	for _, subject := range subjects {
		membershipID := deterministicUUID("membership:" + merchantID.String() + ":" + subject)
		if _, err := tx.Exec(ctx, `
			INSERT INTO merchant_memberships (id, merchant_id, principal_subject, status)
			VALUES ($1, $2, $3, 'active')
			ON CONFLICT (merchant_id, principal_subject) DO UPDATE SET status = 'active', updated_at = now()
		`, membershipID, merchantID, subject); err != nil {
			return err
		}
		for _, permission := range grants[subject] {
			if _, err := tx.Exec(ctx, `
				INSERT INTO merchant_membership_permissions (id, membership_id, permission_code)
				VALUES ($1, $2, $3)
				ON CONFLICT (membership_id, permission_code) DO NOTHING
			`, deterministicUUID("permission:"+membershipID.String()+":"+permission), membershipID, permission); err != nil {
				return err
			}
		}
		if err := enqueueBackfillEvent(ctx, tx, merchants.EventTypeMerchantMembershipUpdated, "merchant", merchantID.String(), merchantID, map[string]any{
			"merchant_id":        merchantID.String(),
			"membership_id":      membershipID.String(),
			"principal_subject":  subject,
			"membership_status":  "active",
			"permission_codes":   grants[subject],
			"idempotency_source": "merchant-backfill",
		}); err != nil {
			return err
		}
	}
	return nil
}

func membershipGrants(item workItem) map[string][]string {
	grants := map[string]map[string]struct{}{}
	add := func(subject string, permissions ...string) {
		if grants[subject] == nil {
			grants[subject] = map[string]struct{}{}
		}
		for _, permission := range permissions {
			grants[subject][permission] = struct{}{}
		}
	}
	for _, subject := range item.SellerOwners {
		add(subject,
			merchants.PermissionMerchantManage,
			merchants.PermissionTeamManage,
			merchants.PermissionRetailStoresManage,
			merchants.PermissionRetailCatalogManage,
			merchants.PermissionRetailOrdersManage,
			merchants.PermissionRetailInventoryManage,
		)
	}
	for _, subject := range item.SupplierOwners {
		add(subject,
			merchants.PermissionMerchantManage,
			merchants.PermissionTeamManage,
			merchants.PermissionSupplyCatalogManage,
			merchants.PermissionSupplyInventoryManage,
			merchants.PermissionSupplyFulfillment,
			merchants.PermissionSupplyShippingManage,
		)
	}

	out := map[string][]string{}
	for subject, permissions := range grants {
		for permission := range permissions {
			out[subject] = append(out[subject], permission)
		}
		sort.Strings(out[subject])
	}
	return out
}

func sortedGrantSubjects(grants map[string][]string) []string {
	subjects := make([]string, 0, len(grants))
	for subject := range grants {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	return subjects
}

func recordCrosswalk(ctx context.Context, tx pgx.Tx, sourceType string, sellerID, supplierID *uuid.UUID, merchantID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO merchant_migration_crosswalk (id, source_type, source_seller_id, source_supplier_id, merchant_id, migrated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT DO NOTHING
	`, deterministicUUID(fmt.Sprintf("crosswalk:%s:%v:%v", sourceType, idString(sellerID), idString(supplierID))), sourceType, sellerID, supplierID, merchantID)
	return err
}

func recordQuarantine(ctx context.Context, tx pgx.Tx, item workItem) error {
	audit := quarantineAudit(item, false)
	details, err := json.Marshal(audit)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO merchant_migration_quarantine (id, source_type, source_seller_id, source_supplier_id, reason_code, details, quarantined_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, now())
		ON CONFLICT DO NOTHING
	`, deterministicUUID(fmt.Sprintf("quarantine:%s:%v:%v:%s", item.SourceType, idString(item.sellerID()), idString(item.supplierID()), item.ReasonCode)),
		item.SourceType, item.sellerID(), item.supplierID(), item.ReasonCode, string(details))
	return err
}

func (w workItem) profileCount() int {
	count := 0
	if w.Seller != nil {
		count++
	}
	if w.Supplier != nil {
		count++
	}
	return count
}

func (w workItem) alreadyLinked() bool {
	if w.Seller != nil && w.Seller.MerchantID == nil {
		return false
	}
	if w.Supplier != nil && w.Supplier.MerchantID == nil {
		return false
	}
	return w.Seller != nil || w.Supplier != nil
}

func (w workItem) sellerID() *uuid.UUID {
	if w.Seller == nil {
		return nil
	}
	return &w.Seller.ID
}

func (w workItem) supplierID() *uuid.UUID {
	if w.Supplier == nil {
		return nil
	}
	return &w.Supplier.ID
}

func mappedAudit(item workItem, merchantID uuid.UUID, dryRun bool) AuditEntry {
	return AuditEntry{
		Action:           "map",
		SourceType:       item.SourceType,
		SourceSellerID:   item.sellerID(),
		SourceSupplierID: item.supplierID(),
		MerchantID:       merchantID,
		DryRun:           dryRun,
	}
}

func quarantineAudit(item workItem, dryRun bool) AuditEntry {
	return AuditEntry{
		Action:           "quarantine",
		SourceType:       item.SourceType,
		SourceSellerID:   item.sellerID(),
		SourceSupplierID: item.supplierID(),
		ReasonCode:       item.ReasonCode,
		DryRun:           dryRun,
	}
}

func deterministicMerchantID(item workItem) uuid.UUID {
	parts := []string{item.SourceType}
	if item.Seller != nil {
		parts = append(parts, "seller:"+item.Seller.ID.String())
	}
	if item.Supplier != nil {
		parts = append(parts, "supplier:"+item.Supplier.ID.String())
	}
	return deterministicUUID(strings.Join(parts, ":"))
}

func deterministicUUID(key string) uuid.UUID {
	return uuid.NewSHA1(merchantNamespace, []byte(key))
}

func sortedProfiles(in map[uuid.UUID]profile) []profile {
	out := make([]profile, 0, len(in))
	for _, p := range in {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

func isActive(status string) bool {
	return strings.EqualFold(status, "active")
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func normalizedSubjects(subjects []string) []string {
	seen := map[string]struct{}{}
	for _, subject := range subjects {
		trimmed := strings.TrimSpace(subject)
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for subject := range seen {
		out = append(out, subject)
	}
	sort.Strings(out)
	return out
}

func sameSubjects(a, b []string) bool {
	a = normalizedSubjects(a)
	b = normalizedSubjects(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var errNoProfiles = errors.New("no profiles in work item")
