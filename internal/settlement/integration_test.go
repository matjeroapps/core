package settlement_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupSettlementDB(t *testing.T) (*database.Pool, finance.Service, balance.Service, settlement.Service, context.Context) {
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

	return db, finService, balService, settleService, ctx
}

func TestSettlementCalculationLifecycleIntegration(t *testing.T) {
	pool, finService, balService, settleService, ctx := setupSettlementDB(t)

	// 1. Create ledger account & balance projection
	acc, err := finService.CreateAccount(ctx, finance.CreateAccountParams{
		AccountCode: "SELLER-SETTLE-001",
		Name:        "Seller Payable Account",
		AccountType: finance.AccountTypeLiability,
		Currency:    "SAR",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Insert projected balance directly via balance upsert tx
	balRepo := balance.NewRepository(pool.Pool)
	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := balRepo.UpsertAccountBalanceTx(ctx, tx, acc.ID, "SAR", 150000, 50000); err != nil {
		t.Fatalf("upsert account balance: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	bal, err := balService.GetAccountBalance(ctx, acc.ID)
	if err != nil {
		t.Fatalf("get account balance: %v", err)
	}
	if bal.BalanceMinor != 100000 {
		t.Fatalf("expected projected balance 100000, got %d", bal.BalanceMinor)
	}

	// 2. Create Settlement Period
	now := time.Now().UTC()
	period, err := balService.CreateSettlementPeriod(ctx, balance.CreateSettlementPeriodParams{
		StartDate: now.Add(-7 * 24 * time.Hour),
		EndDate:   now,
	})
	if err != nil {
		t.Fatalf("create settlement period: %v", err)
	}

	// 3. Calculate Settlement
	settlements, err := settleService.CalculatePeriodSettlements(ctx, settlement.CalculateSettlementParams{
		PeriodID:      period.ID,
		AccountID:     acc.ID,
		CorrelationID: "corr-test-1",
	})
	if err != nil {
		t.Fatalf("calculate period settlements: %v", err)
	}
	if len(settlements) != 1 {
		t.Fatalf("expected 1 settlement, got %d", len(settlements))
	}

	st := settlements[0]
	if st.Status != settlement.StatusCalculated {
		t.Errorf("expected status CALCULATED, got %s", st.Status)
	}
	if st.GrossAmountMinor != 100000 || st.NetAmountMinor != 100000 {
		t.Errorf("expected gross/net 100000, got gross %d net %d", st.GrossAmountMinor, st.NetAmountMinor)
	}

	// Verify outbox event settlement.calculated.v1 was queued
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_events WHERE event_type = $1 AND aggregate_id = $2",
		"settlement.calculated.v1", st.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query outbox calculated event: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 outbox event for settlement.calculated.v1, got %d", count)
	}

	// 4. Finalize Settlement
	finalized, err := settleService.FinalizePeriodSettlements(ctx, settlement.FinalizeSettlementParams{
		PeriodID:      period.ID,
		CorrelationID: "corr-test-2",
	})
	if err != nil {
		t.Fatalf("finalize period settlements: %v", err)
	}
	if len(finalized) != 1 {
		t.Fatalf("expected 1 finalized settlement, got %d", len(finalized))
	}

	stFinal := finalized[0]
	if stFinal.Status != settlement.StatusFinalized {
		t.Errorf("expected status FINALIZED, got %s", stFinal.Status)
	}

	// Verify outbox event settlement.finalized.v1 was queued
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_events WHERE event_type = $1 AND aggregate_id = $2",
		"settlement.finalized.v1", stFinal.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query outbox finalized event: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 outbox event for settlement.finalized.v1, got %d", count)
	}

	// 5. Verify Immutability of Finalized Settlements
	_, err = pool.Exec(ctx, "UPDATE settlements SET net_amount_minor = 99999 WHERE id = $1", stFinal.ID)
	if err == nil {
		t.Fatal("expected database error updating finalized settlement, got nil")
	}

	_, err = pool.Exec(ctx, "DELETE FROM settlements WHERE id = $1", stFinal.ID)
	if err == nil {
		t.Fatal("expected database error deleting finalized settlement, got nil")
	}

	// 6. Attempting to finalize again should fail with ErrSettlementAlreadyFinalized
	_, err = settleService.FinalizePeriodSettlements(ctx, settlement.FinalizeSettlementParams{
		PeriodID: period.ID,
	})
	if err == nil {
		t.Fatal("expected error finalizing already finalized period, got nil")
	}
}
