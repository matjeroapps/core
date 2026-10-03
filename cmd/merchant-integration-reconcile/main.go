package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"core/internal/integration"
)

func main() {
	dryRunFlag := flag.Bool("dry-run", false, "Perform migration scan and print accounting summary without database writes")
	applyFlag := flag.Bool("apply", false, "Execute migration and write canonical Merchant connections and crosswalk records")
	flag.Parse()

	if !*dryRunFlag && !*applyFlag {
		fmt.Println("Error: must specify either --dry-run or --apply flag")
		flag.Usage()
		os.Exit(1)
	}

	dryRun := *dryRunFlag
	if *applyFlag {
		dryRun = false
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		fmt.Println("Error: TEST_DATABASE_URL or DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Error: failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := integration.NewMerchantRepository(pool)
	svc := integration.NewMerchantService(repo, pool)

	summary, err := svc.ReconcileLegacyConnections(ctx, dryRun)
	if err != nil {
		fmt.Printf("Error: reconciliation failed: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(out))
}
