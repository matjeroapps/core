package commerce

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestP52CheckoutSessionCapabilityExpiryAndReplayFoundation(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	suffix := uuid.NewString()
	seller, err := repo.CreateSeller(ctx, "p52-seller-"+suffix, "P52 Seller", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p52-store-"+suffix, "P52 Store", "active", nil, "p52-"+suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, cartToken, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}

	session, rawCapability, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rawCapability == "" || session.Status != CheckoutSessionStatusOpen {
		t.Fatalf("session creation = %+v, capability present=%t", session, rawCapability != "")
	}
	var storedDigest []byte
	if err := db.QueryRow(ctx, `SELECT guest_order_access_token_digest FROM checkout_sessions WHERE id = $1`, session.ID).Scan(&storedDigest); err != nil {
		t.Fatal(err)
	}
	if len(storedDigest) != 32 || string(storedDigest) == rawCapability {
		t.Fatalf("stored capability is not a digest: len=%d", len(storedDigest))
	}
	var sessionCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM checkout_sessions WHERE cart_id = $1`, cart.ID).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("session count = %d, want 1", sessionCount)
	}

	request := testFinalizeRequest(session.ID)
	decision, err := repo.EvaluateCheckoutSession(ctx, store.ID, request)
	if err != nil || decision.Status != CheckoutSessionStatusOpen || decision.Replay {
		t.Fatalf("open session decision = %+v, err=%v", decision, err)
	}

	second, secondCapability, err := repo.CreateCheckoutSession(ctx, store.ID, cartToken, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if secondCapability == rawCapability || second.ID == session.ID {
		t.Fatal("multiple sessions did not receive independent capabilities")
	}

	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EvaluateCheckoutSession(ctx, store.ID, request); !errors.Is(err, ErrCheckoutExpired) {
		t.Fatalf("timestamp-expired open session error = %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET status = 'expired' WHERE id = $1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EvaluateCheckoutSession(ctx, store.ID, request); !errors.Is(err, ErrCheckoutExpired) {
		t.Fatalf("persisted-expired session error = %v", err)
	}

	if _, err := db.Exec(ctx, `UPDATE carts SET status = 'checked_out' WHERE id = $1`, cart.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET status = 'finalized', expires_at = clock_timestamp() - interval '1 hour' WHERE id = $1`, session.ID); err != nil {
		t.Fatal(err)
	}
	var storedFingerprint string
	finalizedSession := session
	finalizedSession.Status = CheckoutSessionStatusFinalized
	finalizedSession.FinalizeFingerprint = &storedFingerprint
	checkedOutCart := cart
	checkedOutCart.Status = CartStatusCheckedOut
	computed, err := ComputeFinalizeFingerprint(finalizedSession, checkedOutCart, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET finalize_fingerprint = $2 WHERE id = $1`, session.ID, computed); err != nil {
		t.Fatal(err)
	}
	decision, err = repo.EvaluateCheckoutSession(ctx, store.ID, request)
	if err != nil || !decision.Replay {
		t.Fatalf("finalized replay = %+v, err=%v", decision, err)
	}

	changed := request
	changed.ContactEmail = "changed@example.test"
	if _, err := repo.EvaluateCheckoutSession(ctx, store.ID, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed finalized request error = %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE carts SET status = 'active' WHERE id = $1`, cart.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EvaluateCheckoutSession(ctx, store.ID, request); !errors.Is(err, ErrCheckoutCartInvariant) {
		t.Fatalf("finalized Cart mismatch error = %v", err)
	}

	if _, err := db.Exec(ctx, `UPDATE checkout_sessions SET status = 'open' WHERE id = $1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE carts SET status = 'checked_out' WHERE id = $1`, cart.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EvaluateCheckoutSession(ctx, store.ID, FinalizeRequest{SessionID: second.ID, ShippingAddress: testFinalizeRequest(second.ID).ShippingAddress, ContactEmail: "customer@example.test"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("open Session plus checked-out Cart error = %v", err)
	}
}

func TestStoreOperationalStatePausesCheckoutSessionCreationAndFinalize(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	setup := setupSellerCheckoutTest(t, db, repo, ctx, uuid.NewString(), 10, 1000)

	state, err := repo.GetStoreOperationalState(ctx, setup.Store.ID)
	if err != nil {
		t.Fatalf("GetStoreOperationalState: %v", err)
	}
	if !state.CheckoutAccepting || state.CheckoutStatus != StoreCheckoutStatusAccepting {
		t.Fatalf("initial operational state = %+v", state)
	}
	if _, err := repo.UpdateStoreOperationalState(ctx, setup.Store.ID, StoreCheckoutStatusPaused, strings.Repeat("x", 241), "subject-of-seller-a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized maintenance message error = %v", err)
	}

	state, err = repo.UpdateStoreOperationalState(ctx, setup.Store.ID, StoreCheckoutStatusPaused, "Back soon", "subject-of-seller-a")
	if err != nil {
		t.Fatalf("UpdateStoreOperationalState: %v", err)
	}
	if state.CheckoutAccepting || state.CheckoutStatus != StoreCheckoutStatusPaused || state.MaintenanceMessage != "Back soon" || state.UpdatedAt == nil {
		t.Fatalf("paused operational state = %+v", state)
	}

	if _, _, err := repo.CreateCheckoutSession(ctx, setup.Store.ID, setup.CartToken, nil, time.Hour); !errors.Is(err, ErrCheckoutPaused) {
		t.Fatalf("paused checkout session creation error = %v", err)
	}
	if _, err := repo.FinalizeCheckout(ctx, setup.Store.ID, testFinalizeRequest(setup.Session.ID), "corr-paused"); !errors.Is(err, ErrCheckoutPaused) {
		t.Fatalf("paused checkout finalize error = %v", err)
	}

	if _, err := repo.UpdateStoreOperationalState(ctx, setup.Store.ID, StoreCheckoutStatusAccepting, "", "subject-of-seller-a"); err != nil {
		t.Fatalf("resume operational state: %v", err)
	}
	if _, _, err := repo.CreateCheckoutSession(ctx, setup.Store.ID, setup.CartToken, nil, time.Hour); err != nil {
		t.Fatalf("resumed checkout session creation: %v", err)
	}
}

func TestP52CheckoutSessionConstraints(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	suffix := uuid.NewString()
	seller, err := repo.CreateSeller(ctx, "p52-constraint-seller-"+suffix, "P52 Constraint Seller", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p52-constraint-store-"+suffix, "P52 Constraint Store", "active", nil, "p52-c-"+suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, token, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateCheckoutSession(ctx, store.ID, token, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	storeB, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p52-constraint-store-b-"+suffix, "P52 Constraint Store B", "active", nil, "p52-c-b-"+suffix+".test", "platform", "active", true, nil, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	cartB, tokenB, err := repo.CreateCart(ctx, storeB.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CreateCheckoutSession(ctx, store.ID, tokenB, nil, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-Store Cart session error = %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest) VALUES ($1,$2,$3,'open',clock_timestamp()+interval '1 hour',decode(repeat('00',32),'hex'))`, uuid.NewString(), store.ID, cartB.ID); err == nil {
		t.Fatal("cross-Store Cart FK was accepted")
	}
	customerB, err := repo.CreateCustomer(ctx, storeB.ID, "EG", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO checkout_sessions (id, store_id, cart_id, customer_id, status, expires_at, guest_order_access_token_digest) VALUES ($1,$2,$3,$4,'open',clock_timestamp()+interval '1 hour',decode(repeat('00',32),'hex'))`, uuid.NewString(), store.ID, cart.ID, customerB.ID); err == nil {
		t.Fatal("cross-Store Customer FK was accepted")
	}
	if _, err := db.Exec(ctx, `INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest) VALUES ($1,$2,$3,'invalid',clock_timestamp()+interval '1 hour',decode(repeat('00',32),'hex'))`, uuid.NewString(), store.ID, cart.ID); err == nil {
		t.Fatal("invalid Session status was accepted")
	}
	if _, err := db.Exec(ctx, `INSERT INTO checkout_sessions (id, store_id, cart_id, status, expires_at, guest_order_access_token_digest) VALUES ($1,$2,$3,'open',clock_timestamp()+interval '1 hour',decode('00','hex'))`, uuid.NewString(), store.ID, cart.ID); err == nil {
		t.Fatal("short capability digest was accepted")
	}
}

func TestP52ExpiryDecisionIsCapturedAfterSessionAndCartLocks(t *testing.T) {
	db, repo, ctx := setupP51Database(t)
	suffix := uuid.NewString()
	seller, err := repo.CreateSeller(ctx, "p52-lock-seller-"+suffix, "P52 Lock Seller", "active", nil)
	if err != nil {
		t.Fatal(err)
	}
	store, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "p52-lock-store-"+suffix, "P52 Lock Store", "active", nil, "p52-lock-"+suffix+".test", "platform", "active", true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cart, token, err := repo.CreateCart(ctx, store.ID, "EG", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := repo.CreateCheckoutSession(ctx, store.ID, token, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var expiresAt time.Time
	if err := db.QueryRow(ctx, `UPDATE checkout_sessions SET expires_at = clock_timestamp() + interval '100 milliseconds' WHERE id = $1 RETURNING expires_at`, session.ID).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}

	cartTx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cartTx.Rollback(ctx)
	if _, err := cartTx.Exec(ctx, `SELECT id FROM carts WHERE id = $1 FOR UPDATE`, cart.ID); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := repo.EvaluateCheckoutSession(ctx, store.ID, testFinalizeRequest(session.ID))
		result <- err
	}()

	// A NOWAIT probe is the barrier proving the evaluator acquired Session before
	// it reached the Cart lock. No timing sleep is used for synchronization.
	barrierCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		probe, err := db.Pool.Begin(barrierCtx)
		if err != nil {
			t.Fatal(err)
		}
		var lockedID string
		err = probe.QueryRow(barrierCtx, `SELECT id FROM checkout_sessions WHERE id = $1 FOR UPDATE NOWAIT`, session.ID).Scan(&lockedID)
		_ = probe.Rollback(barrierCtx)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
				break
			}
			t.Fatalf("session lock probe: %v", err)
		}
		runtime.Gosched()
		select {
		case err := <-result:
			t.Fatalf("evaluation completed before Cart barrier: %v", err)
		default:
		}
	}

	for {
		var now time.Time
		if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		if !now.Before(expiresAt) {
			break
		}
		runtime.Gosched()
	}
	if err := cartTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrCheckoutExpired) {
		t.Fatalf("lock-linearized expiry error = %v", err)
	}
}

func testFinalizeRequest(sessionID string) FinalizeRequest {
	return FinalizeRequest{
		SessionID: sessionID,
		ShippingAddress: ShippingAddress{
			RecipientName: "Customer", AddressLine1: "1 Main Street", City: "Cairo", CountryCode: "EG",
		},
		ContactEmail: "customer@example.test",
	}
}
