package themes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

type themesTestEnv struct {
	ctx     context.Context
	pool    *database.Pool
	repo    Repository
	svc     Service
	sellerA string
	sellerB string
	storeA  string
	storeB  string
}

func applyMigration(t *testing.T, db *database.Pool, name string) {
	t.Helper()
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	if _, err := db.Exec(context.Background(), string(sqlBytes)); err != nil {
		t.Fatalf("apply migration %s: %v", name, err)
	}
}

func setupThemesTest(t *testing.T) themesTestEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)

	for _, m := range []string{
		"000002_market_reference_data.up.sql",
		"000003_commerce_domain_schema.up.sql",
		"000004_admin_supplier_seller_platforms.up.sql",
		"000005_store_domain_lifecycle.up.sql",
		"000006_store_domain_integrity.up.sql",
		"000007_theme_engine_schema.up.sql",
		"000008_storefront_revisions.up.sql",
	} {
		applyMigration(t, db, m)
	}

	sellerA := uuid.NewString()
	sellerB := uuid.NewString()
	storeA := uuid.NewString()
	storeB := uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO sellers (id, code, name, status) VALUES ($1,'seller-a','Seller A','active'),($2,'seller-b','Seller B','active')`, sellerA, sellerB); err != nil {
		t.Fatalf("insert sellers: %v", err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO stores (id, seller_id, market_code, code, name, status)
		VALUES ($1,$2,'EG','store-a','Store A','active'),($3,$4,'EG','store-b','Store B','active')`,
		storeA, sellerA, storeB, sellerB); err != nil {
		t.Fatalf("insert stores: %v", err)
	}

	repo := NewRepository(db.Pool)
	commerceRepo := commerce.NewRepository(db.Pool)
	svc := NewService(repo, commerceRepo, Options{PreviewSecret: []byte("test-secret")})
	if err := svc.SeedBuiltInThemes(ctx); err != nil {
		t.Fatalf("seed built-in themes: %v", err)
	}
	return themesTestEnv{ctx: ctx, pool: db, repo: repo, svc: svc, sellerA: sellerA, sellerB: sellerB, storeA: storeA, storeB: storeB}
}

func (e themesTestEnv) defaultThemeVersion(t *testing.T) ThemeVersion {
	t.Helper()
	theme, err := e.repo.GetThemeByKey(e.ctx, DefaultThemeKey)
	if err != nil {
		t.Fatalf("get default theme: %v", err)
	}
	v, err := e.repo.GetLatestPublishedVersion(e.ctx, theme.ID)
	if err != nil {
		t.Fatalf("get default version: %v", err)
	}
	return v
}

func defaultConfigWithHeroTitle(t *testing.T, title string) map[string]any {
	t.Helper()
	data, err := json.Marshal(DefaultConfiguration)
	if err != nil {
		t.Fatalf("marshal default config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("clone default config: %v", err)
	}
	hero, ok := cfg["hero"].(map[string]any)
	if !ok {
		t.Fatal("default config hero is not an object")
	}
	hero["title"] = title
	return cfg
}

func TestThemePersistence(t *testing.T) {
	e := setupThemesTest(t)
	theme, err := e.repo.CreateTheme(e.ctx, "custom-1", "Custom One", "desc", ThemeTypeFree, ThemeStatusActive)
	if err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}
	got, err := e.repo.GetTheme(e.ctx, theme.ID)
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if got.Key != "custom-1" || got.Type != ThemeTypeFree {
		t.Fatalf("unexpected theme: %+v", got)
	}
	themes, err := e.repo.ListThemes(e.ctx)
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	if len(themes) < 2 { // default + custom
		t.Fatalf("expected at least 2 themes, got %d", len(themes))
	}
}

