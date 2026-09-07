package accounting_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/accounting"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/events"
)

func setupAccountingDB(t *testing.T) (*database.Pool, finance.Service, accounting.Consumer, context.Context) {
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
	financeSvc := finance.NewService(repo)
	consumer := accounting.NewConsumer(db.Pool, financeSvc)

	return db, financeSvc, consumer, ctx
}

func TestAccountingIntegration(t *testing.T) {
	db, financeSvc, consumer, ctx := setupAccountingDB(t)

	t.Run("Payment Captured Event End-to-End", func(t *testing.T) {
		paymentID := "pay_" + uuid.NewString()[:8]
		orderID := "ord_" + uuid.NewString()[:8]

		payload := events.PaymentCapturedPayload{
			PaymentID:         paymentID,
			OrderID:           orderID,
			AmountMinor:       35000,
			Currency:          "SAR",
			PaymentMethod:     "MADA",
			Provider:          "CHECKOUT",
			ProviderReference: "prov_ref_100",
			CapturedAt:        time.Now().UTC(),
		}

		envelope, err := events.NewPaymentCapturedEvent(payload, "corr-integration-1", "caus-integration-1")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("handle payment captured event: %v", err)
		}

		// 1. Verify Inbox store record exists
		var inboxCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM processed_events WHERE consumer_name = $1 AND event_id = $2
		`, accounting.ConsumerName, envelope.EventID).Scan(&inboxCount)
		if err != nil {
			t.Fatalf("query processed_events: %v", err)
		}
		if inboxCount != 1 {
			t.Fatalf("expected 1 processed_events record, got %d", inboxCount)
		}

		// 2. Verify Journal Entry posted
		var entryID, refType, refID, desc, currency string
		err = db.Pool.QueryRow(ctx, `
			SELECT id, reference_type, reference_id, description, currency
			FROM journal_entries
			WHERE reference_type = 'PAYMENT_CAPTURED' AND reference_id = $1
		`, paymentID).Scan(&entryID, &refType, &refID, &desc, &currency)
		if err != nil {
			t.Fatalf("query journal_entries: %v", err)
		}
		if refType != "PAYMENT_CAPTURED" || refID != paymentID || currency != "SAR" {
			t.Errorf("unexpected journal entry values: refType=%s, refID=%s, currency=%s", refType, refID, currency)
		}

		// 3. Verify Journal Lines created (Debit 1010-PAYMENT-CLEARING-SAR, Credit 2010-CUSTOMER-FUNDS-SAR)
		lines, err := financeSvc.GetJournalEntry(ctx, entryID)
		if err != nil {
			t.Fatalf("get journal entry: %v", err)
		}
		if len(lines.Lines) != 2 {
			t.Fatalf("expected 2 journal lines, got %d", len(lines.Lines))
		}

		// 4. Verify Outbox Event created (ledger.journal_entry.posted.v1)
		var outboxCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'ledger.journal_entry.posted.v1'
		`, entryID).Scan(&outboxCount)
		if err != nil {
			t.Fatalf("query outbox_events: %v", err)
		}
		if outboxCount != 1 {
			t.Fatalf("expected 1 outbox_events record, got %d", outboxCount)
		}
	})

	t.Run("Duplicate Event Processing Idempotency", func(t *testing.T) {
		paymentID := "pay_" + uuid.NewString()[:8]
		orderID := "ord_" + uuid.NewString()[:8]

		payload := events.PaymentCapturedPayload{
			PaymentID:     paymentID,
			OrderID:       orderID,
			AmountMinor:   50000,
			Currency:      "SAR",
			PaymentMethod: "CREDIT_CARD",
			CapturedAt:    time.Now().UTC(),
		}

		envelope, err := events.NewPaymentCapturedEvent(payload, "corr-idempotent", "caus-idempotent")
		if err != nil {
			t.Fatalf("create event envelope: %v", err)
		}

		// First process
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("first event handle: %v", err)
		}

		// Second process with EXACT SAME event envelope
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("second duplicate event handle failed: %v", err)
		}

		// Verify exactly ONE journal entry exists for paymentID
		var entryCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM journal_entries WHERE reference_type = 'PAYMENT_CAPTURED' AND reference_id = $1
		`, paymentID).Scan(&entryCount)
		if err != nil {
			t.Fatalf("query journal entry count: %v", err)
		}
		if entryCount != 1 {
			t.Fatalf("expected exactly 1 journal entry, got %d", entryCount)
		}

		// Third process with DIFFERENT event ID but SAME paymentID (e.g. re-emitted event)
		envelope2, err := events.NewPaymentCapturedEvent(payload, "corr-re-emit", "caus-re-emit")
		if err != nil {
			t.Fatalf("create second event envelope: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope2)
		if err != nil {
			t.Fatalf("re-emitted event handle failed: %v", err)
		}

		// Still exactly ONE journal entry exists
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM journal_entries WHERE reference_type = 'PAYMENT_CAPTURED' AND reference_id = $1
		`, paymentID).Scan(&entryCount)
		if err != nil {
			t.Fatalf("query journal entry count: %v", err)
		}
		if entryCount != 1 {
			t.Fatalf("expected exactly 1 journal entry after re-emitted event, got %d", entryCount)
		}
	})

	t.Run("Payment Failed Event Does Not Create Journal Entry", func(t *testing.T) {
		paymentID := "pay_failed_" + uuid.NewString()[:8]
		orderID := "ord_failed_" + uuid.NewString()[:8]

		payload := events.PaymentFailedPayload{
			PaymentID:     paymentID,
			OrderID:       orderID,
			AmountMinor:   20000,
			Currency:      "SAR",
			PaymentMethod: "MADA",
			ErrorMessage:  "Payment declined by issuer",
			FailedAt:      time.Now().UTC(),
		}

		envelope, err := events.NewPaymentFailedEvent(payload, "corr-failed", "caus-failed")
		if err != nil {
			t.Fatalf("create failed event envelope: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("handle payment failed event: %v", err)
		}

		// Verify Inbox store record exists
		var inboxCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM processed_events WHERE consumer_name = $1 AND event_id = $2
		`, accounting.ConsumerName, envelope.EventID).Scan(&inboxCount)
		if err != nil {
			t.Fatalf("query processed_events: %v", err)
		}
		if inboxCount != 1 {
			t.Fatalf("expected 1 processed_events record, got %d", inboxCount)
		}

		// Verify NO journal entry created
		var entryCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM journal_entries WHERE reference_id = $1
		`, paymentID).Scan(&entryCount)
		if err != nil {
			t.Fatalf("query journal entries count: %v", err)
		}
		if entryCount != 0 {
			t.Fatalf("expected 0 journal entries for failed payment, got %d", entryCount)
		}
	})

	t.Run("Ledger Failure Rolls Back Inbox Record and Allows Safe Retry", func(t *testing.T) {
		paymentID := "pay_rollback_" + uuid.NewString()[:8]

		// Using invalid currency length in payload forces a failure during account lookup or validation
		payload := map[string]any{
			"payment_id":     paymentID,
			"order_id":       "ord_rollback_1",
			"amount_minor":   int64(10000),
			"currency":       "INVALID_CURRENCY",
			"payment_method": "CREDIT_CARD",
			"captured_at":    time.Now().UTC().Format(time.RFC3339),
		}

		envelope := events.EventEnvelope{
			EventID:          uuid.NewString(),
			EventType:        events.EventTypePaymentCaptured,
			SchemaVersion:    1,
			AggregateType:    "payment",
			AggregateID:      paymentID,
			AggregateVersion: 1,
			OccurredAt:       time.Now().UTC(),
			Payload:          payload,
		}

		// Execution fails due to invalid currency
		err := consumer.HandleEvent(ctx, envelope)
		if err == nil {
			t.Fatalf("expected error due to invalid currency, got nil")
		}

		// Verify inbox record was ROLLED BACK and does NOT exist
		var inboxCount int
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM processed_events WHERE consumer_name = $1 AND event_id = $2
		`, accounting.ConsumerName, envelope.EventID).Scan(&inboxCount)
		if err != nil {
			t.Fatalf("query processed_events: %v", err)
		}
		if inboxCount != 0 {
			t.Fatalf("expected 0 inbox records after failed transaction, got %d", inboxCount)
		}

		// Now fix payload currency to valid SAR
		payload["currency"] = "SAR"
		envelope.Payload = payload

		// Retry execution succeeds
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("retry handle event failed: %v", err)
		}

		// Inbox record now exists
		err = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM processed_events WHERE consumer_name = $1 AND event_id = $2
		`, accounting.ConsumerName, envelope.EventID).Scan(&inboxCount)
		if err != nil {
			t.Fatalf("query processed_events: %v", err)
		}
		if inboxCount != 1 {
			t.Fatalf("expected 1 inbox record after successful retry, got %d", inboxCount)
		}
	})
}
