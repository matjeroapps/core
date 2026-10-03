package merchants_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/merchants"
	"core/internal/testdb"
	"core/packages/database"
)

// setupBootstrapTestDB applies the merchant/integration migrations needed by
// the bootstrap read path (including the subject index added by Feature 025).
func setupBootstrapTestDB(t *testing.T) *database.Pool {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for database-backed bootstrap tests")
	}
	db := testdb.Open(t, dbURL)
	paths := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000009_supplier_retail_capability",
		"000018_supplier_retail_affiliation",
		"000027_integration_foundation",
		"000028_supplier_integrations",
		"000029_seller_integrations",
		"000034_merchant_identity_foundation",
		"000035_seller_supplier_merchant_linkage",
		"000036_merchant_profile_cardinality",
		"000037_merchant_owned_integration_connections",
		"000038_merchant_integration_external_account_lifecycle",
		"000039_integration_supply_staging",
		"000040_merchant_membership_subject_index",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)
	return db
}

func bootstrapMerchant(t *testing.T, svc *merchants.Service, code string, initial merchants.CapabilityType) *merchants.Merchant {
	t.Helper()
	m, err := svc.CreateMerchant(context.Background(), code, "Bootstrap Merchant "+code, initial)
	if err != nil {
		t.Fatalf("create merchant %s: %v", code, err)
	}
	return m
}

func bootstrapMember(t *testing.T, svc *merchants.Service, merchantID uuid.UUID, subject string, perms []string) *merchants.MerchantMembership {
	t.Helper()
	mem, err := svc.AddMember(context.Background(), merchantID, subject, perms)
	if err != nil {
		t.Fatalf("add member %s: %v", subject, err)
	}
	return mem
}

func setMembershipStatus(t *testing.T, db *database.Pool, membershipID uuid.UUID, status string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		"UPDATE merchant_memberships SET status = $2, updated_at = NOW() WHERE id = $1", membershipID, status); err != nil {
		t.Fatalf("set membership status: %v", err)
	}
}

func setMerchantStatus(t *testing.T, db *database.Pool, merchantID uuid.UUID, status string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		"UPDATE merchants SET status = $2, updated_at = NOW() WHERE id = $1", merchantID, status); err != nil {
		t.Fatalf("set merchant status: %v", err)
	}
}

// bootstrapSeller inserts one seller profile linked to the merchant (a merchant
// has at most one active seller profile) and returns its ID.
func bootstrapSeller(t *testing.T, db *database.Pool, merchantID uuid.UUID) uuid.UUID {
	t.Helper()
	sellerID := uuid.New()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO sellers (id, code, name, status, merchant_id, created_at, updated_at)
		VALUES ($1, $2, 'Bootstrap Seller', 'active', $3, NOW(), NOW())
	`, sellerID, "BS-"+sellerID.String()[:8], merchantID); err != nil {
		t.Fatalf("insert seller: %v", err)
	}
	return sellerID
}

// bootstrapStoreForSeller inserts one store under an existing seller profile.
func bootstrapStoreForSeller(t *testing.T, db *database.Pool, sellerID uuid.UUID, code string) uuid.UUID {
	t.Helper()
	storeID := uuid.New()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO stores (id, seller_id, market_code, code, name, status, created_at, updated_at)
		VALUES ($1, $2, 'EG', $3, $4, 'active', NOW(), NOW())
	`, storeID, sellerID, "ST-"+code, "Bootstrap Store "+code); err != nil {
		t.Fatalf("insert store: %v", err)
	}
	return storeID
}

// bootstrapStore inserts a seller profile linked to the merchant plus one store,
// returning the store ID.
func bootstrapStore(t *testing.T, db *database.Pool, merchantID uuid.UUID, code string) uuid.UUID {
	t.Helper()
	sellerID := bootstrapSeller(t, db, merchantID)
	return bootstrapStoreForSeller(t, db, sellerID, code)
}

func insertOpenReviewCase(t *testing.T, db *database.Pool, merchantID, connectionID uuid.UUID, reasonCode string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `
		INSERT INTO merchant_integration_review_cases (id, merchant_id, connection_id, case_type, status, reason_code, details)
		VALUES ($1, $2, $3, 'DUPLICATE_SKU', 'OPEN', $4, '{}'::jsonb)
	`, uuid.New(), merchantID, connectionID, reasonCode); err != nil {
		t.Fatalf("insert review case: %v", err)
	}
}