func TestVersionUniqueness(t *testing.T) {
	e := setupThemesTest(t)
	theme, err := e.repo.CreateTheme(e.ctx, "custom-2", "Custom Two", "desc", ThemeTypeFree, ThemeStatusActive)
	if err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}
	if _, err := e.repo.CreateThemeVersion(e.ctx, theme.ID, "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0"); err != nil {
		t.Fatalf("first version: %v", err)
	}
	if _, err := e.repo.CreateThemeVersion(e.ctx, theme.ID, "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0"); err == nil {
		t.Fatal("expected duplicate (theme, version) to conflict")
	} else if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestOneActiveInstallationPerStore(t *testing.T) {
	e := setupThemesTest(t)
	v := e.defaultThemeVersion(t)
	if _, err := e.repo.CreateInstallation(e.ctx, e.storeA, v.ThemeID, v.ID, ThemeInstallationStatusActive); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// second active installation for same store must deactivate the first
	if err := e.repo.DeactivateInstallations(e.ctx, e.storeA); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	v2, err := e.repo.CreateThemeVersion(e.ctx, v.ThemeID, "2.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0")
	if err != nil {
		t.Fatalf("second version: %v", err)
	}
	if _, err := e.repo.CreateInstallation(e.ctx, e.storeA, v2.ThemeID, v2.ID, ThemeInstallationStatusActive); err != nil {
		t.Fatalf("second install: %v", err)
	}
	var activeCount int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM theme_installations WHERE store_id=$1 AND status='active'`, e.storeA).Scan(&activeCount); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active installation, got %d", activeCount)
	}
}

func TestConfigurationPersistenceAndRevisions(t *testing.T) {
	e := setupThemesTest(t)
	v := e.defaultThemeVersion(t)
	inst, err := e.repo.CreateInstallation(e.ctx, e.storeA, v.ThemeID, v.ID, ThemeInstallationStatusActive)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.repo.CreateConfiguration(e.ctx, inst.ID, v.DefaultConfiguration, v.DefaultConfiguration); err != nil {
		t.Fatalf("create config: %v", err)
	}
	draft := map[string]any{"hero": map[string]any{"title": "New"}}
	rev, err := e.repo.UpdateDraftConfiguration(e.ctx, inst.ID, draft)
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if rev != 1 {
		t.Fatalf("expected draft revision 1, got %d", rev)
	}
	cfg, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.DraftRevision != 1 || cfg.PublishedRevision != 0 {
		t.Fatalf("unexpected revisions: draft=%d published=%d", cfg.DraftRevision, cfg.PublishedRevision)
	}
}

func TestPublishCopiesDraftAndBumpsRevision(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	newDraft := map[string]any{"hero": map[string]any{"title": "Published Title"}}
	if _, err := e.svc.UpdateDraftConfiguration(e.ctx, e.sellerA, e.storeA, newDraft); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	pubRev, err := e.svc.PublishConfiguration(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pubRev != 1 {
		t.Fatalf("expected published revision 1, got %d", pubRev)
	}
	inst, cfg, err := e.svc.GetInstallation(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	if inst.ID == "" {
		t.Fatal("missing installation")
	}
	hero, _ := cfg.PublishedConfig["hero"].(map[string]any)
	if hero["title"] != "Published Title" {
		t.Fatalf("published config not updated: %+v", cfg.PublishedConfig)
	}
	if cfg.PublishedRevision != 1 {
		t.Fatalf("expected published revision 1, got %d", cfg.PublishedRevision)
	}
}

func TestPublishInvalidDraftDoesNotPartialPublish(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Write an invalid (unsafe) draft directly through the repository, bypassing
	// service validation, to exercise the atomic publish guard.
	inst, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	unsafe := map[string]any{"hero": map[string]any{"title": "<script>alert(1)</script>"}}
	if _, err := e.repo.UpdateDraftConfiguration(e.ctx, inst.ID, unsafe); err != nil {
		t.Fatalf("seed unsafe draft: %v", err)
	}
	if _, err := e.svc.PublishConfiguration(e.ctx, e.sellerA, e.storeA); err == nil {
		t.Fatal("expected publish of unsafe draft to fail")
	}
	cfg, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.PublishedRevision != 0 {
		t.Fatalf("published revision should remain 0 after failed publish, got %d", cfg.PublishedRevision)
	}
}

func TestFKConstraints(t *testing.T) {
	e := setupThemesTest(t)
	v := e.defaultThemeVersion(t)
	if _, err := e.repo.CreateInstallation(e.ctx, "non-existent-store", v.ThemeID, v.ID, ThemeInstallationStatusActive); err == nil {
		t.Fatal("expected FK violation for invalid store_id")
	}
	if _, err := e.repo.CreateThemeVersion(e.ctx, "non-existent-theme", "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0"); err == nil {
		t.Fatal("expected FK violation for invalid theme_id")
	}
}

func TestInstallSwitchAndUpgrade(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install default: %v", err)
	}
	// Upgrade to a newer published version.
	theme, err := e.repo.GetThemeByKey(e.ctx, DefaultThemeKey)
	if err != nil {
		t.Fatalf("get theme: %v", err)
	}
	if _, err := e.repo.CreateThemeVersion(e.ctx, theme.ID, "2.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0"); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if err := e.svc.UpgradeInstallation(e.ctx, e.sellerA, e.storeA, "2.0.0"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	inst, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	v2, err := e.repo.GetThemeVersion(e.ctx, inst.ThemeVersionID)
	if err != nil {
		t.Fatalf("get version: %v", err)
	}
	if v2.Version != "2.0.0" {
		t.Fatalf("expected version 2.0.0 after upgrade, got %s", v2.Version)
	}
}

func TestAuthorizationCrossStore(t *testing.T) {
	e := setupThemesTest(t)
	// Seller A installs on Store A successfully.
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("seller A install store A: %v", err)
	}
	// Seller A must NOT manage Store B.
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeB, DefaultThemeKey, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-store install, got %v", err)
	}
	if _, _, err := e.svc.GetInstallation(e.ctx, e.sellerA, e.storeB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-store get, got %v", err)
	}
	if _, _, err := e.svc.GetDraftConfiguration(e.ctx, e.sellerA, e.storeB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-store draft read, got %v", err)
	}
	if _, err := e.svc.PublishConfiguration(e.ctx, e.sellerA, e.storeB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-store publish, got %v", err)
	}
	if _, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeB); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-store preview, got %v", err)
	}
}

func TestPreviewTokenLifecycle(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Controllable clock for deterministic expiry.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc := NewService(e.repo, commerce.NewRepository(e.pool.Pool), Options{PreviewSecret: []byte("test-secret"), Clock: clock})

	token, err := svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	claims, err := svc.VerifyPreviewToken(token)
	if err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if claims.StoreID != e.storeA {
		t.Fatalf("token store mismatch: %s", claims.StoreID)
	}

	// Expired token.
	now = now.Add(1 * time.Hour) // beyond 15m TTL
	if _, err := svc.VerifyPreviewToken(token); err == nil {
		t.Fatal("expected expired token to fail verification")
	}

	// Tampered signature.
	tampered := token + "x"
	if _, err := svc.VerifyPreviewToken(tampered); err == nil {
		t.Fatal("expected tampered token to fail verification")
	}

	// Wrong secret cannot verify.
	other := NewService(e.repo, commerce.NewRepository(e.pool.Pool), Options{PreviewSecret: []byte("other-secret"), Clock: clock})
	if _, err := other.VerifyPreviewToken(token); err == nil {
		t.Fatal("expected token signed with different secret to fail")
	}
}

// --- Correctness regressions ---

// TestFailedInstallRollsBackDeactivation proves install/switch is atomic. The
// composite FK violation below fires on the installation INSERT, after the
// previous installation has already been deactivated inside the same
// transaction. If the sequence were not transactional the store would be left
// with no active theme at all.
func TestFailedInstallRollsBackDeactivation(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install default: %v", err)
	}
	before, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation before: %v", err)
	}

	// A version belonging to a different theme violates the composite FK.
	other, err := e.repo.CreateTheme(e.ctx, "other-theme", "Other", "", ThemeTypeFree, ThemeStatusActive)
	if err != nil {
		t.Fatalf("create other theme: %v", err)
	}
	otherVersion, err := e.repo.CreateThemeVersion(e.ctx, other.ID, "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0")
	if err != nil {
		t.Fatalf("create other version: %v", err)
	}
	theme, err := e.repo.GetThemeByKey(e.ctx, DefaultThemeKey)
	if err != nil {
		t.Fatalf("get default theme: %v", err)
	}

	if _, _, err := e.repo.InstallAtomically(e.ctx, e.storeA, theme.ID, otherVersion.ID, DefaultConfiguration); err == nil {
		t.Fatal("expected mismatched theme/version install to fail")
	}

	after, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation after failed install: %v", err)
	}
	if after.ID != before.ID {
		t.Fatalf("failed install replaced the active installation: %s -> %s", before.ID, after.ID)
	}
	var activeCount int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM theme_installations WHERE store_id=$1 AND status='active'`, e.storeA).Scan(&activeCount); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active installation after rollback, got %d", activeCount)
	}
}

