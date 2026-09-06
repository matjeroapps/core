package outbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/config"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

func setupOutboxDB(t *testing.T) (*database.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)

	ctx := context.Background()
	for _, name := range []string{
		"000001_event_delivery_foundation",
		"000013_outbox_publish_claims",
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "migrations", name+".up.sql"))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	return db, ctx
}

type fakePublisher struct {
	mu        sync.Mutex
	published []events.EventEnvelope
	failCount int
}

func (f *fakePublisher) PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failCount > 0 {
		f.failCount--
		return errors.New("simulated publish failure")
	}

	f.published = append(f.published, event)
	return nil
}

func sampleEnvelope() events.EventEnvelope {
	return events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        "commerce.order.created.v1",
		SchemaVersion:    1,
		AggregateType:    "order",
		AggregateID:      "ord_123",
		AggregateVersion: 1,
		CorrelationID:    "corr_abc_999",
		CausationID:      "cmd_create_order",
		OccurredAt:       time.Now().UTC().Truncate(time.Microsecond),
		Payload: map[string]any{
			"order_id":    "ord_123",
			"store_id":    "str_456",
			"total_cents": float64(1500),
		},
	}
}

func TestOutboxTwoPublishersNeverClaimSameEvent(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	var eventIDs []string
	for i := 0; i < 10; i++ {
		env := sampleEnvelope()
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if err := store.Enqueue(ctx, tx, env); err != nil {
			t.Fatalf("enqueue event: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit tx: %v", err)
		}
		eventIDs = append(eventIDs, env.EventID)
	}

	claimIDA := uuid.NewString()
	claimIDB := uuid.NewString()

	var claimedA, claimedB []string
	var errA, errB error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		claimedA, errA = store.ClaimBatch(ctx, db.Pool, claimIDA, 10, 30*time.Second)
	}()

	go func() {
		defer wg.Done()
		claimedB, errB = store.ClaimBatch(ctx, db.Pool, claimIDB, 10, 30*time.Second)
	}()

	wg.Wait()

	if errA != nil {
		t.Fatalf("publisher A claim error: %v", errA)
	}
	if errB != nil {
		t.Fatalf("publisher B claim error: %v", errB)
	}

	totalClaimed := len(claimedA) + len(claimedB)
	if totalClaimed != 10 {
		t.Errorf("expected total 10 claimed events across both publishers, got %d (A: %d, B: %d)",
			totalClaimed, len(claimedA), len(claimedB))
	}

	seen := make(map[string]bool)
	for _, id := range claimedA {
		seen[id] = true
	}
	for _, id := range claimedB {
		if seen[id] {
			t.Errorf("overlap detected! Both publishers claimed event %s", id)
		}
	}
}

func TestOutboxStaleLeaseCanBeReclaimed(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimA := uuid.NewString()
	claimed, err := store.ClaimBatch(ctx, db.Pool, claimA, 1, 30*time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim A failed: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET publish_claimed_at = clock_timestamp() - interval '1 minute'
		WHERE event_id = $1::uuid
	`, env.EventID)
	if err != nil {
		t.Fatalf("simulate lease expiry: %v", err)
	}

	claimB := uuid.NewString()
	reclaimed, err := store.ClaimBatch(ctx, db.Pool, claimB, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim B error: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != env.EventID {
		t.Fatalf("expected claim B to reclaim expired event %s, got %v", env.EventID, reclaimed)
	}
}

func TestOutboxStaleAckCannotMarkPublished(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimA := uuid.NewString()
	_, err = store.ClaimBatch(ctx, db.Pool, claimA, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim A: %v", err)
	}

	claimB := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET publish_claim_id = $1::uuid, publish_claimed_at = clock_timestamp()
		WHERE event_id = $2::uuid
	`, claimB, env.EventID)
	if err != nil {
		t.Fatalf("simulate claim takeover: %v", err)
	}

	marked, err := store.MarkPublished(ctx, db.Pool, env.EventID, claimA)
	if err != nil {
		t.Fatalf("MarkPublished with stale claim A error: %v", err)
	}
	if marked {
		t.Errorf("expected MarkPublished to return false for stale claim A, got true")
	}
}