func insertConnectionWithHealth(t *testing.T, db *database.Pool, merchantID uuid.UUID, name, status, health string) uuid.UUID {
	t.Helper()
	connID := uuid.New()
	extAccount := "ext-" + connID.String()[:12]
	if _, err := db.Exec(context.Background(), `
		INSERT INTO merchant_integration_connections (id, merchant_id, connection_type, provider, external_account_id, name, status, health_status)
		VALUES ($1, $2, 'SUPPLY_SOURCE', 'salla', $3, $4, $5, $6)
	`, connID, merchantID, extAccount, name, status, health); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	return connID
}

func TestBootstrapSingleActiveRetailMembership(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchant := bootstrapMerchant(t, mSvc, "BOOT-A", merchants.CapabilityTypeRetail)
	subject := "boot-subject-single"
	bootstrapMember(t, mSvc, merchant.ID, subject, []string{merchants.PermissionRetailStoresManage})
	storeID := bootstrapStore(t, db, merchant.ID, "A1")
	_ = storeID

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if bs.Subject != subject {
		t.Errorf("expected subject %s, got %s", subject, bs.Subject)
	}
	if bs.Meta.ContractVersion != "1" {
		t.Errorf("expected contract version 1, got %s", bs.Meta.ContractVersion)
	}
	if bs.Meta.GeneratedAt.IsZero() {
		t.Errorf("expected generated_at to be set")
	}
	if len(bs.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(bs.Workspaces))
	}
	ws := bs.Workspaces[0]
	if ws.MerchantID != merchant.ID {
		t.Errorf("expected merchant %s, got %s", merchant.ID, ws.MerchantID)
	}
	if ws.MerchantStatus != merchants.MerchantStatusActive {
		t.Errorf("expected active merchant, got %s", ws.MerchantStatus)
	}
	if ws.Membership.Status != merchants.MembershipStatusActive {
		t.Errorf("expected active membership, got %s", ws.Membership.Status)
	}
	if len(ws.Membership.Permissions) != 1 || ws.Membership.Permissions[0] != merchants.PermissionRetailStoresManage {
		t.Errorf("expected effective permissions [retail.stores.manage], got %v", ws.Membership.Permissions)
	}
	if ws.Capabilities == nil || ws.Capabilities.Retail == nil || !ws.Capabilities.Retail.IsActive() {
		t.Errorf("expected active retail capability, got %+v", ws.Capabilities)
	}
	if ws.Capabilities.Supply == nil || ws.Capabilities.Supply.IsActive() {
		t.Errorf("expected inactive supply capability, got %+v", ws.Capabilities)
	}
	if len(ws.Stores) != 1 {
		t.Fatalf("expected 1 store summary, got %d", len(ws.Stores))
	}
	if ws.Stores[0].MarketCode != "EG" {
		t.Errorf("expected market_code EG, got %s", ws.Stores[0].MarketCode)
	}
	if ws.PlanSummary == nil || ws.PlanSummary.State != merchants.PlanStateNoPlanModel {
		t.Errorf("expected explicit NO_PLAN_MODEL plan state, got %+v", ws.PlanSummary)
	}
	if ws.PendingActions == nil {
		t.Fatalf("expected pending actions block")
	}
	if ws.PendingActions.OpenReviewCases != 0 || ws.PendingActions.UnhealthyConnections != 0 {
		t.Errorf("expected zero pending actions, got %+v", ws.PendingActions)
	}
}

func TestBootstrapMultipleMerchants(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchantA := bootstrapMerchant(t, mSvc, "BOOT-M1", merchants.CapabilityTypeRetail)
	merchantB := bootstrapMerchant(t, mSvc, "BOOT-M2", merchants.CapabilityTypeSupply)
	subject := "boot-subject-multi"
	bootstrapMember(t, mSvc, merchantA.ID, subject, []string{merchants.PermissionRetailStoresManage})
	bootstrapMember(t, mSvc, merchantB.ID, subject, []string{merchants.PermissionSupplyCatalogManage})

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(bs.Workspaces))
	}
	seen := map[uuid.UUID]merchants.MerchantWorkspace{}
	for _, ws := range bs.Workspaces {
		seen[ws.MerchantID] = ws
	}
	wsA, okA := seen[merchantA.ID]
	wsB, okB := seen[merchantB.ID]
	if !okA || !okB {
		t.Fatalf("expected both merchants in workspaces, got %v", seen)
	}
	if wsA.Capabilities == nil || !wsA.Capabilities.Retail.IsActive() {
		t.Errorf("expected active retail on %s", merchantA.ID)
	}
	if wsB.Capabilities == nil || !wsB.Capabilities.Supply.IsActive() {
		t.Errorf("expected active supply on %s", merchantB.ID)
	}
}