// TestInstallationThemeVersionIntegrity proves the composite FK rejects an
// installation whose theme_id and theme_version_id disagree.
func TestInstallationThemeVersionIntegrity(t *testing.T) {
	e := setupThemesTest(t)
	themeA, err := e.repo.CreateTheme(e.ctx, "integrity-a", "Integrity A", "", ThemeTypeFree, ThemeStatusActive)
	if err != nil {
		t.Fatalf("create theme A: %v", err)
	}
	themeB, err := e.repo.CreateTheme(e.ctx, "integrity-b", "Integrity B", "", ThemeTypeFree, ThemeStatusActive)
	if err != nil {
		t.Fatalf("create theme B: %v", err)
	}
	versionB, err := e.repo.CreateThemeVersion(e.ctx, themeB.ID, "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0")
	if err != nil {
		t.Fatalf("create version B: %v", err)
	}
	// theme A paired with a version owned by theme B must be rejected.
	if _, err := e.repo.CreateInstallation(e.ctx, e.storeA, themeA.ID, versionB.ID, ThemeInstallationStatusActive); err == nil {
		t.Fatal("expected composite FK violation for theme/version mismatch")
	}
	// The matching pair is accepted.
	versionA, err := e.repo.CreateThemeVersion(e.ctx, themeA.ID, "1.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0")
	if err != nil {
		t.Fatalf("create version A: %v", err)
	}
	if _, err := e.repo.CreateInstallation(e.ctx, e.storeA, themeA.ID, versionA.ID, ThemeInstallationStatusActive); err != nil {
		t.Fatalf("expected matching theme/version to install: %v", err)
	}
}