func TestOutboxLostClaimSkipsPublish(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimA := uuid.NewString()
	_, err = store.ClaimBatch(ctx, db.Pool, claimA, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim A: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET publish_claim_id = NULL
		WHERE event_id = $1::uuid
	`, env.EventID)
	if err != nil {
		t.Fatalf("simulate lost claim: %v", err)
	}

	loaded, err := store.RenewAndLoadEvent(ctx, db.Pool, env.EventID, claimA, 30*time.Second)
	if !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("expected ErrClaimLost, got loaded=%v, err=%v", loaded, err)
	}
}

func TestOutboxRenewClaimAndLoadReturnsCompleteEnvelope(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimID := uuid.NewString()
	claimed, err := store.ClaimBatch(ctx, db.Pool, claimID, 1, 30*time.Second)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim batch failed: %v", err)
	}

	loaded, err := store.RenewAndLoadEvent(ctx, db.Pool, env.EventID, claimID, 30*time.Second)
	if err != nil {
		t.Fatalf("RenewAndLoadEvent error: %v", err)
	}

	if loaded.EventID != env.EventID {
		t.Errorf("EventID mismatch: expected %s, got %s", env.EventID, loaded.EventID)
	}
	if loaded.EventType != env.EventType {
		t.Errorf("EventType mismatch: expected %s, got %s", env.EventType, loaded.EventType)
	}
	if loaded.SchemaVersion != env.SchemaVersion {
		t.Errorf("SchemaVersion mismatch: expected %d, got %d", env.SchemaVersion, loaded.SchemaVersion)
	}
	if loaded.AggregateType != env.AggregateType {
		t.Errorf("AggregateType mismatch: expected %s, got %s", env.AggregateType, loaded.AggregateType)
	}
	if loaded.AggregateID != env.AggregateID {
		t.Errorf("AggregateID mismatch: expected %s, got %s", env.AggregateID, loaded.AggregateID)
	}
	if loaded.AggregateVersion != env.AggregateVersion {
		t.Errorf("AggregateVersion mismatch: expected %d, got %d", env.AggregateVersion, loaded.AggregateVersion)
	}
	if loaded.CorrelationID != env.CorrelationID {
		t.Errorf("CorrelationID mismatch: expected %s, got %s", env.CorrelationID, loaded.CorrelationID)
	}
	if loaded.CausationID != env.CausationID {
		t.Errorf("CausationID mismatch: expected %s, got %s", env.CausationID, loaded.CausationID)
	}
	if !loaded.OccurredAt.Equal(env.OccurredAt) {
		t.Errorf("OccurredAt mismatch: expected %v, got %v", env.OccurredAt, loaded.OccurredAt)
	}
}

func TestOutboxNearExpiryDBAuthoritativeRenewal(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env1 := sampleEnvelope()
	env2 := sampleEnvelope()
	env3 := sampleEnvelope()
	env4 := sampleEnvelope()

	for _, env := range []events.EventEnvelope{env1, env2, env3, env4} {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if err := store.Enqueue(ctx, tx, env); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	claimID := uuid.NewString()
	claimIDOther := uuid.NewString()

	_, err := store.ClaimBatch(ctx, db.Pool, claimID, 3, 30*time.Second)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	_, err = store.ClaimBatch(ctx, db.Pool, claimIDOther, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim batch other: %v", err)
	}

	// 1. Healthy claim (env1): remaining lease = 25s (> renewalMargin 10s)
	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events SET publish_claimed_at = clock_timestamp() - interval '5 seconds'
		WHERE event_id = $1::uuid
	`, env1.EventID)
	if err != nil {
		t.Fatalf("set healthy timestamp: %v", err)
	}

	// 2. Near-expiry claim (env2): remaining lease = 5s (<= renewalMargin 10s)
	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events SET publish_claimed_at = clock_timestamp() - interval '25 seconds'
		WHERE event_id = $1::uuid
	`, env2.EventID)
	if err != nil {
		t.Fatalf("set near-expiry timestamp: %v", err)
	}

	// 3. Expired claim (env3): remaining lease = -5s (expired)
	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events SET publish_claimed_at = clock_timestamp() - interval '35 seconds'
		WHERE event_id = $1::uuid
	`, env3.EventID)
	if err != nil {
		t.Fatalf("set expired timestamp: %v", err)
	}

	var t1Before, t2Before, t3Before, t4Before time.Time
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env1.EventID).Scan(&t1Before)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env2.EventID).Scan(&t2Before)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env3.EventID).Scan(&t3Before)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env4.EventID).Scan(&t4Before)

	err = store.RenewBatchNearExpiry(ctx, db.Pool, []string{env1.EventID, env2.EventID, env3.EventID, env4.EventID}, claimID, 30*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("RenewBatchNearExpiry error: %v", err)
	}

	var t1After, t2After, t3After, t4After time.Time
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env1.EventID).Scan(&t1After)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env2.EventID).Scan(&t2After)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env3.EventID).Scan(&t3After)
	_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, env4.EventID).Scan(&t4After)

	if !t2After.After(t2Before) {
		t.Errorf("expected near-expiry event %s to be renewed, got before=%v, after=%v", env2.EventID, t2Before, t2After)
	}
	if !t1After.Equal(t1Before) {
		t.Errorf("expected healthy event %s NOT to be renewed, got before=%v, after=%v", env1.EventID, t1Before, t1After)
	}
	if !t3After.Equal(t3Before) {
		t.Errorf("expected expired event %s NOT to be renewed, got before=%v, after=%v", env3.EventID, t3Before, t3After)
	}
	if !t4After.Equal(t4Before) {
		t.Errorf("expected other worker claim %s NOT to be renewed, got before=%v, after=%v", env4.EventID, t4Before, t4After)
	}
}

