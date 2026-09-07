package payments_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/payments"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupPaymentsDB(t *testing.T) (*database.Pool, payments.Service, context.Context) {
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

	repo := payments.NewRepository(db.Pool)
	service := payments.NewService(repo)
	return db, service, ctx
}

func seedOrderForPayments(t *testing.T, db *database.Pool, ctx context.Context) string {
	t.Helper()
	sellerID := uuid.NewString()
	storeID := uuid.NewString()
	custID := uuid.NewString()
	cartID := uuid.NewString()
	sessionID := uuid.NewString()
	orderID := uuid.NewString()

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status) VALUES ($1, $2, 'Test Seller', 'active')
	`, sellerID, "sel-"+sellerID[:8])
	if err != nil {
		t.Fatalf("seed seller: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status) VALUES ($1, $2, 'EG', $3, 'Test Store', 'active')
	`, storeID, sellerID, "str-"+storeID[:8])
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO customers (id, store_id, market_code, email, display_name, status) VALUES ($1, $2, 'EG', $3, 'Test Customer', 'active')
	`, custID, storeID, "cust-"+custID[:8]+"@example.com")
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO carts (id, store_id, market_code, customer_id, cart_token_digest, status, created_at, updated_at)
		VALUES ($1, $2, 'EG', $3, $4, 'checked_out', clock_timestamp(), clock_timestamp())
	`, cartID, storeID, custID, "token-"+cartID[:8])
	if err != nil {
		t.Fatalf("seed cart: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO checkout_sessions (id, store_id, cart_id, customer_id, status, expires_at, guest_order_access_token_digest, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'finalized', clock_timestamp() + interval '1 hour', decode('0000000000000000000000000000000000000000000000000000000000000000', 'hex'), clock_timestamp(), clock_timestamp())
	`, sessionID, storeID, cartID, custID)
	if err != nil {
		t.Fatalf("seed checkout session: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (
			id, order_number, store_id, market_code, customer_id, checkout_session_id,
			status, currency_code, subtotal_minor, total_minor, confirmation_deadline_at
		) VALUES (
			$1, $2, $3, 'EG', $4, $5, 'pending', 'EGP', 1000, 1000, clock_timestamp() + interval '1 day'
		)
	`, orderID, "ORD-"+orderID[:8], storeID, custID, sessionID)
	if err != nil {
		t.Fatalf("seed order: %v", err)
	}

	return orderID
}

func TestInitializeAndGetPayment(t *testing.T) {
	db, service, ctx := setupPaymentsDB(t)
	orderID := seedOrderForPayments(t, db, ctx)

	params := payments.InitializePaymentParams{
		OrderID:           orderID,
		AmountMinor:       1500,
		Currency:          "EGP",
		PaymentMethod:     "card",
		Provider:          "tap",
		ProviderReference: "chg_test_123",
	}

	p, err := service.InitializePayment(ctx, params)
	if err != nil {
		t.Fatalf("InitializePayment failed: %v", err)
	}

	if p.ID == "" {
		t.Errorf("expected non-empty payment ID")
	}
	if p.OrderID != orderID {
		t.Errorf("expected order_id %s, got %s", orderID, p.OrderID)
	}
	if p.Status != payments.StatusCreated {
		t.Errorf("expected status CREATED, got %s", p.Status)
	}
	if len(p.Attempts) != 1 {
		t.Fatalf("expected 1 payment attempt, got %d", len(p.Attempts))
	}
	if p.Attempts[0].Provider != "tap" {
		t.Errorf("expected provider tap, got %s", p.Attempts[0].Provider)
	}

	// Retrieve by ID
	got, err := service.GetPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPayment failed: %v", err)
	}
	if got.ID != p.ID || got.Status != payments.StatusCreated {
		t.Errorf("mismatch in fetched payment: %+v", got)
	}

	// Retrieve by Order ID
	gotByOrder, err := service.GetPaymentByOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetPaymentByOrder failed: %v", err)
	}
	if gotByOrder.ID != p.ID {
		t.Errorf("mismatch in fetched payment by order: %+v", gotByOrder)
	}
}