// TestReinstallPreviouslyUsedVersion covers A -> B -> A: a store must be able to
// return to a theme version it used before. Installations are append-only
// history, so the (store, version) pair is intentionally not unique.
func TestReinstallPreviouslyUsedVersion(t *testing.T) {
	e := setupThemesTest(t)
	theme, err := e.repo.GetThemeByKey(e.ctx, DefaultThemeKey)
	if err != nil {
		t.Fatalf("get default theme: %v", err)
	}
	versionB, err := e.repo.CreateThemeVersion(e.ctx, theme.ID, "2.0.0", ThemeVersionStatusPublished, DefaultConfigurationSchema, DefaultConfiguration, "1.0.0")
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}

	first, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, "")
	if err != nil {
		t.Fatalf("install A: %v", err)
	}
	second, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, "2.0.0")
	if err != nil {
		t.Fatalf("install B: %v", err)
	}
	if second.ThemeVersionID != versionB.ID {
		t.Fatalf("expected B install to use version %s, got %s", versionB.ID, second.ThemeVersionID)
	}
	// Returning to A must succeed even though this store already used it.
	third, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, "")
	if err != nil {
		t.Fatalf("reinstall A: %v", err)
	}
	if third.ThemeVersionID != first.ThemeVersionID {
		t.Fatalf("expected reinstall to reuse version %s, got %s", first.ThemeVersionID, third.ThemeVersionID)
	}
	if third.ID == first.ID {
		t.Fatal("expected reinstall to create a new installation row (history is append-only)")
	}

	var activeCount int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM theme_installations WHERE store_id=$1 AND status='active'`, e.storeA).Scan(&activeCount); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active installation after A->B->A, got %d", activeCount)
	}
	var totalCount int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM theme_installations WHERE store_id=$1`, e.storeA).Scan(&totalCount); err != nil {
		t.Fatalf("count total: %v", err)
	}
	if totalCount != 3 {
		t.Fatalf("expected 3 historical installations, got %d", totalCount)
	}
	// History is preserved: the superseded A and B rows are inactive, not deleted.
	for _, id := range []string{first.ID, second.ID} {
		var status string
		if err := e.pool.QueryRow(e.ctx, `SELECT status FROM theme_installations WHERE id=$1`, id).Scan(&status); err != nil {
			t.Fatalf("read historical installation %s: %v", id, err)
		}
		if status != ThemeInstallationStatusInactive {
			t.Fatalf("expected historical installation %s to be inactive, got %s", id, status)
		}
	}
}