func TestBootstrapNoMembershipYieldsEmptyWorkspaces(t *testing.T) {
	db := setupBootstrapTestDB(t)

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	bootstrapMerchant(t, mSvc, "BOOT-NONE", merchants.CapabilityTypeRetail)

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(context.Background(), "boot-subject-nobody")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 0 {
		t.Fatalf("expected empty workspaces, got %d", len(bs.Workspaces))
	}
}

func TestBootstrapInvitedAndSuspendedMembershipLeakNothing(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	invitedMerchant := bootstrapMerchant(t, mSvc, "BOOT-INV", merchants.CapabilityTypeRetail)
	suspendedMerchant := bootstrapMerchant(t, mSvc, "BOOT-SUS", merchants.CapabilityTypeSupply)

	invitedMem := bootstrapMember(t, mSvc, invitedMerchant.ID, "boot-subject-invited", []string{merchants.PermissionRetailStoresManage})
	suspendedMem := bootstrapMember(t, mSvc, suspendedMerchant.ID, "boot-subject-suspended", []string{merchants.PermissionSupplyCatalogManage})
	setMembershipStatus(t, db, invitedMem.ID, "invited")
	setMembershipStatus(t, db, suspendedMem.ID, "suspended")
	bootstrapStore(t, db, invitedMerchant.ID, "INV1")
	connID := insertConnectionWithHealth(t, db, suspendedMerchant.ID, "sus conn", "ERROR", "healthy")
	insertOpenReviewCase(t, db, suspendedMerchant.ID, connID, "DUPLICATE_SKU")

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))

	invitedBS, err := svc.Build(ctx, "boot-subject-invited")
	if err != nil {
		t.Fatalf("bootstrap invited: %v", err)
	}
	if len(invitedBS.Workspaces) != 1 {
		t.Fatalf("expected 1 invited workspace, got %d", len(invitedBS.Workspaces))
	}
	inv := invitedBS.Workspaces[0]
	if inv.Membership.Status != merchants.MembershipStatusInvited {
		t.Errorf("expected invited status, got %s", inv.Membership.Status)
	}
	if inv.Capabilities != nil {
		t.Errorf("invited membership must not expose capability details, got %+v", inv.Capabilities)
	}
	if len(inv.Stores) != 0 {
		t.Errorf("invited membership must not expose stores, got %d", len(inv.Stores))
	}
	if inv.PlanSummary != nil || inv.PendingActions != nil {
		t.Errorf("invited membership must not expose plan/pending details, got %+v / %+v", inv.PlanSummary, inv.PendingActions)
	}

	suspendedBS, err := svc.Build(ctx, "boot-subject-suspended")
	if err != nil {
		t.Fatalf("bootstrap suspended: %v", err)
	}
	if len(suspendedBS.Workspaces) != 1 {
		t.Fatalf("expected 1 suspended workspace, got %d", len(suspendedBS.Workspaces))
	}
	sus := suspendedBS.Workspaces[0]
	if sus.Membership.Status != merchants.MembershipStatusSuspended {
		t.Errorf("expected suspended status, got %s", sus.Membership.Status)
	}
	if sus.Capabilities != nil || len(sus.Stores) != 0 || sus.PlanSummary != nil || sus.PendingActions != nil {
		t.Errorf("suspended membership must not leak capability/store/plan/pending data: %+v", sus)
	}
}

func TestBootstrapSuspendedMerchantIsNotOperable(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchant := bootstrapMerchant(t, mSvc, "BOOT-SUSM", merchants.CapabilityTypeRetail)
	subject := "boot-subject-susm"
	bootstrapMember(t, mSvc, merchant.ID, subject, []string{merchants.PermissionRetailStoresManage})
	setMerchantStatus(t, db, merchant.ID, "suspended")

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(bs.Workspaces))
	}
	ws := bs.Workspaces[0]
	if ws.MerchantStatus != merchants.MerchantStatusSuspended {
		t.Errorf("expected suspended merchant status, got %s", ws.MerchantStatus)
	}
	if ws.Capabilities != nil || len(ws.Stores) != 0 || ws.PlanSummary != nil || ws.PendingActions != nil {
		t.Errorf("suspended merchant must not expose operable details: %+v", ws)
	}
}

