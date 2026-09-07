package balance_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/events"
)

func setupBalanceDB(t *testing.T) (*database.Pool, finance.Service, balance.Service, balance.Consumer, context.Context) {
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
	financeSvc := finance.NewService(finRepo)

	balRepo := balance.NewRepository(db.Pool)
	balanceSvc := balance.NewService(balRepo)
	consumer := balance.NewConsumer(db.Pool, balRepo)

	return db, financeSvc, balanceSvc, consumer, ctx
}

func TestBalanceProjectionIntegration(t *testing.T) {
	db, financeSvc, balanceSvc, consumer, ctx := setupBalanceDB(t)

	t.Run("Journal Entry Posted Event Updates Balance Projection Correctly", func(t *testing.T) {
		// 1. Create two ledger accounts (Debit asset account, Credit revenue account)
		debitAccountCode := "ACC-DEBIT-" + uuid.NewString()[:6]
		creditAccountCode := "ACC-CREDIT-" + uuid.NewString()[:6]

		acc1, err := financeSvc.CreateAccount(ctx, finance.CreateAccountParams{
			AccountCode: debitAccountCode,
			Name:        "Test Clearing Account",
			AccountType: finance.AccountTypeAsset,
			Currency:    "SAR",
		})
		if err != nil {
			t.Fatalf("create account 1: %v", err)
		}

		acc2, err := financeSvc.CreateAccount(ctx, finance.CreateAccountParams{
			AccountCode: creditAccountCode,
			Name:        "Test Revenue Account",
			AccountType: finance.AccountTypeRevenue,
			Currency:    "SAR",
		})
		if err != nil {
			t.Fatalf("create account 2: %v", err)
		}

		// Initial query should return 0 balances
		bal1, err := balanceSvc.GetAccountBalance(ctx, acc1.ID)
		if err != nil {
			t.Fatalf("get initial balance account 1: %v", err)
		}
		if bal1.DebitTotalMinor != 0 || bal1.CreditTotalMinor != 0 || bal1.BalanceMinor != 0 {
			t.Fatalf("expected 0 initial balance, got %+v", bal1)
		}

		// 2. Post event with 1000 minor units Debit on acc1, 1000 minor units Credit on acc2
		payload := events.JournalEntryPostedPayload{
			JournalEntryID: uuid.NewString(),
			ReferenceType:  "TEST_PAYMENT",
			ReferenceID:    "ref_" + uuid.NewString()[:8],
			Description:    "Test Journal Posting",
			Currency:       "SAR",
			PostedAt:       time.Now().UTC(),
			Lines: []events.JournalEntryPostedLinePayload{
				{
					AccountID:        acc1.ID,
					DebitAmountMinor: 1000,
				},
				{
					AccountID:         acc2.ID,
					CreditAmountMinor: 1000,
				},
			},
		}

		envelope, err := events.NewJournalEntryPostedEvent(payload, "corr-1", "caus-1")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("handle event: %v", err)
		}

		// 3. Verify Account 1 Balance (Debit increases balance)
		bal1, err = balanceSvc.GetAccountBalance(ctx, acc1.ID)
		if err != nil {
			t.Fatalf("get balance account 1: %v", err)
		}
		if bal1.DebitTotalMinor != 1000 || bal1.CreditTotalMinor != 0 || bal1.BalanceMinor != 1000 {
			t.Errorf("unexpected balance for debit account 1: %+v", bal1)
		}

		// 4. Verify Account 2 Balance (Credit decreases balance / increases credit total)
		bal2, err := balanceSvc.GetAccountBalance(ctx, acc2.ID)
		if err != nil {
			t.Fatalf("get balance account 2: %v", err)
		}
		if bal2.DebitTotalMinor != 0 || bal2.CreditTotalMinor != 1000 || bal2.BalanceMinor != -1000 {
			t.Errorf("unexpected balance for credit account 2: %+v", bal2)
		}
	})

	t.Run("Duplicate Event Processing Idempotency", func(t *testing.T) {
		acc, err := financeSvc.CreateAccount(ctx, finance.CreateAccountParams{
			AccountCode: "ACC-IDEM-" + uuid.NewString()[:6],
			Name:        "Idempotency Test Account",
			AccountType: finance.AccountTypeAsset,
			Currency:    "SAR",
		})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}

		payload := events.JournalEntryPostedPayload{
			JournalEntryID: uuid.NewString(),
			ReferenceType:  "TEST_IDEMPOTENCY",
			ReferenceID:    "ref_" + uuid.NewString()[:8],
			Currency:       "SAR",
			PostedAt:       time.Now().UTC(),
			Lines: []events.JournalEntryPostedLinePayload{
				{
					AccountID:        acc.ID,
					DebitAmountMinor: 5000,
				},
			},
		}

		envelope, err := events.NewJournalEntryPostedEvent(payload, "corr-idem", "caus-idem")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		// Handle first time
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("first handle event: %v", err)
		}

		// Handle second time (duplicate event ID)
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("second duplicate handle event failed: %v", err)
		}

		// Balance should be incremented ONLY ONCE (5000, not 10000)
		bal, err := balanceSvc.GetAccountBalance(ctx, acc.ID)
		if err != nil {
			t.Fatalf("get balance: %v", err)
		}
		if bal.DebitTotalMinor != 5000 || bal.BalanceMinor != 5000 {
			t.Fatalf("expected debit 5000 after duplicate processing, got debit=%d, balance=%d", bal.DebitTotalMinor, bal.BalanceMinor)
		}
	})

	t.Run("Rollback on Failure Allows Safe Retry", func(t *testing.T) {
		nonExistentAccountID := uuid.NewString()

		payload := events.JournalEntryPostedPayload{
			JournalEntryID: uuid.NewString(),
			ReferenceType:  "TEST_ROLLBACK",
			ReferenceID:    "ref_" + uuid.NewString()[:8],
			Currency:       "SAR",
			PostedAt:       time.Now().UTC(),
			Lines: []events.JournalEntryPostedLinePayload{
				{
					AccountID:        nonExistentAccountID,
					DebitAmountMinor: 2500,
				},
			},
		}

		envelope, err := events.NewJournalEntryPostedEvent(payload, "corr-rollback", "caus-rollback")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		// Fails due to non-existent account ID
		err = consumer.HandleEvent(ctx, envelope)
		if err == nil {
			t.Fatalf("expected error due to invalid account ID, got nil")
		}

		// Verify processed_events inbox record was rolled back
		var inboxCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM processed_events WHERE consumer_name = $1 AND event_id = $2
		`, balance.ConsumerName, envelope.EventID).Scan(&inboxCount)
		if err != nil {
			t.Fatalf("query inbox: %v", err)
		}
		if inboxCount != 0 {
			t.Fatalf("expected inbox record to be 0 after rollback, got %d", inboxCount)
		}
	})

	t.Run("Concurrent Event Processing Safety", func(t *testing.T) {
		acc, err := financeSvc.CreateAccount(ctx, finance.CreateAccountParams{
			AccountCode: "ACC-CONC-" + uuid.NewString()[:6],
			Name:        "Concurrency Test Account",
			AccountType: finance.AccountTypeAsset,
			Currency:    "SAR",
		})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}

		const numEvents = 10
		const amountPerEvent int64 = 100

		var wg sync.WaitGroup
		errs := make(chan error, numEvents)

		for i := 0; i < numEvents; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				payload := events.JournalEntryPostedPayload{
					JournalEntryID: uuid.NewString(),
					ReferenceType:  "CONCURRENT_TEST",
					ReferenceID:    "ref_conc_" + uuid.NewString()[:8],
					Currency:       "SAR",
					PostedAt:       time.Now().UTC(),
					Lines: []events.JournalEntryPostedLinePayload{
						{
							AccountID:        acc.ID,
							DebitAmountMinor: amountPerEvent,
						},
					},
				}

				envelope, err := events.NewJournalEntryPostedEvent(payload, "corr-conc", "caus-conc")
				if err != nil {
					errs <- err
					return
				}

				if err := consumer.HandleEvent(ctx, envelope); err != nil {
					errs <- err
				}
			}(i)
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			t.Fatalf("concurrent event processing error: %v", err)
		}

		// Total debit should equal numEvents * amountPerEvent
		expectedTotal := int64(numEvents) * amountPerEvent
		bal, err := balanceSvc.GetAccountBalance(ctx, acc.ID)
		if err != nil {
			t.Fatalf("get balance after concurrent events: %v", err)
		}

		if bal.DebitTotalMinor != expectedTotal || bal.BalanceMinor != expectedTotal {
			t.Fatalf("expected total debit %d, got debit=%d, balance=%d", expectedTotal, bal.DebitTotalMinor, bal.BalanceMinor)
		}
	})

	t.Run("Settlement Period Operations", func(t *testing.T) {
		start := time.Now().UTC()
		end := start.Add(7 * 24 * time.Hour)

		period, err := balanceSvc.CreateSettlementPeriod(ctx, balance.CreateSettlementPeriodParams{
			StartDate: start,
			EndDate:   end,
		})
		if err != nil {
			t.Fatalf("create settlement period: %v", err)
		}

		if period.Status != balance.SettlementPeriodStatusOpen {
			t.Errorf("expected new settlement period status OPEN, got %s", period.Status)
		}

		fetched, err := balanceSvc.GetSettlementPeriod(ctx, period.ID)
		if err != nil {
			t.Fatalf("get settlement period: %v", err)
		}
		if fetched.ID != period.ID || fetched.Status != balance.SettlementPeriodStatusOpen {
			t.Errorf("unexpected fetched settlement period: %+v", fetched)
		}

		// Close settlement period
		closed, err := balanceSvc.CloseSettlementPeriod(ctx, period.ID)
		if err != nil {
			t.Fatalf("close settlement period: %v", err)
		}
		if closed.Status != balance.SettlementPeriodStatusClosed || closed.ClosedAt == nil {
			t.Errorf("expected closed settlement period, got %+v", closed)
		}

		// Closing again returns error
		_, err = balanceSvc.CloseSettlementPeriod(ctx, period.ID)
		if err != balance.ErrSettlementPeriodAlreadyClosed {
			t.Errorf("expected ErrSettlementPeriodAlreadyClosed, got %v", err)
		}
	})
}
