package commerce

import (
	"path/filepath"
	"testing"

	"core/internal/testdb"
	"core/packages/database"
)

func applyMigrationBatch(t *testing.T, db *database.Pool, names ...string) {
	t.Helper()

	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, paths...)
}