func TestOutboxPublishFailureSchedulesBackoff(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimID := uuid.NewString()
	_, err = store.ClaimBatch(ctx, db.Pool, claimID, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	released, err := store.ReleaseWithBackoff(ctx, db.Pool, env.EventID, claimID)
	if err != nil {
		t.Fatalf("ReleaseWithBackoff error: %v", err)
	}
	if !released {
		t.Fatalf("expected ReleaseWithBackoff to return true, got false")
	}

	var attempts int
	var nextAttemptAt time.Time
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts, next_attempt_at
		FROM outbox_events
		WHERE event_id = $1::uuid
	`, env.EventID).Scan(&attempts, &nextAttemptAt)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected publish_attempts = 1, got %d", attempts)
	}
	if !nextAttemptAt.After(time.Now().Add(-1 * time.Second)) {
		t.Errorf("expected next_attempt_at in the future, got %v", nextAttemptAt)
	}
}

func TestOutboxBackoffIsBounded(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for i := 0; i < 10; i++ {
		claimID := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			UPDATE outbox_events
			SET next_attempt_at = clock_timestamp() - interval '1 second'
			WHERE event_id = $1::uuid
		`, env.EventID)
		if err != nil {
			t.Fatalf("fast forward next_attempt_at: %v", err)
		}

		claimed, err := store.ClaimBatch(ctx, db.Pool, claimID, 1, 30*time.Second)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("iteration %d claim failed: %v", i, err)
		}

		released, err := store.ReleaseWithBackoff(ctx, db.Pool, env.EventID, claimID)
		if err != nil || !released {
			t.Fatalf("iteration %d release failed: %v", i, err)
		}
	}

	var attempts int
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts
		FROM outbox_events
		WHERE event_id = $1::uuid
	`, env.EventID).Scan(&attempts)
	if err != nil {
		t.Fatalf("query attempts: %v", err)
	}

	if attempts != 10 {
		t.Errorf("expected publish_attempts = 10, got %d", attempts)
	}
}

func TestOutboxBackoffFormulaExactDBTime(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	expectedDelays := []int{1, 2, 4, 8, 16, 32, 64}

	for i, expectedSec := range expectedDelays {
		claimID := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			UPDATE outbox_events
			SET next_attempt_at = clock_timestamp() - interval '1 second'
			WHERE event_id = $1::uuid
		`, env.EventID)
		if err != nil {
			t.Fatalf("fast forward next_attempt_at: %v", err)
		}

		claimed, err := store.ClaimBatch(ctx, db.Pool, claimID, 1, 30*time.Second)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("attempt %d claim failed: %v", i+1, err)
		}

		released, err := store.ReleaseWithBackoff(ctx, db.Pool, env.EventID, claimID)
		if err != nil || !released {
			t.Fatalf("attempt %d release failed: %v", i+1, err)
		}

		var delaySec float64
		err = db.Pool.QueryRow(ctx, `
			SELECT EXTRACT(EPOCH FROM (next_attempt_at - clock_timestamp()))
			FROM outbox_events WHERE event_id = $1::uuid
		`, env.EventID).Scan(&delaySec)
		if err != nil {
			t.Fatalf("query backoff delay: %v", err)
		}

		if delaySec < float64(expectedSec)-0.5 || delaySec > float64(expectedSec)+0.5 {
			t.Errorf("attempt %d: expected backoff delay ~%ds, got %.2fs", i+1, expectedSec, delaySec)
		}
	}
}

