package marketplace_finance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/marketplace_finance"
	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupMarketplaceFinanceDB(t *testing.T) (*database.Pool, finance.Service, settlement.Service, marketplace_finance.Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	ctx := context.Background()

	migrations := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000007_theme_engine_schema",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000012_order_aggregate_schema",
		"000013_outbox_publish_claims",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000016_catalog_invariants",
		"000017_create_shipping_schema",
		"000018_supplier_retail_affiliation",
		"000019_create_payments_schema",
		"000020_create_ledger_schema",
		"000021_create_balance_projection_schema",
		"000022_create_settlement_calculation_schema",
		"000023_create_marketplace_financial_rules_schema",
	}

	for _, name := range migrations {
		path := filepath.Join("..", "..", "migrations", name+".up.sql")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	finRepo := finance.NewRepository(db.Pool)
	finService := finance.NewService(finRepo)

	balRepo := balance.NewRepository(db.Pool)
	balService := balance.NewService(balRepo)

	settleRepo := settlement.NewRepository(db.Pool)
	settleService := settlement.NewService(settleRepo, balService)

	mfRepo := marketplace_finance.NewRepository(db.Pool)
	mfService := marketplace_finance.NewService(mfRepo)

	return db, finService, settleService, mfService, ctx
}

func TestFinancialRulesDatabaseIntegration(t *testing.T) {
	_, _, _, mfService, ctx := setupMarketplaceFinanceDB(t)

	rule, err := mfService.CreateRule(ctx, marketplace_finance.CreateRuleParams{
		Name:           "Platform Fee SAR",
		RuleType:       marketplace_finance.RuleTypePercentage,
		Percentage:     15.0,
		Currency:       "SAR",
		AllocationType: marketplace_finance.AllocationTypePlatformShare,
		Status:         marketplace_finance.RuleStatusActive,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if rule.ID == "" {
		t.Fatal("expected rule ID to be generated")
	}

	rules, err := mfService.ListRules(ctx, "ACTIVE")
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}

	if len(rules) == 0 {
		t.Fatal("expected at least 1 rule")
	}
}

func TestSettlementAllocationIntegration(t *testing.T) {
	db, finService, _, mfService, ctx := setupMarketplaceFinanceDB(t)

	// Create ledger account
	acc, err := finService.CreateAccount(ctx, finance.CreateAccountParams{
		AccountCode: "SELLER-MF-001",
		Name:        "Seller MF Account",
		AccountType: finance.AccountTypeLiability,
		Currency:    "SAR",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	periodID := uuid.NewString()
	settlementID := uuid.NewString()

	// Insert settlement period and settlement
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO settlement_periods (id, start_date, end_date, status, created_at)
		VALUES ($1, NOW(), NOW() + INTERVAL '1 day', 'OPEN', NOW())
	`, periodID)
	if err != nil {
		t.Fatalf("insert period: %v", err)
	}

	gross := int64(10000)
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO settlements (id, settlement_period_id, account_id, currency, gross_amount_minor, adjustment_amount_minor, net_amount_minor, status, created_at)
		VALUES ($1, $2, $3, 'SAR', $4, 0, $4, 'CALCULATED', NOW())
	`, settlementID, periodID, acc.ID, gross)
	if err != nil {
		t.Fatalf("insert settlement: %v", err)
	}

	// Create Platform Fee Rule
	_, err = mfService.CreateRule(ctx, marketplace_finance.CreateRuleParams{
		Name:           "Platform Fee 10%",
		RuleType:       marketplace_finance.RuleTypePercentage,
		Percentage:     10.0,
		Currency:       "SAR",
		AllocationType: marketplace_finance.AllocationTypePlatformShare,
		Status:         marketplace_finance.RuleStatusActive,
	})
	if err != nil {
		t.Fatalf("create platform rule: %v", err)
	}

	// Allocate Settlement
	allocs, err := mfService.AllocateSettlement(ctx, marketplace_finance.AllocateSettlementParams{
		SettlementID: settlementID,
	})
	if err != nil {
		t.Fatalf("allocate settlement: %v", err)
	}

	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(allocs))
	}

	var platformAmt, sellerAmt int64
	for _, a := range allocs {
		if a.AllocationType == marketplace_finance.AllocationTypePlatformShare {
			platformAmt = a.AmountMinor
		} else if a.AllocationType == marketplace_finance.AllocationTypeSellerShare {
			sellerAmt = a.AmountMinor
		}
	}

	if platformAmt != 1000 || sellerAmt != 9000 {
		t.Errorf("expected platform 1000 and seller 9000; got platform %d, seller %d", platformAmt, sellerAmt)
	}

	// Verify allocations stored in DB
	storedAllocs, err := mfService.ListAllocations(ctx, settlementID)
	if err != nil {
		t.Fatalf("list allocations: %v", err)
	}
	if len(storedAllocs) != 2 {
		t.Fatalf("expected 2 stored allocations, got %d", len(storedAllocs))
	}

	// Verify outbox events enqueued
	var outboxCount int
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events WHERE event_type = 'marketplace_finance.allocation.calculated.v1'
	`).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Errorf("expected 2 outbox events, got %d", outboxCount)
	}
}

func TestFinalizedSettlementImmutabilityTrigger(t *testing.T) {
	db, finService, _, mfService, ctx := setupMarketplaceFinanceDB(t)

	acc, err := finService.CreateAccount(ctx, finance.CreateAccountParams{
		AccountCode: "SELLER-MF-002",
		Name:        "Seller MF Account 2",
		AccountType: finance.AccountTypeLiability,
		Currency:    "SAR",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	periodID := uuid.NewString()
	settlementID := uuid.NewString()

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO settlement_periods (id, start_date, end_date, status, created_at)
		VALUES ($1, NOW(), NOW() + INTERVAL '1 day', 'FINALIZED', NOW())
	`, periodID)
	if err != nil {
		t.Fatalf("insert period: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO settlements (id, settlement_period_id, account_id, currency, gross_amount_minor, adjustment_amount_minor, net_amount_minor, status, created_at, finalized_at)
		VALUES ($1, $2, $3, 'SAR', 5000, 0, 5000, 'FINALIZED', NOW(), NOW())
	`, settlementID, periodID, acc.ID)
	if err != nil {
		t.Fatalf("insert finalized settlement: %v", err)
	}

	// Allocate should be rejected for finalized settlement
	_, err = mfService.AllocateSettlement(ctx, marketplace_finance.AllocateSettlementParams{
		SettlementID: settlementID,
	})
	if err != marketplace_finance.ErrSettlementFinalized {
		t.Fatalf("expected ErrSettlementFinalized, got %v", err)
	}
}
