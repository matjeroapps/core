package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultLockID int64 = 260025040

type Migration struct {
	ID       string
	Name     string
	Path     string
	SQL      string
	Checksum string
}

type AppliedMigration struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Checksum  string    `json:"checksum"`
	AppliedAt time.Time `json:"applied_at"`
}

type Status struct {
	Database        string   `json:"database,omitempty"`
	LatestAvailable string   `json:"latest_available"`
	AppliedCount    int      `json:"applied_count"`
	PendingCount    int      `json:"pending_count"`
	Pending         []string `json:"pending"`
	Dirty           bool     `json:"dirty"`
	Locked          bool     `json:"locked"`
}

type Runner struct {
	Pool       *pgxpool.Pool
	Migrations []Migration
	LockID     int64
}

func LoadDir(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
		body, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", path, err)
		}
		name := strings.TrimSuffix(entry.Name(), ".up.sql")
		id := name
		if idx := strings.Index(name, "_"); idx > 0 {
			id = name[:idx]
		}
		sum := sha256.Sum256(body)
		migrations = append(migrations, Migration{
			ID:       id,
			Name:     name,
			Path:     path,
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Name < migrations[j].Name
	})
	return migrations, nil
}

func (r Runner) Up(ctx context.Context) (Status, error) {
	if r.Pool == nil {
		return Status{}, fmt.Errorf("migration runner requires a database pool")
	}
	lockID := r.lockID()
	conn, err := r.Pool.Acquire(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		return Status{}, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)
	}()

	if err := ensureMetadata(ctx, conn); err != nil {
		return Status{}, err
	}
	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return Status{}, err
	}

	for _, migration := range r.Migrations {
		if existing, ok := applied[migration.Name]; ok {
			if existing.Checksum != migration.Checksum {
				return Status{}, fmt.Errorf("migration %s checksum mismatch", migration.Name)
			}
			continue
		}
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return Status{}, fmt.Errorf("begin migration %s: %w", migration.Name, err)
		}
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return Status{}, fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO schema_migrations (version, name, checksum, applied_at)
			VALUES ($1, $2, $3, NOW())
		`, migration.ID, migration.Name, migration.Checksum); err != nil {
			_ = tx.Rollback(ctx)
			return Status{}, fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Status{}, fmt.Errorf("commit migration %s: %w", migration.Name, err)
		}
		applied[migration.Name] = AppliedMigration{
			ID:       migration.ID,
			Name:     migration.Name,
			Checksum: migration.Checksum,
		}
	}

	return r.Status(ctx)
}

func (r Runner) Status(ctx context.Context) (Status, error) {
	if r.Pool == nil {
		return Status{}, fmt.Errorf("migration runner requires a database pool")
	}
	if err := ensureMetadata(ctx, r.Pool); err != nil {
		return Status{}, err
	}
	applied, err := loadApplied(ctx, r.Pool)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		LatestAvailable: latestAvailable(r.Migrations),
		AppliedCount:    len(applied),
		Pending:         []string{},
		Dirty:           false,
		Locked:          false,
	}
	for _, migration := range r.Migrations {
		existing, ok := applied[migration.Name]
		if !ok {
			status.Pending = append(status.Pending, migration.Name)
			continue
		}
		if existing.Checksum != migration.Checksum {
			status.Dirty = true
		}
	}
	status.PendingCount = len(status.Pending)
	return status, nil
}

func (s Status) JSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func (r Runner) lockID() int64 {
	if r.LockID != 0 {
		return r.LockID
	}
	return defaultLockID
}

func latestAvailable(migrations []Migration) string {
	if len(migrations) == 0 {
		return ""
	}
	return migrations[len(migrations)-1].Name
}