func TestOutboxRetryPreservesEventID(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	env.EventType = "commerce.order.status_changed.v1"
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{failCount: 1}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("first ProcessBatch error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event claimed, got %d", processed)
	}

	fakePub.mu.Lock()
	publishedCount := len(fakePub.published)
	fakePub.mu.Unlock()
	if publishedCount != 0 {
		t.Fatalf("expected 0 published events due to failure, got %d", publishedCount)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET next_attempt_at = clock_timestamp() - interval '1 second'
		WHERE event_id = $1::uuid
	`, env.EventID)
	if err != nil {
		t.Fatalf("reset next_attempt_at: %v", err)
	}

	processed, err = proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("second ProcessBatch error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event claimed, got %d", processed)
	}

	fakePub.mu.Lock()
	defer fakePub.mu.Unlock()
	if len(fakePub.published) != 1 {
		t.Fatalf("expected 1 published event on retry, got %d", len(fakePub.published))
	}
	if fakePub.published[0].EventID != env.EventID {
		t.Errorf("expected retried event to preserve stable EventID %s, got %s", env.EventID, fakePub.published[0].EventID)
	}
}

func TestOutboxConfirmThenCrashRepublishesSameEventID(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claim1 := uuid.NewString()
	claimed1, err := store.ClaimBatch(ctx, db.Pool, claim1, 1, 30*time.Second)
	if err != nil || len(claimed1) != 1 {
		t.Fatalf("claim1 failed: %v", err)
	}
	env1, err := store.RenewAndLoadEvent(ctx, db.Pool, env.EventID, claim1, 30*time.Second)
	if err != nil {
		t.Fatalf("RenewAndLoadEvent worker 1 error: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events
		SET publish_claimed_at = clock_timestamp() - interval '1 minute'
		WHERE event_id = $1::uuid
	`, env.EventID)
	if err != nil {
		t.Fatalf("expire claim1: %v", err)
	}

	claim2 := uuid.NewString()
	claimed2, err := store.ClaimBatch(ctx, db.Pool, claim2, 1, 30*time.Second)
	if err != nil || len(claimed2) != 1 {
		t.Fatalf("claim2 failed: %v", err)
	}
	env2, err := store.RenewAndLoadEvent(ctx, db.Pool, env.EventID, claim2, 30*time.Second)
	if err != nil {
		t.Fatalf("RenewAndLoadEvent worker 2 error: %v", err)
	}

	if env1.EventID != env2.EventID {
		t.Errorf("expected both publication attempts to preserve same EventID %s, got %s vs %s",
			env.EventID, env1.EventID, env2.EventID)
	}
}

func TestOrderCreatedPublishedEnvelopeMatchesOutbox(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("ProcessBatch expected 1, got %d (err: %v)", processed, err)
	}

	fakePub.mu.Lock()
	defer fakePub.mu.Unlock()
	if len(fakePub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(fakePub.published))
	}
	pubEnv := fakePub.published[0]

	if pubEnv.EventID != env.EventID ||
		pubEnv.EventType != env.EventType ||
		pubEnv.SchemaVersion != env.SchemaVersion ||
		pubEnv.AggregateType != env.AggregateType ||
		pubEnv.AggregateID != env.AggregateID ||
		pubEnv.AggregateVersion != env.AggregateVersion ||
		pubEnv.CorrelationID != env.CorrelationID ||
		pubEnv.CausationID != env.CausationID {
		t.Errorf("published envelope does not match outbox row: got %+v", pubEnv)
	}
	if !pubEnv.OccurredAt.Equal(env.OccurredAt) {
		t.Errorf("OccurredAt mismatch: expected %v, got %v", env.OccurredAt, pubEnv.OccurredAt)
	}
	envPayloadJSON, _ := json.Marshal(env.Payload)
	pubPayloadJSON, _ := json.Marshal(pubEnv.Payload)
	if !bytes.Equal(envPayloadJSON, pubPayloadJSON) {
		t.Errorf("Payload mismatch: expected %s, got %s", string(envPayloadJSON), string(pubPayloadJSON))
	}
}

