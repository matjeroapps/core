package migrations

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"core/internal/testdb"
)

func TestLoadDirOrdersMigrationsAndComputesChecksums(t *testing.T) {
	got, err := LoadDir(fstest.MapFS{
		"migrations/000002_second.up.sql":  {Data: []byte("SELECT 2;")},
		"migrations/000001_first.up.sql":   {Data: []byte("SELECT 1;")},
		"migrations/000001_first.down.sql": {Data: []byte("SELECT 0;")},
	}, "migrations")
	if err != nil {
		t.Fatalf("LoadDir returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "000001_first" || got[1].Name != "000002_second" {
		t.Fatalf("order = %q, %q", got[0].Name, got[1].Name)
	}
	if got[0].Checksum == "" {
		t.Fatal("checksum must be set")
	}
}

func TestRunnerAppliesMigrationsAndRerunIsNoop(t *testing.T) {
	pool := openPool(t)
	runner := Runner{Pool: pool, Migrations: testMigrations()}

	status, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("Up returned error: %v", err)
	}
	if status.PendingCount != 0 {
		t.Fatalf("pending = %d, want 0", status.PendingCount)
	}

	second, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("second Up returned error: %v", err)
	}
	if second.AppliedCount != status.AppliedCount || second.PendingCount != 0 {
		t.Fatalf("second status = %+v, want same applied count and no pending", second)
	}
}

func TestRunnerContinuesPartiallyMigratedDatabase(t *testing.T) {
	pool := openPool(t)
	runner := Runner{Pool: pool, Migrations: testMigrations()[:1]}
	if _, err := runner.Up(context.Background()); err != nil {
		t.Fatalf("initial Up returned error: %v", err)
	}

	full := Runner{Pool: pool, Migrations: testMigrations()}
	status, err := full.Up(context.Background())
	if err != nil {
		t.Fatalf("partial Up returned error: %v", err)
	}
	if status.PendingCount != 0 || status.AppliedCount != 2 {
		t.Fatalf("status = %+v, want two applied and no pending", status)
	}
}

func TestRunnerDoesNotRecordFailedMigration(t *testing.T) {
	pool := openPool(t)
	runner := Runner{Pool: pool, Migrations: []Migration{{
		ID:       "000001",
		Name:     "000001_bad",
		SQL:      "SELECT * FROM definitely_missing_table;",
		Checksum: "bad",
	}}}

	if _, err := runner.Up(context.Background()); err == nil {
		t.Fatal("Up returned nil error for failing migration")
	}
	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.AppliedCount != 0 || status.PendingCount != 1 {
		t.Fatalf("status = %+v, want failed migration left pending", status)
	}
}

func TestRunnerSerializesConcurrentExecution(t *testing.T) {
	pool := openPool(t)
	runner := Runner{Pool: pool, Migrations: testMigrations()}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := runner.Up(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Up returned error: %v", err)
		}
	}
	status, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.PendingCount != 0 || status.AppliedCount != 2 {
		t.Fatalf("status = %+v, want two applied and no pending", status)
	}
}

func TestCoreMigrationsInclude000040(t *testing.T) {
	got, err := LoadDir(os.DirFS("../.."), "migrations")
	if err != nil {
		t.Fatalf("LoadDir returned error: %v", err)
	}
	var found bool
	for _, migration := range got {
		if strings.HasPrefix(migration.Name, "000040_merchant_membership_subject_index") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("000040_merchant_membership_subject_index migration was not discovered")
	}
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for migration runner integration tests")
	}
	return testdb.Open(t, dsn).Pool
}

func testMigrations() []Migration {
	return []Migration{
		{
			ID:       "000001",
			Name:     "000001_first",
			SQL:      "CREATE TABLE migration_runner_example (id TEXT PRIMARY KEY);",
			Checksum: "first",
		},
		{
			ID:       "000002",
			Name:     "000002_second",
			SQL:      "ALTER TABLE migration_runner_example ADD COLUMN name TEXT;",
			Checksum: "second",
		},
	}
}