func TestBootstrapMissingAndSuspendedCapabilities(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	supplyOnly := bootstrapMerchant(t, mSvc, "BOOT-SUP", merchants.CapabilityTypeSupply)
	dualSuspended := bootstrapMerchant(t, mSvc, "BOOT-DUAL", merchants.CapabilityTypeRetail)
	if _, err := mSvc.ActivateCapability(ctx, dualSuspended.ID, merchants.CapabilityTypeSupply); err != nil {
		t.Fatalf("activate supply: %v", err)
	}
	if _, err := mSvc.SuspendCapability(ctx, dualSuspended.ID, merchants.CapabilityTypeSupply); err != nil {
		t.Fatalf("suspend supply: %v", err)
	}

	subject := "boot-subject-caps"
	bootstrapMember(t, mSvc, supplyOnly.ID, subject, []string{merchants.PermissionSupplyCatalogManage})
	bootstrapMember(t, mSvc, dualSuspended.ID, subject, []string{merchants.PermissionSupplyCatalogManage})

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(bs.Workspaces))
	}
	byMerchant := map[uuid.UUID]merchants.MerchantWorkspace{}
	for _, ws := range bs.Workspaces {
		byMerchant[ws.MerchantID] = ws
	}

	sup, ok := byMerchant[supplyOnly.ID]
	if !ok || sup.Capabilities == nil {
		t.Fatalf("expected capabilities for supply-only merchant")
	}
	if !sup.Capabilities.Supply.IsActive() {
		t.Errorf("expected active supply, got %+v", sup.Capabilities.Supply)
	}
	if sup.Capabilities.Retail.IsActive() {
		t.Errorf("expected inactive retail on supply-only merchant, got %+v", sup.Capabilities.Retail)
	}

	dual, ok := byMerchant[dualSuspended.ID]
	if !ok || dual.Capabilities == nil {
		t.Fatalf("expected capabilities for dual merchant")
	}
	if dual.Capabilities.Supply.Status != merchants.CapabilityStatusSuspended {
		t.Errorf("expected suspended supply status, got %s", dual.Capabilities.Supply.Status)
	}
	if !dual.Capabilities.Retail.IsActive() {
		t.Errorf("expected active retail, got %+v", dual.Capabilities.Retail)
	}
}

func TestBootstrapPendingActionsFromRealRows(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchant := bootstrapMerchant(t, mSvc, "BOOT-PEND", merchants.CapabilityTypeSupply)
	subject := "boot-subject-pending"
	bootstrapMember(t, mSvc, merchant.ID, subject, []string{merchants.PermissionSupplyCatalogManage})

	errConn := insertConnectionWithHealth(t, db, merchant.ID, "error conn", "ACTIVE", "healthy")
	if _, err := db.Exec(ctx, "UPDATE merchant_integration_connections SET status = 'ERROR' WHERE id = $1", errConn); err != nil {
		t.Fatalf("set connection error: %v", err)
	}
	degradedConn := insertConnectionWithHealth(t, db, merchant.ID, "degraded conn", "ACTIVE", "degraded")
	pausedConn := insertConnectionWithHealth(t, db, merchant.ID, "paused conn", "PAUSED", "healthy")
	insertOpenReviewCase(t, db, merchant.ID, errConn, "DUPLICATE_SKU")
	insertOpenReviewCase(t, db, merchant.ID, degradedConn, "MAPPING_CONFLICT")
	insertOpenReviewCase(t, db, merchant.ID, pausedConn, "DUPLICATE_SKU")
	// A resolved case must not count.
	if _, err := db.Exec(ctx, `
		UPDATE merchant_integration_review_cases SET status = 'RESOLVED', resolution = 'APPROVED', resolved_by = 'tester', resolved_at = NOW()
		WHERE connection_id = $1 AND reason_code = 'MAPPING_CONFLICT'
	`, degradedConn); err != nil {
		t.Fatalf("resolve case: %v", err)
	}

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(bs.Workspaces))
	}
	pa := bs.Workspaces[0].PendingActions
	if pa == nil {
		t.Fatalf("expected pending actions block")
	}
	if pa.OpenReviewCases != 2 {
		t.Errorf("expected 2 open review cases, got %d", pa.OpenReviewCases)
	}
	// ERROR status + degraded health are actionable; PAUSED/healthy is not.
	if pa.UnhealthyConnections != 2 {
		t.Errorf("expected 2 unhealthy connections, got %d", pa.UnhealthyConnections)
	}
}

