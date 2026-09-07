package finance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupFinanceDB(t *testing.T) (*database.Pool, finance.Service, context.Context) {
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

	repo := finance.NewRepository(db.Pool)
	service := finance.NewService(repo)
	return db, service, ctx
}

func TestFinanceLedgerIntegration(t *testing.T) {
	db, service, ctx := setupFinanceDB(t)

	// 1. Create Ledger Accounts
	accAsset, err := service.CreateAccount(ctx, finance.CreateAccountParams{
		AccountCode: "1010-" + uuid.NewString()[:8],
		Name:        "Cash Asset",
		AccountType: finance.AccountTypeAsset,
		Currency:    "SAR",
	})
	if err != nil {
		t.Fatalf("create asset account: %v", err)
	}

	accRevenue, err := service.CreateAccount(ctx, finance.CreateAccountParams{
		AccountCode: "4010-" + uuid.NewString()[:8],
		Name:        "Sales Revenue",
		AccountType: finance.AccountTypeRevenue,
		Currency:    "SAR",
	})
	if err != nil {
		t.Fatalf("create revenue account: %v", err)
	}

	// 2. Post Balanced Journal Entry
	refID := uuid.NewString()
	entry, err := service.PostJournalEntry(ctx, finance.PostJournalEntryParams{
		ReferenceType: "PAYMENT",
		ReferenceID:   refID,
		Description:   "Payment capture ledger record",
		Currency:      "SAR",
		Lines: []finance.CreateJournalLineParams{
			{AccountID: accAsset.ID, DebitAmountMinor: 15000, CreditAmountMinor: 0},
			{AccountID: accRevenue.ID, DebitAmountMinor: 0, CreditAmountMinor: 15000},
		},
		CorrelationID: "corr-100",
		CausationID:   "caus-100",
	})
	if err != nil {
		t.Fatalf("post journal entry: %v", err)
	}

	if entry.ID == "" {
		t.Fatal("expected non-empty entry id")
	}

	// Fetch Posted Journal Entry
	fetched, err := service.GetJournalEntry(ctx, entry.ID)
	if err != nil {
		t.Fatalf("get journal entry: %v", err)
	}
	if len(fetched.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(fetched.Lines))
	}

	// Verify Outbox Event Enqueued
	var outboxCount int
	err = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'ledger.journal_entry.posted.v1'
	`, entry.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("query outbox count: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", outboxCount)
	}

	// 3. Test Duplicate Posting Protection
	_, err = service.PostJournalEntry(ctx, finance.PostJournalEntryParams{
		ReferenceType: "PAYMENT",
		ReferenceID:   refID, // Same reference!
		Description:   "Duplicate payment capture",
		Currency:      "SAR",
		Lines: []finance.CreateJournalLineParams{
			{AccountID: accAsset.ID, DebitAmountMinor: 15000, CreditAmountMinor: 0},
			{AccountID: accRevenue.ID, DebitAmountMinor: 0, CreditAmountMinor: 15000},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate posting error, got nil")
	}

	// 4. Test Unbalanced Entry Rejection
	_, err = service.PostJournalEntry(ctx, finance.PostJournalEntryParams{
		ReferenceType: "ORDER",
		ReferenceID:   uuid.NewString(),
		Description:   "Unbalanced entry",
		Currency:      "SAR",
		Lines: []finance.CreateJournalLineParams{
			{AccountID: accAsset.ID, DebitAmountMinor: 15000, CreditAmountMinor: 0},
			{AccountID: accRevenue.ID, DebitAmountMinor: 0, CreditAmountMinor: 10000},
		},
	})
	if err == nil {
		t.Fatal("expected unbalanced entry error, got nil")
	}

	// 5. Test DB Immutability Enforcement (Triggers)
	_, err = db.Pool.Exec(ctx, `UPDATE journal_entries SET description = 'modified' WHERE id = $1`, entry.ID)
	if err == nil {
		t.Fatal("expected UPDATE on journal_entries to fail due to immutability trigger")
	}

	_, err = db.Pool.Exec(ctx, `DELETE FROM journal_entries WHERE id = $1`, entry.ID)
	if err == nil {
		t.Fatal("expected DELETE on journal_entries to fail due to immutability trigger")
	}

	_, err = db.Pool.Exec(ctx, `DELETE FROM journal_lines WHERE journal_entry_id = $1`, entry.ID)
	if err == nil {
		t.Fatal("expected DELETE on journal_lines to fail due to immutability trigger")
	}
}