func TestUpdatePaymentStatus_TransitionsAndOutbox(t *testing.T) {
	db, service, ctx := setupPaymentsDB(t)
	orderID := seedOrderForPayments(t, db, ctx)

	p, err := service.InitializePayment(ctx, payments.InitializePaymentParams{
		OrderID:       orderID,
		AmountMinor:   2000,
		Currency:      "EGP",
		PaymentMethod: "card",
	})
	if err != nil {
		t.Fatalf("InitializePayment failed: %v", err)
	}

	// Transition CREATED -> PENDING
	pPending, err := service.UpdatePaymentStatus(ctx, payments.UpdateStatusParams{
		PaymentID: p.ID,
		NewStatus: payments.StatusPending,
	})
	if err != nil {
		t.Fatalf("UpdatePaymentStatus to PENDING failed: %v", err)
	}
	if pPending.Status != payments.StatusPending {
		t.Errorf("expected PENDING, got %s", pPending.Status)
	}

	// Transition PENDING -> CAPTURED (Terminal state -> Emits Outbox Event)
	pCaptured, err := service.UpdatePaymentStatus(ctx, payments.UpdateStatusParams{
		PaymentID:         p.ID,
		NewStatus:         payments.StatusCaptured,
		Provider:          "moyasar",
		ProviderReference: "chg_cap_456",
		CorrelationID:     "corr-123",
		CausationID:       "caus-456",
	})
	if err != nil {
		t.Fatalf("UpdatePaymentStatus to CAPTURED failed: %v", err)
	}
	if pCaptured.Status != payments.StatusCaptured {
		t.Errorf("expected CAPTURED, got %s", pCaptured.Status)
	}

	// Verify Outbox Event created for PaymentCaptured
	var eventType, aggregateID string
	err = db.Pool.QueryRow(ctx, `
		SELECT event_type, aggregate_id
		FROM outbox_events
		WHERE aggregate_type = 'payment' AND aggregate_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, p.ID).Scan(&eventType, &aggregateID)
	if err != nil {
		t.Fatalf("failed to query outbox_events: %v", err)
	}

	if eventType != "payments.payment.captured.v1" {
		t.Errorf("expected outbox event_type payments.payment.captured.v1, got %s", eventType)
	}

	// Try invalid transition CAPTURED -> FAILED (Blocked!)
	_, err = service.UpdatePaymentStatus(ctx, payments.UpdateStatusParams{
		PaymentID: p.ID,
		NewStatus: payments.StatusFailed,
	})
	if err == nil {
		t.Errorf("expected error transitioning CAPTURED to FAILED, got nil")
	}
}

func TestUpdatePaymentStatus_FailedOutbox(t *testing.T) {
	db, service, ctx := setupPaymentsDB(t)
	orderID := seedOrderForPayments(t, db, ctx)

	p, err := service.InitializePayment(ctx, payments.InitializePaymentParams{
		OrderID:       orderID,
		AmountMinor:   3000,
		Currency:      "EGP",
		PaymentMethod: "card",
	})
	if err != nil {
		t.Fatalf("InitializePayment failed: %v", err)
	}

	// Transition CREATED -> FAILED
	pFailed, err := service.UpdatePaymentStatus(ctx, payments.UpdateStatusParams{
		PaymentID:    p.ID,
		NewStatus:    payments.StatusFailed,
		Provider:     "tap",
		ErrorMessage: "insufficient_funds",
	})
	if err != nil {
		t.Fatalf("UpdatePaymentStatus to FAILED failed: %v", err)
	}
	if pFailed.Status != payments.StatusFailed {
		t.Errorf("expected FAILED, got %s", pFailed.Status)
	}

	// Verify Outbox Event created for PaymentFailed
	var eventType string
	err = db.Pool.QueryRow(ctx, `
		SELECT event_type
		FROM outbox_events
		WHERE aggregate_type = 'payment' AND aggregate_id = $1 AND event_type = 'payments.payment.failed.v1'
	`, p.ID).Scan(&eventType)
	if err != nil {
		t.Fatalf("failed to query outbox_events for payment failed event: %v", err)
	}

	// Attempt FAILED -> CAPTURED (Strictly blocked state transition)
	_, err = service.UpdatePaymentStatus(ctx, payments.UpdateStatusParams{
		PaymentID: p.ID,
		NewStatus: payments.StatusCaptured,
	})
	if err == nil {
		t.Errorf("expected error transitioning FAILED to CAPTURED, got nil")
	}
}

func TestPersistWebhookInbox_Deduplication(t *testing.T) {
	_, service, ctx := setupPaymentsDB(t)

	payload := json.RawMessage(`{"id":"evt_1001","type":"charge.captured","amount":5000}`)
	params := payments.PersistWebhookInboxParams{
		Provider:          "tap",
		ConnectionID:      "conn_abc",
		ProviderEventID:   "evt_1001",
		EventType:         "charge.captured",
		PayloadJSON:       payload,
		SignatureVerified: true,
	}

	// First insertion
	wb1, dedup1, err := service.PersistWebhookInbox(ctx, params)
	if err != nil {
		t.Fatalf("PersistWebhookInbox (1st) failed: %v", err)
	}
	if dedup1 {
		t.Errorf("expected deduplicated=false on first insertion")
	}
	if wb1.ID == "" {
		t.Errorf("expected non-empty webhook inbox ID")
	}

	// Second insertion (duplicate provider_event_id)
	wb2, dedup2, err := service.PersistWebhookInbox(ctx, params)
	if err != nil {
		t.Fatalf("PersistWebhookInbox (2nd) failed: %v", err)
	}
	if !dedup2 {
		t.Errorf("expected deduplicated=true on second insertion")
	}
	if wb2.ID != wb1.ID {
		t.Errorf("expected duplicate return to match original ID %s, got %s", wb1.ID, wb2.ID)
	}
}