func TestOrderCreatedPublishedCorrelationPreserved(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	expectedCorrelationID := "http_req_corr_98765"
	env := sampleEnvelope()
	env.CorrelationID = expectedCorrelationID

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	_, err = proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}

	fakePub.mu.Lock()
	defer fakePub.mu.Unlock()
	if len(fakePub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(fakePub.published))
	}

	if fakePub.published[0].CorrelationID != expectedCorrelationID {
		t.Errorf("expected CorrelationID %s to be preserved, got %s",
			expectedCorrelationID, fakePub.published[0].CorrelationID)
	}
}

func TestOutboxMalformedPayloadTriggersBackoff(t *testing.T) {
	db, ctx := setupOutboxDB(t)

	eventID := uuid.NewString()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, occurred_at
		) VALUES (
			$1::uuid, 'order', 'ord_bad', 1, 'commerce.order.created.v1',
			1, '"{bad-json-payload"', clock_timestamp()
		)
	`, eventID)
	if err != nil {
		t.Fatalf("insert bad payload event: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event processed, got %d", processed)
	}

	fakePub.mu.Lock()
	publishedCount := len(fakePub.published)
	fakePub.mu.Unlock()
	if publishedCount != 0 {
		t.Fatalf("expected 0 published events for malformed payload, got %d", publishedCount)
	}

	var attempts int
	var claimID *string
	var publishedAt *time.Time
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts, publish_claim_id::text, published_at
		FROM outbox_events WHERE event_id = $1::uuid
	`, eventID).Scan(&attempts, &claimID, &publishedAt)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected publish_attempts = 1, got %d", attempts)
	}
	if claimID != nil {
		t.Errorf("expected publish_claim_id = NULL after backoff, got %v", *claimID)
	}
	if publishedAt != nil {
		t.Errorf("expected published_at = NULL, got %v", *publishedAt)
	}
}

func TestOutboxInvalidEnvelopeFieldsTriggersBackoff(t *testing.T) {
	db, ctx := setupOutboxDB(t)

	eventID := uuid.NewString()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, occurred_at
		) VALUES (
			$1::uuid, 'order', 'ord_bad', 1, '',
			1, '{}', clock_timestamp()
		)
	`, eventID)
	if err != nil {
		t.Fatalf("insert invalid envelope event: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event processed, got %d", processed)
	}

	var attempts int
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts
		FROM outbox_events WHERE event_id = $1::uuid
	`, eventID).Scan(&attempts)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected publish_attempts = 1 after invalid envelope backoff, got %d", attempts)
	}
}

func TestOutboxStaleMalformedReleaseProtection(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	eventID := uuid.NewString()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, occurred_at
		) VALUES (
			$1::uuid, 'order', 'ord_bad', 1, '',
			1, '{}', clock_timestamp()
		)
	`, eventID)
	if err != nil {
		t.Fatalf("insert invalid event: %v", err)
	}

	claimA := uuid.NewString()
	claimB := uuid.NewString()

	_, err = store.ClaimBatch(ctx, db.Pool, claimA, 1, 30*time.Second)
	if err != nil {
		t.Fatalf("claim A: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE outbox_events SET publish_claim_id = $1::uuid WHERE event_id = $2::uuid
	`, claimB, eventID)
	if err != nil {
		t.Fatalf("steal claim: %v", err)
	}

	released, err := store.ReleaseWithBackoff(ctx, db.Pool, eventID, claimA)
	if err != nil {
		t.Fatalf("ReleaseWithBackoff error: %v", err)
	}
	if released {
		t.Errorf("expected ReleaseWithBackoff to return false for stale claim A, got true")
	}

	var currentClaim string
	err = db.Pool.QueryRow(ctx, `SELECT publish_claim_id::text FROM outbox_events WHERE event_id = $1::uuid`, eventID).Scan(&currentClaim)
	if err != nil || currentClaim != claimB {
		t.Errorf("expected claim B to remain owner, got %s (err: %v)", currentClaim, err)
	}
}

