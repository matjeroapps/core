package testdb_test

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/matjeroapps/core/internal/testdb"
)

func TestConcurrentDBSetup(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			subTestName := fmt.Sprintf("Worker_%d", workerID)
			t.Run(subTestName, func(st *testing.T) {
				pool := testdb.Open(st, dsn)
				if pool == nil {
					st.Fatal("expected non-nil pool")
				}
				var currentSchema string
				if err := pool.QueryRow(st.Context(), "SELECT current_schema()").Scan(&currentSchema); err != nil {
					st.Fatalf("query current_schema: %v", err)
				}
				if currentSchema == "" || currentSchema == "public" {
					st.Fatalf("expected isolated schema, got %q", currentSchema)
				}
			})
		}()
	}

	wg.Wait()
}