func TestBootstrapListMembershipsBySubjectReturnsAllStatuses(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchantA := bootstrapMerchant(t, mSvc, "BOOT-L1", merchants.CapabilityTypeRetail)
	merchantB := bootstrapMerchant(t, mSvc, "BOOT-L2", merchants.CapabilityTypeRetail)
	subject := "boot-subject-list"

	active := bootstrapMember(t, mSvc, merchantA.ID, subject, []string{merchants.PermissionRetailStoresManage})
	invited := bootstrapMember(t, mSvc, merchantB.ID, subject, nil)
	setMembershipStatus(t, db, invited.ID, "invited")

	repo := merchants.NewPostgresRepository(db.Pool)
	mems, err := repo.ListMembershipsBySubject(ctx, subject)
	if err != nil {
		t.Fatalf("ListMembershipsBySubject: %v", err)
	}
	if len(mems) != 2 {
		t.Fatalf("expected 2 memberships (all statuses), got %d", len(mems))
	}
	byMerchant := map[uuid.UUID]merchants.MerchantMembership{}
	for _, mem := range mems {
		byMerchant[mem.MerchantID] = *mem
	}
	if got, ok := byMerchant[merchantA.ID]; !ok || got.Status != merchants.MembershipStatusActive || got.ID != active.ID {
		t.Errorf("expected active membership on %s, got %+v", merchantA.ID, got)
	}
	if got, ok := byMerchant[merchantB.ID]; !ok || got.Status != merchants.MembershipStatusInvited {
		t.Errorf("expected invited membership on %s, got %+v", merchantB.ID, got)
	}
	// Permissions loaded for the active membership.
	if got := byMerchant[merchantA.ID]; len(got.Permissions) != 1 {
		t.Errorf("expected permissions to load with membership, got %v", got.Permissions)
	}
}

func TestBootstrapStoreSummariesScopedToMerchant(t *testing.T) {
	db := setupBootstrapTestDB(t)
	ctx := context.Background()

	mSvc := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchantA := bootstrapMerchant(t, mSvc, "BOOT-S1", merchants.CapabilityTypeRetail)
	merchantB := bootstrapMerchant(t, mSvc, "BOOT-S2", merchants.CapabilityTypeRetail)
	subject := "boot-subject-stores"
	bootstrapMember(t, mSvc, merchantA.ID, subject, []string{merchants.PermissionRetailStoresManage})
	sellerA := bootstrapSeller(t, db, merchantA.ID)
	sellerB := bootstrapSeller(t, db, merchantB.ID)
	bootstrapStoreForSeller(t, db, sellerA, "S1A")
	bootstrapStoreForSeller(t, db, sellerA, "S1B")
	bootstrapStoreForSeller(t, db, sellerB, "S2A")

	svc := merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool))
	bs, err := svc.Build(ctx, subject)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(bs.Workspaces) != 1 {
		t.Fatalf("expected only the member merchant in workspaces, got %d", len(bs.Workspaces))
	}
	stores := bs.Workspaces[0].Stores
	if len(stores) != 2 {
		t.Fatalf("expected 2 stores scoped to the member merchant, got %d", len(stores))
	}
	for _, s := range stores {
		if s.ID == uuid.Nil || s.Code == "" || s.Name == "" || s.Status == "" || s.MarketCode == "" {
			t.Errorf("store summary missing required fields: %+v", s)
		}
	}
}

func TestMerchantMembershipSubjectIndexDownUpReapplication(t *testing.T) {
	db := setupBootstrapTestDB(t)
	downPath := filepath.Join("..", "..", "migrations", "000040_merchant_membership_subject_index.down.sql")
	upPath := filepath.Join("..", "..", "migrations", "000040_merchant_membership_subject_index.up.sql")
	testdb.ApplyMigrations(t, db, downPath)
	testdb.ApplyMigrations(t, db, upPath)

	var count int
	if err := db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = current_schema() AND indexname = 'idx_merchant_memberships_principal_subject'
	`).Scan(&count); err != nil {
		t.Fatalf("query index: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected subject index to exist after reapplication, found %d", count)
	}
}
