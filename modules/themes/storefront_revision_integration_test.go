package themes

import (
	"os"
	"testing"

	"core/internal/testdb"
)

// Theme writes and the public storefront cache generation.
//
// The bootstrap payload carries the store's published theme and published
// configuration, so a theme change is a public output change. Draft work is not:
// it stays invisible until it is published, and invalidating on every draft edit
// would throw away a store's whole public cache for content no customer can see.

func (e themesTestEnv) storefrontRevision(t *testing.T, storeID string) int64 {
	t.Helper()
	var revision int64
	if err := e.pool.Pool.QueryRow(e.ctx, `
		SELECT COALESCE((SELECT revision FROM storefront_revisions WHERE store_id = $1), 1)
	`, storeID).Scan(&revision); err != nil {
		t.Fatalf("read storefront revision: %v", err)
	}
	return revision
}

// expectRevisionBump asserts which stores a theme write invalidated, so a bump
// that is too wide fails as loudly as one that is missing.
func (e themesTestEnv) expectRevisionBump(t *testing.T, name string, bumpA, bumpB bool, write func() error) {
	t.Helper()
	beforeA, beforeB := e.storefrontRevision(t, e.storeA), e.storefrontRevision(t, e.storeB)
	if err := write(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	afterA, afterB := e.storefrontRevision(t, e.storeA), e.storefrontRevision(t, e.storeB)

	assertThemeRevision(t, name+": store A", beforeA, afterA, bumpA)
	assertThemeRevision(t, name+": store B", beforeB, afterB, bumpB)
}

func assertThemeRevision(t *testing.T, label string, before, after int64, expectBump bool) {
	t.Helper()
	switch {
	case expectBump && after <= before:
		t.Errorf("%s: revision did not advance (%d -> %d)", label, before, after)
	case !expectBump && after != before:
		t.Errorf("%s: revision advanced unexpectedly (%d -> %d)", label, before, after)
	}
}

func TestThemeInstallBumpsStorefrontRevision(t *testing.T) {
	e := setupThemesTest(t)
	version := e.defaultThemeVersion(t)

	e.expectRevisionBump(t, "install a theme", true, false, func() error {
		_, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, version.Version)
		return err
	})
}

// Publishing is the moment configuration becomes public, so it is the only
// configuration write that may advance the generation.
func TestThemePublishBumpsStorefrontRevisionButDraftDoesNot(t *testing.T) {
	e := setupThemesTest(t)
	version := e.defaultThemeVersion(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, version.Version); err != nil {
		t.Fatalf("install theme: %v", err)
	}

	e.expectRevisionBump(t, "edit the draft", false, false, func() error {
		_, err := e.svc.UpdateDraftConfiguration(e.ctx, e.sellerA, e.storeA, map[string]any{
			"logo": "https://cdn.matjero.test/draft.png",
		})
		return err
	})

	e.expectRevisionBump(t, "publish the draft", true, false, func() error {
		_, err := e.svc.PublishConfiguration(e.ctx, e.sellerA, e.storeA)
		return err
	})

	e.expectRevisionBump(t, "discard a draft", false, false, func() error {
		if _, err := e.svc.UpdateDraftConfiguration(e.ctx, e.sellerA, e.storeA, map[string]any{
			"logo": "https://cdn.matjero.test/second-draft.png",
		}); err != nil {
			return err
		}
		_, err := e.svc.DiscardDraft(e.ctx, e.sellerA, e.storeA)
		return err
	})

	e.expectRevisionBump(t, "issue a preview token", false, false, func() error {
		_, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
		return err
	})
}

// The bootstrap payload names the installed version, so an upgrade changes public
// output even when the configuration is untouched.
func TestThemeUpgradeBumpsStorefrontRevision(t *testing.T) {
	e := setupThemesTest(t)
	version := e.defaultThemeVersion(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, version.Version); err != nil {
		t.Fatalf("install theme: %v", err)
	}
	next, err := e.repo.CreateThemeVersion(e.ctx, version.ThemeID, "9.9.9", ThemeVersionStatusPublished,
		version.ConfigurationSchema, version.DefaultConfiguration, version.ComponentRegistryVersion)
	if err != nil {
		t.Fatalf("create target version: %v", err)
	}

	e.expectRevisionBump(t, "upgrade the installation", true, false, func() error {
		return e.svc.UpgradeInstallation(e.ctx, e.sellerA, e.storeA, next.Version)
	})
}

// Leaving a store with no active theme changes its bootstrap payload.
func TestThemeDeactivationBumpsStorefrontRevision(t *testing.T) {
	e := setupThemesTest(t)
	version := e.defaultThemeVersion(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, version.Version); err != nil {
		t.Fatalf("install theme: %v", err)
	}

	e.expectRevisionBump(t, "deactivate installations", true, false, func() error {
		return e.repo.DeactivateInstallations(e.ctx, e.storeA)
	})
}

// A failed publish must leave the generation untouched, otherwise a store's cache
// would be discarded for content that was never published.
func TestThemeFailedPublishDoesNotBumpStorefrontRevision(t *testing.T) {
	e := setupThemesTest(t)
	version := e.defaultThemeVersion(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, version.Version); err != nil {
		t.Fatalf("install theme: %v", err)
	}
	installation, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	config, err := e.repo.GetConfiguration(e.ctx, installation.ID)
	if err != nil {
		t.Fatalf("get configuration: %v", err)
	}

	before := e.storefrontRevision(t, e.storeA)
	// A stale expected draft revision is exactly the time-of-check/time-of-use
	// rejection PublishConfiguration exists to enforce.
	if _, err := e.repo.PublishConfiguration(e.ctx, installation.ID, config.DraftRevision+1); err == nil {
		t.Fatal("expected the publish to be rejected")
	}
	if after := e.storefrontRevision(t, e.storeA); after != before {
		t.Fatalf("revision advanced on a rejected publish (%d -> %d)", before, after)
	}
}

// The theme migration must remain independently reversible now that theme writes
// touch the revision table.
func TestThemeMigrationReplayKeepsRevisionsIntact(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	for _, m := range []string{
		"000002_market_reference_data.up.sql",
		"000003_commerce_domain_schema.up.sql",
		"000007_theme_engine_schema.up.sql",
		"000008_storefront_revisions.up.sql",
	} {
		applyMigration(t, db, m)
	}
	for _, m := range []string{
		"000008_storefront_revisions.down.sql",
		"000007_theme_engine_schema.down.sql",
	} {
		applyMigration(t, db, m)
	}
	for _, m := range []string{
		"000007_theme_engine_schema.up.sql",
		"000008_storefront_revisions.up.sql",
	} {
		applyMigration(t, db, m)
	}
}