// TestPublishRejectsStaleDraftRevision closes the TOCTOU window: a draft edited
// after validation must not be published under the stale revision.
func TestPublishRejectsStaleDraftRevision(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	inst, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	cfg, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	// Simulate a concurrent draft edit between validation and publish.
	if _, err := e.repo.UpdateDraftConfiguration(e.ctx, inst.ID, map[string]any{"hero": map[string]any{"title": "Concurrent"}}); err != nil {
		t.Fatalf("concurrent draft edit: %v", err)
	}
	if _, err := e.repo.PublishConfiguration(e.ctx, inst.ID, cfg.DraftRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for stale draft revision, got %v", err)
	}
	after, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config after: %v", err)
	}
	if after.PublishedRevision != cfg.PublishedRevision {
		t.Fatalf("published revision must not advance on stale publish: %d -> %d", cfg.PublishedRevision, after.PublishedRevision)
	}
}

// TestConcurrentPublishBumpsRevisionOnce ensures N concurrent publishes advance
// the published revision exactly N times (no lost updates under the row lock).
func TestConcurrentPublishBumpsRevisionOnce(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	inst, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	cfg, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.repo.PublishConfiguration(e.ctx, inst.ID, cfg.DraftRevision); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent publish failed: %v", err)
	}

	after, err := e.repo.GetConfiguration(e.ctx, inst.ID)
	if err != nil {
		t.Fatalf("get config after: %v", err)
	}
	if after.PublishedRevision != workers {
		t.Fatalf("expected published revision %d after %d concurrent publishes, got %d", workers, workers, after.PublishedRevision)
	}
}

// TestPreviewTokenRequiresConfiguredSecret proves preview fails closed instead of
// issuing or accepting tokens signed with an empty key.
func TestPreviewTokenRequiresConfiguredSecret(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	unconfigured := NewService(e.repo, commerce.NewRepository(e.pool.Pool), Options{})
	if _, err := unconfigured.CreatePreviewToken(e.ctx, e.sellerA, e.storeA); !errors.Is(err, ErrPreviewNotConfigured) {
		t.Fatalf("expected ErrPreviewNotConfigured, got %v", err)
	}
	// A token minted with a real secret must not verify when the secret is unset.
	token, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := unconfigured.VerifyPreviewToken(token); !errors.Is(err, ErrPreviewNotConfigured) {
		t.Fatalf("expected ErrPreviewNotConfigured on verify, got %v", err)
	}
}

func TestResolveStorefrontPreviewReturnsCurrentDraftTheme(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := e.svc.UpdateDraftConfiguration(e.ctx, e.sellerA, e.storeA, defaultConfigWithHeroTitle(t, "Draft")); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	token, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	preview, err := e.svc.ResolveStorefrontPreview(e.ctx, token, e.storeA)
	if err != nil {
		t.Fatalf("resolve preview: %v", err)
	}
	if preview.Key != DefaultThemeKey || preview.Version != DefaultThemeVersion {
		t.Fatalf("theme identity = %+v", preview)
	}
	hero, _ := preview.Configuration["hero"].(map[string]any)
	if hero["title"] != "Draft" {
		t.Fatalf("hero title = %q, want Draft", hero["title"])
	}
	if preview.DraftRevision != 1 {
		t.Fatalf("draft revision = %d, want 1", preview.DraftRevision)
	}
}

