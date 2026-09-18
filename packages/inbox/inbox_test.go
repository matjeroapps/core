package inbox_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testdb"
	"core/packages/database"
	"core/packages/inbox"
)

func setupInboxDB(t *testing.T) (*database.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)

	content, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000001_event_delivery_foundation.up.sql"))
	if err != nil {
		t.Fatalf("read migration 000001: %v", err)
	}
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
		t.Fatalf("apply migration 000001: %v", err)
	}

	return db, ctx
}

func TestInboxDuplicateSameConsumerProcessedOnce(t *testing.T) {
	db, ctx := setupInboxDB(t)
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	store := inbox.NewStore()
	consumer := "orders-worker"
	eventID := "11111111-1111-1111-1111-111111111111"

	first, err := store.RecordProcessed(ctx, tx, consumer, eventID)
	if err != nil {
		t.Fatalf("first RecordProcessed: %v", err)
	}
	if !first {
		t.Errorf("expected first call to return true (processed), got false")
	}

	second, err := store.RecordProcessed(ctx, tx, consumer, eventID)
	if err != nil {
		t.Fatalf("second RecordProcessed: %v", err)
	}
	if second {
		t.Errorf("expected second call to return false (duplicate), got true")
	}
}

func TestInboxSameEventDifferentConsumersEachProcessOnce(t *testing.T) {
	db, ctx := setupInboxDB(t)
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	store := inbox.NewStore()
	eventID := "22222222-2222-2222-2222-222222222222"

	consumerA := "orders-worker"
	consumerB := "analytics-worker"

	resA, err := store.RecordProcessed(ctx, tx, consumerA, eventID)
	if err != nil {
		t.Fatalf("consumerA RecordProcessed: %v", err)
	}
	if !resA {
		t.Errorf("expected consumerA to process event, got false")
	}

	resB, err := store.RecordProcessed(ctx, tx, consumerB, eventID)
	if err != nil {
		t.Fatalf("consumerB RecordProcessed: %v", err)
	}
	if !resB {
		t.Errorf("expected consumerB to process event independently, got false")
	}
}