func TestOutboxClaimBatchUsesSkipLocked(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env1 := sampleEnvelope()
	env2 := sampleEnvelope()

	for _, env := range []events.EventEnvelope{env1, env2} {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if err := store.Enqueue(ctx, tx, env); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	txLock, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin txLock: %v", err)
	}
	defer func() { _ = txLock.Rollback(ctx) }()

	var lockedID string
	err = txLock.QueryRow(ctx, `
		SELECT event_id::text FROM outbox_events WHERE event_id = $1::uuid FOR UPDATE
	`, env1.EventID).Scan(&lockedID)
	if err != nil {
		t.Fatalf("lock env1 error: %v", err)
	}

	claimIDB := uuid.NewString()
	claimedB, err := store.ClaimBatch(ctx, db.Pool, claimIDB, 10, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimBatch worker B error: %v", err)
	}

	if len(claimedB) != 1 || claimedB[0] != env2.EventID {
		t.Errorf("expected worker B to skip locked event %s and claim %s, got %v",
			env1.EventID, env2.EventID, claimedB)
	}
}

func TestOutboxProcessorLongBatchRenewsNearExpiry(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	envA := sampleEnvelope()
	envB := sampleEnvelope()

	for _, env := range []events.EventEnvelope{envA, envB} {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if err := store.Enqueue(ctx, tx, env); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	var tBBefore, tBAfter time.Time
	outbox.SetTestHookBeforeNextBatchEventForTest(func(claimID string, eventID string, remainingIDs []string) {
		if eventID == envA.EventID {
			// While event A is being processed, set event B near expiry: remaining lease = 5s (<= renewalMargin 10s)
			_, err := db.Pool.Exec(ctx, `
				UPDATE outbox_events
				SET publish_claimed_at = clock_timestamp() - interval '25 seconds'
				WHERE event_id = $1::uuid
			`, envB.EventID)
			if err != nil {
				t.Fatalf("simulate near expiry for event B: %v", err)
			}
			_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, envB.EventID).Scan(&tBBefore)
		} else if eventID == envB.EventID {
			// When event B iteration reaches hook right after RenewBatchNearExpiry, capture renewed timestamp
			_ = db.Pool.QueryRow(ctx, `SELECT publish_claimed_at FROM outbox_events WHERE event_id = $1::uuid`, envB.EventID).Scan(&tBAfter)
		}
	})
	t.Cleanup(func() {
		outbox.ResetTestHookBeforeNextBatchEventForTest()
	})

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}
	if processed != 2 {
		t.Fatalf("expected 2 events processed, got %d", processed)
	}

	fakePub.mu.Lock()
	defer fakePub.mu.Unlock()
	if len(fakePub.published) != 2 {
		t.Fatalf("expected 2 published events, got %d", len(fakePub.published))
	}

	if tBBefore.IsZero() || tBAfter.IsZero() {
		t.Fatalf("expected both tBBefore and tBAfter to be captured, got before=%v, after=%v", tBBefore, tBAfter)
	}
	if !tBAfter.After(tBBefore) {
		t.Errorf("expected near-expiry batch event B publish_claimed_at to move forward, before=%v, after=%v", tBBefore, tBAfter)
	}
}

