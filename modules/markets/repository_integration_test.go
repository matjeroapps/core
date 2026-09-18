package markets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"core/internal/testdb"
	"core/packages/database"
	"core/packages/i18n"
)

func TestRepositoryReadsSeededMarkets(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}

	ctx := context.Background()
	db := testdb.Open(t, dsn)

	applySQLFile(t, db, filepath.Join("..", "..", "migrations", "000002_market_reference_data.up.sql"))

	repo := NewRepository(db.Pool)

	marketsArabic, err := repo.List(ctx, i18n.LocaleArabic)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(marketsArabic) != 3 {
		t.Fatalf("expected 3 markets, got %d", len(marketsArabic))
	}

	marketsByCode := make(map[string]Market, len(marketsArabic))
	for _, market := range marketsArabic {
		marketsByCode[market.Code] = market
	}
	if got := marketsByCode["EG"].Country.Name; got != "مصر" {
		t.Fatalf("EG country name = %q", got)
	}
	if got := marketsByCode["SA"].Country.Name; got != "السعودية" {
		t.Fatalf("SA country name = %q", got)
	}
	if got := marketsByCode["AE"].Country.Name; got != "الإمارات العربية المتحدة" {
		t.Fatalf("AE country name = %q", got)
	}

	eg, err := repo.GetByCode(ctx, "EG", i18n.LocaleEnglish)
	if err != nil {
		t.Fatalf("GetByCode returned error: %v", err)
	}
	if eg.Country.Name != "Egypt" {
		t.Fatalf("EG country name = %q", eg.Country.Name)
	}
	if len(eg.SupportedLocales) != 2 {
		t.Fatalf("EG supported locales = %v", eg.SupportedLocales)
	}
}

func applySQLFile(t *testing.T, db *database.Pool, path string) {
	t.Helper()

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	if _, err := db.Exec(context.Background(), string(sqlBytes)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}