func TestResolveStorefrontPreviewFailsClosedForInvalidTokens(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install store A: %v", err)
	}
	if _, err := e.svc.Install(e.ctx, e.sellerB, e.storeB, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install store B: %v", err)
	}
	instA, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation A: %v", err)
	}
	cfgA, err := e.repo.GetConfiguration(e.ctx, instA.ID)
	if err != nil {
		t.Fatalf("get config A: %v", err)
	}

	cases := []struct {
		name  string
		token string
		store string
	}{
		{"malformed token", "not-a-token", e.storeA},
		{"tampered signature", mustPreviewToken(t, e.svc, e.storeA, instA.ID, cfgA.DraftRevision) + "x", e.storeA},
		{"wrong store", mustPreviewToken(t, e.svc, e.storeA, instA.ID, cfgA.DraftRevision), e.storeB},
		{"wrong installation", mustPreviewToken(t, e.svc, e.storeA, "00000000-0000-0000-0000-000000000000", cfgA.DraftRevision), e.storeA},
		{"stale draft revision", mustPreviewToken(t, e.svc, e.storeA, instA.ID, cfgA.DraftRevision+1), e.storeA},
		{"empty token", "", e.storeA},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e.svc.ResolveStorefrontPreview(e.ctx, tc.token, tc.store); !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestResolveStorefrontPreviewRejectsExpiredToken(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := NewService(e.repo, commerce.NewRepository(e.pool.Pool), Options{
		PreviewSecret: []byte("test-secret"),
		Clock:         func() time.Time { return now },
	})
	token, err := svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	now = now.Add(16 * time.Minute)
	if _, err := svc.ResolveStorefrontPreview(e.ctx, token, e.storeA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired token, got %v", err)
	}
}

func TestResolveStorefrontPreviewRequiresConfiguredSecret(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	token, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	unconfigured := NewService(e.repo, commerce.NewRepository(e.pool.Pool), Options{})
	if _, err := unconfigured.ResolveStorefrontPreview(e.ctx, token, e.storeA); !errors.Is(err, ErrPreviewNotConfigured) {
		t.Fatalf("expected ErrPreviewNotConfigured, got %v", err)
	}
}

func TestResolveStorefrontPreviewRequiresCurrentInstallationAndRevision(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	token, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := e.svc.UpdateDraftConfiguration(e.ctx, e.sellerA, e.storeA, defaultConfigWithHeroTitle(t, "Next draft")); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, token, e.storeA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected stale token to fail, got %v", err)
	}

	fresh, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue fresh token: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, fresh, e.storeA); err != nil {
		t.Fatalf("fresh token should resolve: %v", err)
	}

	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("switch theme: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, fresh, e.storeA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected switched installation token to fail, got %v", err)
	}
	switched, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue switched token: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, switched, e.storeA); err != nil {
		t.Fatalf("switched token should resolve: %v", err)
	}
}

func TestResolveStorefrontPreviewFailsWhenCurrentInstallationIsMissing(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	token, err := e.svc.CreatePreviewToken(e.ctx, e.sellerA, e.storeA)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `UPDATE theme_installations SET status='inactive' WHERE store_id=$1`, e.storeA); err != nil {
		t.Fatalf("remove active installation: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, token, e.storeA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveStorefrontPreviewRejectsInconsistentStoredDraft(t *testing.T) {
	e := setupThemesTest(t)
	if _, err := e.svc.Install(e.ctx, e.sellerA, e.storeA, DefaultThemeKey, ""); err != nil {
		t.Fatalf("install: %v", err)
	}
	inst, err := e.repo.GetInstallationByStore(e.ctx, e.storeA)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	unsafe := defaultConfigWithHeroTitle(t, "<script>alert(1)</script>")
	token := mustPreviewToken(t, e.svc, e.storeA, inst.ID, 1)
	_, err = e.pool.Exec(e.ctx, `UPDATE theme_configurations SET draft_config=$2, draft_revision=1 WHERE installation_id=$1`, inst.ID, unsafe)
	if err != nil {
		t.Fatalf("write inconsistent draft: %v", err)
	}
	if _, err := e.svc.ResolveStorefrontPreview(e.ctx, token, e.storeA); !errors.Is(err, ErrUnsafeContent) {
		t.Fatalf("expected ErrUnsafeContent, got %v", err)
	}
}

func mustPreviewToken(t *testing.T, svc Service, storeID, installationID string, draftRevision int) string {
	t.Helper()
	token, err := svc.IssuePreviewToken(storeID, installationID, draftRevision)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("issued empty preview token")
	}
	return token
}

func TestThemeMigrationUpDownReapply(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	applyMigration(t, db, "000002_market_reference_data.up.sql")
	applyMigration(t, db, "000003_commerce_domain_schema.up.sql")
	applyMigration(t, db, "000007_theme_engine_schema.up.sql")
	// down
	downBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000007_theme_engine_schema.down.sql"))
	if err != nil {
		t.Fatalf("read down migration: %v", err)
	}
	if _, err := db.Exec(context.Background(), string(downBytes)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	// up again (idempotent reapply)
	applyMigration(t, db, "000007_theme_engine_schema.up.sql")
}