func TestOutboxLostClaimSkipsNetworkPublish(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimIDOther := uuid.NewString()
	outbox.SetTestHookAfterClaimBatchForTest(func(claimID string, eventIDs []string) {
		_, err := db.Pool.Exec(ctx, `
			UPDATE outbox_events SET publish_claim_id = $1::uuid WHERE event_id = $2::uuid
		`, claimIDOther, env.EventID)
		if err != nil {
			t.Fatalf("steal claim in hook: %v", err)
		}
	})
	t.Cleanup(func() {
		outbox.ResetTestHookAfterClaimBatchForTest()
	})

	fakePub := &fakePublisher{}
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	_, err = proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("ProcessBatch error: %v", err)
	}

	fakePub.mu.Lock()
	pubCount := len(fakePub.published)
	fakePub.mu.Unlock()
	if pubCount != 0 {
		t.Errorf("expected network publish call count = 0 on lost claim, got %d", pubCount)
	}

	var currentClaim string
	var attempts int
	var publishedAt *time.Time
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_claim_id::text, publish_attempts, published_at
		FROM outbox_events WHERE event_id = $1::uuid
	`, env.EventID).Scan(&currentClaim, &attempts, &publishedAt)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}
	if currentClaim != claimIDOther {
		t.Errorf("expected claim B (%s) to remain untouched owner, got %s", claimIDOther, currentClaim)
	}
	if attempts != 0 {
		t.Errorf("expected publish_attempts = 0 (unmodified by stale owner), got %d", attempts)
	}
	if publishedAt != nil {
		t.Errorf("expected published_at = NULL, got %v", publishedAt)
	}
}

type errorPublisher struct {
	err error
}

func (e *errorPublisher) PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error {
	return e.err
}

func TestOutboxNACKSchedulesBackoff(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	nackPub := &errorPublisher{err: errors.New("rabbitmq publish nacked by broker for message_id " + env.EventID)}
	proc := outbox.NewProcessor(cfg, db.Pool, nackPub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("expected ProcessBatch to return nil error on NACK (event-level backoff), got %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event processed, got %d", processed)
	}

	var attempts int
	var claimID *string
	var publishedAt *time.Time
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts, publish_claim_id::text, published_at
		FROM outbox_events WHERE event_id = $1::uuid
	`, env.EventID).Scan(&attempts, &claimID, &publishedAt)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected publish_attempts = 1 after NACK backoff, got %d", attempts)
	}
	if claimID != nil {
		t.Errorf("expected publish_claim_id = NULL after NACK backoff, got %v", *claimID)
	}
	if publishedAt != nil {
		t.Errorf("expected published_at = NULL, got %v", *publishedAt)
	}
}

