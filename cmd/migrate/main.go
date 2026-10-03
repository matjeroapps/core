package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"core/internal/migrations"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate up|status [--format json]")
	}
	mode := args[0]
	format := "text"
	for i := 1; i < len(args); i++ {
		if args[i] == "--format" && i+1 < len(args) {
			format = args[i+1]
			i++
			continue
		}
		return fmt.Errorf("unknown argument: %s", args[i])
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported format %q", format)
	}

	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	loaded, err := migrations.LoadDir(os.DirFS("."), "migrations")
	if err != nil {
		return err
	}
	runner := migrations.Runner{Pool: pool, Migrations: loaded}

	var status migrations.Status
	switch mode {
	case "up":
		status, err = runner.Up(ctx)
	case "status":
		status, err = runner.Status(ctx)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	if err != nil {
		return err
	}
	status.Database = redactDatabase(dsn)
	if format == "json" {
		body, err := status.JSON()
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(body))
		return nil
	}
	fmt.Fprintf(stdout, "latest_available=%s applied_count=%d pending_count=%d dirty=%t locked=%t\n",
		status.LatestAvailable,
		status.AppliedCount,
		status.PendingCount,
		status.Dirty,
		status.Locked,
	)
	if len(status.Pending) > 0 {
		fmt.Fprintf(stdout, "pending=%s\n", strings.Join(status.Pending, ","))
	}
	_ = stderr
	return nil
}

func redactDatabase(dsn string) string {
	if at := strings.LastIndex(dsn, "@"); at >= 0 {
		schemeEnd := strings.Index(dsn, "://")
		if schemeEnd >= 0 && schemeEnd < at {
			return dsn[:schemeEnd+3] + "[REDACTED]@" + dsn[at+1:]
		}
	}
	return "[REDACTED]"
}