func TestOutboxConfirmTimeoutBackoff(t *testing.T) {
	db, ctx := setupOutboxDB(t)
	store := outbox.NewStore()

	env := sampleEnvelope()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := store.Enqueue(ctx, tx, env); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	timeoutPub := &errorPublisher{err: context.DeadlineExceeded}
	proc := outbox.NewProcessor(cfg, db.Pool, timeoutPub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil {
		t.Fatalf("expected ProcessBatch to return nil error on confirm timeout (event-level backoff), got %v", err)
	}
	if processed != 1 {
		t.Fatalf("expected 1 event processed, got %d", processed)
	}

	var attempts int
	var claimID *string
	var publishedAt *time.Time
	err = db.Pool.QueryRow(ctx, `
		SELECT publish_attempts, publish_claim_id::text, published_at
		FROM outbox_events WHERE event_id = $1::uuid
	`, env.EventID).Scan(&attempts, &claimID, &publishedAt)
	if err != nil {
		t.Fatalf("query event state: %v", err)
	}

	if attempts != 1 {
		t.Errorf("expected publish_attempts = 1 after confirm timeout backoff, got %d", attempts)
	}
	if claimID != nil {
		t.Errorf("expected publish_claim_id = NULL after confirm timeout backoff, got %v", *claimID)
	}
	if publishedAt != nil {
		t.Errorf("expected published_at = NULL, got %v", *publishedAt)
	}
}

func TestOutboxPayloadLargeInt64Precision(t *testing.T) {
	db, ctx := setupOutboxDB(t)

	largeIntStr := "9223372036854775800"
	eventID := uuid.NewString()

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, occurred_at
		) VALUES (
			$1::uuid, 'order', 'ord_large', 1, 'commerce.order.created.v1',
			1, ('{"large_amount": ' || $2 || '}')::jsonb, clock_timestamp()
		)
	`, eventID, largeIntStr)
	if err != nil {
		t.Fatalf("insert large int64 payload: %v", err)
	}

	fakePub := &fakePublisher{}
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	processed, err := proc.ProcessBatch(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("ProcessBatch expected 1, got %d (err: %v)", processed, err)
	}

	fakePub.mu.Lock()
	defer fakePub.mu.Unlock()
	if len(fakePub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(fakePub.published))
	}

	pubPayloadJSON, err := json.Marshal(fakePub.published[0].Payload)
	if err != nil {
		t.Fatalf("marshal published payload: %v", err)
	}
	if !bytes.Contains(pubPayloadJSON, []byte(largeIntStr)) {
		t.Errorf("expected published payload to preserve exact large int64 string %s without precision loss, got %s",
			largeIntStr, string(pubPayloadJSON))
	}
}

func TestMigration000013ExistingRowCompatibility(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	ctx := context.Background()

	content000001, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_event_delivery_foundation.up.sql"))
	if err != nil {
		t.Fatalf("read 000001: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, string(content000001)); err != nil {
		t.Fatalf("apply 000001: %v", err)
	}

	eventID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, occurred_at
		) VALUES (
			$1::uuid, 'order', 'ord_pre', 1, 'commerce.order.created.v1',
			1, '{}', clock_timestamp()
		)
	`, eventID)
	if err != nil {
		t.Fatalf("insert pre-000013 event: %v", err)
	}

	content000013, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000013_outbox_publish_claims.up.sql"))
	if err != nil {
		t.Fatalf("read 000013: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, string(content000013)); err != nil {
		t.Fatalf("apply 000013 up: %v", err)
	}

	var claimID *string
	var claimedAt *time.Time
	var attempts int
	var nextAttemptAt *time.Time

	err = db.Pool.QueryRow(ctx, `
		SELECT publish_claim_id::text, publish_claimed_at, publish_attempts, next_attempt_at
		FROM outbox_events WHERE event_id = $1::uuid
	`, eventID).Scan(&claimID, &claimedAt, &attempts, &nextAttemptAt)
	if err != nil {
		t.Fatalf("query migrated row: %v", err)
	}

	if claimID != nil {
		t.Errorf("expected publish_claim_id = NULL, got %v", *claimID)
	}
	if claimedAt != nil {
		t.Errorf("expected publish_claimed_at = NULL, got %v", *claimedAt)
	}
	if attempts != 0 {
		t.Errorf("expected publish_attempts = 0, got %d", attempts)
	}
	if nextAttemptAt == nil {
		t.Errorf("expected next_attempt_at NOT NULL, got nil")
	}

	store := outbox.NewStore()
	myClaim := uuid.NewString()
	claimed, err := store.ClaimBatch(ctx, db.Pool, myClaim, 1, 30*time.Second)
	if err != nil || len(claimed) != 1 || claimed[0] != eventID {
		t.Errorf("expected migrated pre-P5.6 row to be claimable immediately, got %v (err: %v)", claimed, err)
	}

	var indexDef string
	err = db.Pool.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE tablename = 'outbox_events' AND indexname = 'outbox_events_unpublished_claim_idx'
		AND schemaname = current_schema()
	`).Scan(&indexDef)
	if err != nil {
		t.Fatalf("query indexdef: %v", err)
	}
	if !bytes.Contains([]byte(indexDef), []byte("published_at IS NULL")) {
		t.Errorf("expected index predicate to contain 'published_at IS NULL', got %s", indexDef)
	}
}

func TestMigration000013UpDownUpSafety(t *testing.T) {
	db, ctx := setupOutboxDB(t)

	downContent, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000013_outbox_publish_claims.down.sql"))
	if err != nil {
		t.Fatalf("read migration 000013 down: %v", err)
	}
	upContent, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000013_outbox_publish_claims.up.sql"))
	if err != nil {
		t.Fatalf("read migration 000013 up: %v", err)
	}

	if _, err := db.Pool.Exec(ctx, string(downContent)); err != nil {
		t.Fatalf("migration 000013 down failed: %v", err)
	}

	if _, err := db.Pool.Exec(ctx, string(upContent)); err != nil {
		t.Fatalf("migration 000013 up again failed: %v", err)
	}
}

func TestOutboxClaimIDGenerationFailure(t *testing.T) {
	db, ctx := setupOutboxDB(t)

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	fakePub := &fakePublisher{}
	proc := outbox.NewProcessor(cfg, db.Pool, fakePub, nil)

	outbox.SetNewClaimUUIDForTest(func() (uuid.UUID, error) {
		return uuid.Nil, errors.New("simulated entropy failure")
	})
	t.Cleanup(func() {
		outbox.ResetNewClaimUUIDForTest()
	})

	processed, err := proc.ProcessBatch(ctx)
	if err == nil {
		t.Fatal("expected error on claim ID generation failure, got nil")
	}
	if processed != 0 {
		t.Errorf("expected 0 rows processed, got %d", processed)
	}
}
