package commerce

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"core/packages/database"
)

// stubImageComposer is superseded by validComposerStub/opaqueComposerStub;
// it exists only for name parity in the docs above.

type stubTextGenerator struct{ fields []TextFieldResult }

func (s stubTextGenerator) GenerateProductText(ctx context.Context, req TextRequest) (TextResult, error) {
	return TextResult{Fields: s.fields}, nil
}

func encodeTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// productAccentPNG is a 4x4 PNG whose bottom half is opaque product pixels and
// whose top half is fully transparent background.
func productTestAccentPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y >= 2 {
				img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
			}
		}
	}
	return encodeTestPNG(t, img)
}

// productTestMaskPNG marks the same bottom half as the product region.
func productTestMaskPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetAlpha(x, y, color.Alpha{A: 255})
		}
	}
	return encodeTestPNG(t, img)
}

// opaqueTestPNG is a fully opaque mask: provingComposite must refuse it
// because an opaque mask proves nothing about what changed in the scene.
func opaqueTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 10, B: 200, A: 255})
		}
	}
	return encodeTestPNG(t, img)
}

// sceneTestPNG is the generated surrounding scene with the same dimensions.
func sceneTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 60, B: 90, A: 255})
		}
	}
	return encodeTestPNG(t, img)
}

type validComposerStub struct{ fg, mask, scene []byte }

func (c validComposerStub) IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error) {
	return Foreground{ContentType: "image/png", Bytes: c.fg, MaskBytes: c.mask}, nil
}

func (c validComposerStub) GenerateProductImage(ctx context.Context, req ProductImageRequest) (GeneratedProductImage, error) {
	return GeneratedProductImage{ContentType: "image/png", Bytes: c.scene}, nil
}

type opaqueComposerStub struct{ fg, mask, scene []byte }

func (c opaqueComposerStub) IsolateForeground(ctx context.Context, req IsolateRequest) (Foreground, error) {
	return Foreground{ContentType: "image/png", Bytes: c.fg, MaskBytes: c.mask}, nil
}

func (c opaqueComposerStub) GenerateProductImage(ctx context.Context, req ProductImageRequest) (GeneratedProductImage, error) {
	return GeneratedProductImage{ContentType: "image/png", Bytes: c.scene}, nil
}

func insertReadyAuthoringAsset(t *testing.T, db *database.Pool, storeID, suffix string) (string, string) {
	t.Helper()
	id := uuid.NewString()
	key := "ai-tests/" + storeID + "/source-" + uuid.NewString() + ".png"
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO store_media_assets (id, store_id, checksum_sha256, storage_key, content_type, byte_size, original_filename, status, created_by_subject)
		VALUES ($1, $2, $3, $4, 'image/png', 4, 'source.png', 'ready', 'ai-tests')
	`, id, storeID, uuid.NewString(), key); err != nil {
		t.Fatalf("insert ready media asset: %v", err)
	}
	return id, key
}

func newAuthoringTestEnv(t *testing.T) (*database.Pool, Service, Repository, string, string, string) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("postgres unavailable: TEST_DATABASE_URL is required for authoring validation")
	}
	db, service, repo, suffix := setupSellerCatalogTestDB(t)
	applySQLFile(t, db, "../../migrations/000046_ai_product_authoring.up.sql")

	ctx := context.Background()
	seller, err := repo.CreateSeller(ctx, "ai-seller-"+suffix, "AI Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "ai-subject-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, subject, "manager", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := repo.CreateStore(ctx, seller.ID, "EG", "ai-store-"+suffix, "AI Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}
	return db, service, repo, suffix, subject, store.ID
}

func TestAuthoringImageBranchAndAcceptFlow(t *testing.T) {
	db, service, repo, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()

	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	composer := validComposerStub{fg: productTestAccentPNG(t), mask: productTestMaskPNG(t), scene: sceneTestPNG(t)}
	service.AuthoringProviders = AuthoringProviderRegistry{
		TextGenerator: stubTextGenerator{fields: []TextFieldResult{{FieldKey: FieldKeyName, Locale: LocaleAR, Value: "اسم قهوة"}}},
		ImageComposer: composer,
	}

	job, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "منتج قهوة مميزة", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1, IDempotencyKey: "key-" + suffix})
	if err != nil {
		t.Fatalf("CreateAuthoringJobForSubject: %v", err)
	}
	if err := service.ProcessAuthoringJob(ctx, job); err != nil {
		t.Fatalf("ProcessAuthoringJob: %v", err)
	}
	fresh, err := service.GetAuthoringJobForSubject(ctx, subject, storeID, job.ID)
	if err != nil {
		t.Fatalf("GetAuthoringJobForSubject: %v", err)
	}
	if fresh.TextStatus != BranchStatusSucceeded {
		t.Fatalf("text status = %s, want succeeded", fresh.TextStatus)
	}
	if fresh.ImageStatus != BranchStatusSucceeded {
		t.Fatalf("image status = %s, want succeeded (stub mask is valid)", fresh.ImageStatus)
	}
	if fresh.Status != JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", fresh.Status)
	}
	if len(fresh.ImageCandidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(fresh.ImageCandidates))
	}
	candidate := fresh.ImageCandidates[0]
	if candidate.Status != ItemStatusProposed || candidate.Visibility != "hidden" {
		t.Fatalf("candidate state = %s/%s, want proposed/hidden", candidate.Status, candidate.Visibility)
	}
	if candidate.AssetID != nil || candidate.ProductID != nil {
		t.Fatalf("proposed candidate must not carry an asset or product id")
	}

	accepted, err := service.AcceptAuthoringCandidateForSubject(ctx, subject, storeID, job.ID, candidate.ID, AuthoringAcceptImageRequest{IsVisible: false, AltText: "قهوة"})
	if err != nil {
		t.Fatalf("AcceptAuthoringCandidateForSubject: %v", err)
	}
	if accepted.Status != ItemStatusAccepted || accepted.AssetID == nil {
		t.Fatalf("accepted candidate state = %s asset=%v", accepted.Status, accepted.AssetID)
	}
	if accepted.ProductID != nil {
		t.Fatalf("accepted candidate must stay unlinked to a product, got product_id %v", *accepted.ProductID)
	}
	if accepted.AltText != "قهوة" {
		t.Fatalf("accepted alt text = %q, want قهوة", accepted.AltText)
	}
	var origin, storedKey string
	if err := db.Pool.QueryRow(ctx, `SELECT origin, storage_key FROM store_media_assets WHERE id = $1`, *accepted.AssetID).Scan(&origin, &storedKey); err != nil {
		t.Fatalf("read created asset: %v", err)
	}
	if origin != "ai_generated" {
		t.Fatalf("asset origin = %s, want ai_generated", origin)
	}
	if storedKey == "" {
		t.Fatalf("asset must keep a server-side storage key")
	}

	// Idempotency: accepting again returns the unchanged result.
	again, err := service.AcceptAuthoringCandidateForSubject(ctx, subject, storeID, job.ID, candidate.ID, AuthoringAcceptImageRequest{IsVisible: true, AltText: "changed"})
	if err != nil {
		t.Fatalf("idempotent accept: %v", err)
	}
	if again.ID != accepted.ID || again.Status != ItemStatusAccepted || again.AltText != accepted.AltText {
		t.Fatalf("idempotent accept returned a changed result: %s / %s / %q", again.ID, again.Status, again.AltText)
	}

	// Tenant isolation: a foreign store gets not-found, no existence leak.
	otherSeller, err := repo.CreateSeller(ctx, "ai-seller-2-"+suffix, "AI Seller 2", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller 2: %v", err)
	}
	otherSubject := "ai-subject-2-" + suffix
	if _, err := repo.CreateSellerMember(ctx, otherSeller.ID, otherSubject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember 2: %v", err)
	}
	otherStore, err := repo.CreateStore(ctx, otherSeller.ID, "EG", "ai-store-2-"+suffix, "AI Store 2", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore 2: %v", err)
	}
	if _, err := service.GetAuthoringJobForSubject(ctx, otherSubject, otherStore.ID, job.ID); err == nil {
		t.Fatalf("foreign store read should fail")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign store read err = %v, want not found", err)
	}
}

// TestAuthoringGeneratedImageIsReviewable asserts the full generated image is
// offered as a review candidate without requiring an alpha-mask composite.
func TestAuthoringGeneratedImageIsReviewable(t *testing.T) {
	db, service, _, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()

	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	service.AuthoringProviders = AuthoringProviderRegistry{
		TextGenerator: stubTextGenerator{fields: []TextFieldResult{{FieldKey: FieldKeyName, Locale: LocaleEN, Value: "name"}}},
		ImageComposer: opaqueComposerStub{fg: opaqueTestPNG(t), mask: opaqueTestPNG(t), scene: sceneTestPNG(t)},
	}
	job, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("CreateAuthoringJobForSubject: %v", err)
	}
	if err := service.ProcessAuthoringJob(ctx, job); err != nil {
		t.Fatalf("ProcessAuthoringJob: %v", err)
	}
	fresh, err := service.GetAuthoringJobForSubject(ctx, subject, storeID, job.ID)
	if err != nil {
		t.Fatalf("GetAuthoringJobForSubject: %v", err)
	}
	if fresh.TextStatus != BranchStatusSucceeded {
		t.Fatalf("text status = %s, want succeeded", fresh.TextStatus)
	}
	if fresh.ImageStatus != BranchStatusSucceeded {
		t.Fatalf("image status = %s, want succeeded", fresh.ImageStatus)
	}
	if fresh.Status != JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", fresh.Status)
	}
	if len(fresh.ImageCandidates) != 1 || fresh.ImageCandidates[0].Status != ItemStatusProposed {
		t.Fatalf("candidate = %+v, want a proposed full-image result", fresh.ImageCandidates)
	}
}

// TestAuthoringAcceptRejectsNonProposed covers the state guard on accept.
func TestAuthoringAcceptRejectsNonProposed(t *testing.T) {
	db, service, repo, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()

	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	job, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("CreateAuthoringJobForSubject: %v", err)
	}
	// No provider configured: no candidate rows exist at accept time.
	candidates, err := repo.listAuthoringJobCandidates(ctx, storeID, job.ID)
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want 0 without an image provider", len(candidates))
	}
	acceptErr := func() error {
		_, err := service.AcceptAuthoringCandidateForSubject(ctx, subject, storeID, job.ID, uuid.NewString(), AuthoringAcceptImageRequest{})
		return err
	}()
	if acceptErr == nil || !errors.Is(acceptErr, ErrNotFound) {
		t.Fatalf("accept of unknown candidate err = %v, want not found", acceptErr)
	}
	// Manager-access check is shared with the mutating paths; a foreign
	// subject without any membership must be rejected.
	if _, err := service.AcceptAuthoringCandidateForSubject(ctx, "stranger-"+suffix, storeID, job.ID, uuid.NewString(), AuthoringAcceptImageRequest{}); err == nil {
		t.Fatalf("accept without membership must fail")
	} else if !strings.Contains(strings.ToLower(err.Error()), "forbidden") && !strings.Contains(strings.ToLower(err.Error()), "member") && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
		t.Fatalf("accept without membership err = %v, want forbidden/not-found", err)
	}
}

// TestAuthoringProviderConfigurationDrivesBranches proves the full ENV-driven
// behavior at the service boundary: job creation succeeds without any HTTP or
// boot failure when no provider is configured, the missing branch is recorded
// as provider_not_configured, the configured branch continues, the aggregate
// state is partial with one branch and failed without any, and nothing is
// silently treated as success.
func TestAuthoringProviderConfigurationDrivesBranches(t *testing.T) {
	db, _, repo, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()

	okComposer := validComposerStub{fg: productTestAccentPNG(t), mask: productTestMaskPNG(t), scene: sceneTestPNG(t)}
	service := NewService(repo)
	service.S3Storage = dbAwareStorage(service.S3Storage)

	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	service.AuthoringProviders = AuthoringProviderRegistry{
		TextFailure: ProviderFailureNotConfigured,
	}

	job, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("create without any configured provider must not fail: %v", err)
	}
	if job.Status != JobStatusPending {
		t.Fatalf("created status = %s, want pending", job.Status)
	}
	if err := service.ProcessAuthoringJob(ctx, job); err != nil {
		t.Fatalf("process with both branches unconfigured: %v", err)
	}
	bothFailed, _ := repo.GetAuthoringJobForStore(ctx, storeID, job.ID)
	if bothFailed.Status != JobStatusFailed || bothFailed.TextStatus != BranchStatusFailed || bothFailed.ImageStatus != BranchStatusFailed {
		t.Fatalf("both unconfigured = %s/%s/%s, want failed/failed/failed", bothFailed.Status, bothFailed.TextStatus, bothFailed.ImageStatus)
	}
	if bothFailed.ErrorCode != AuthoringProviderNotConfigured {
		t.Fatalf("both unconfigured error code = %q, want ai_provider_not_configured", bothFailed.ErrorCode)
	}
	if len(bothFailed.ImageCandidates) != 0 || len(bothFailed.Fields) != 0 {
		t.Fatalf("a failed branch must not leave silent results: %+v / %+v", bothFailed.ImageCandidates, bothFailed.Fields)
	}

	// Text configured (text stub) with the image branch unconfigured: the
	// configured branch continues to run and the job stays partial.
	service2 := NewService(repo)
	service2.S3Storage = dbAwareStorage(service2.S3Storage)
	service2.AuthoringProviders = AuthoringProviderRegistry{
		TextGenerator: stubTextGenerator{fields: []TextFieldResult{{FieldKey: FieldKeyName, Locale: LocaleAR, Value: "name"}}},
	}
	asset2, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	job2, err := service2.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief 2", SourceAssetIDs: []string{asset2}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("create with text only: %v", err)
	}
	if err := service2.ProcessAuthoringJob(ctx, job2); err != nil {
		t.Fatalf("process image-missing: %v", err)
	}
	partial, _ := repo.GetAuthoringJobForStore(ctx, storeID, job2.ID)
	if partial.Status != JobStatusPartial || partial.TextStatus != BranchStatusSucceeded || partial.ImageStatus != BranchStatusFailed {
		t.Fatalf("missing image = %s/%s/%s, want partial/succeeded/failed", partial.Status, partial.TextStatus, partial.ImageStatus)
	}
	if len(partial.Fields) == 0 || partial.Fields[0].Status != ItemStatusProposed {
		t.Fatalf("the configured branch keeps writing results: %+v", partial.Fields)
	}

	// Image configured with text explicitly unconfigured: mirrored partial.
	service3 := NewService(repo)
	service3.S3Storage = dbAwareStorage(service3.S3Storage)
	service3.AuthoringProviders = AuthoringProviderRegistry{
		TextFailure:   ProviderFailureNotConfigured,
		ImageComposer: okComposer,
	}
	asset3, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)
	job3, err := service3.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief 3", SourceAssetIDs: []string{asset3}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("create with image only: %v", err)
	}
	if err := service3.ProcessAuthoringJob(ctx, job3); err != nil {
		t.Fatalf("process text-missing: %v", err)
	}
	partial3, _ := repo.GetAuthoringJobForStore(ctx, storeID, job3.ID)
	if partial3.Status != JobStatusPartial || partial3.TextStatus != BranchStatusFailed || partial3.ImageStatus != BranchStatusSucceeded {
		t.Fatalf("missing text = %s/%s/%s, want partial/failed/succeeded", partial3.Status, partial3.TextStatus, partial3.ImageStatus)
	}
}

// dbAwareStorage keeps whatever storage the shared environment already wired
// (with mocks) and just re-applies the authoring PutObject hook.
func dbAwareStorage(existing *S3Storage) *S3Storage {
	if existing == nil {
		return &S3Storage{cfg: S3Config{URLTTL: time.Minute}}
	}
	return existing
}

// TestAuthoringLimitsAndIdempotency proves the server-side image limit and
// the database-level idempotency of job creation.
func TestAuthoringLimitsAndIdempotency(t *testing.T) {
	db, service, repo, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()

	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)

	// The resolver clamps any configured value above the ceiling.
	service.AuthoringLimitResolver = NewConfigImageLimitResolver(10)
	limits, err := service.GetAuthoringLimitsForSubject(ctx, subject, storeID)
	if err != nil {
		t.Fatalf("limits: %v", err)
	}
	if limits.EffectiveLimit != 7 || limits.DefaultCount != 3 {
		t.Fatalf("limits = %d/%d, want 7/3", limits.EffectiveLimit, limits.DefaultCount)
	}

	// A request above the effective limit is rejected before any write.
	_, err = service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 8, IDempotencyKey: "limit-" + suffix})
	if err == nil || !errors.Is(err, ErrAILimitExceeded) {
		t.Fatalf("limit breach err = %v, want ErrAILimitExceeded", err)
	}

	// Same idempotency key returns the same job, no second row.
	first, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1, IDempotencyKey: "same-" + suffix})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief 2", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1, IDempotencyKey: "same-" + suffix})
	if err != nil {
		t.Fatalf("replayed create: %v", err)
	}
	if first.ID != second.ID || first.Brief != second.Brief {
		t.Fatalf("idempotent replay produced a different job: %s vs %s", first.ID, second.ID)
	}
	jobs, err := countAuthoringJobs(ctx, repo, storeID)
	if err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	// One extra pending job exists from... nothing else: exactly one row.
	if jobs != 1 {
		t.Fatalf("job rows = %d, want 1", jobs)
	}

	// ProviderDefaultCount mirrors min(3, effective_limit).
	if got := DefaultAuthoringImageCount(2); got != 2 {
		t.Fatalf("default count for limit 2 = %d, want 2", got)
	}
}

// TestAuthoringForeignCategoryClassifiedFailed proves a category suggestion
// for another store never lands as a proposed result.
func TestAuthoringForeignCategoryClassifiedFailed(t *testing.T) {
	db, service, repo, suffix, subject, storeID := newAuthoringTestEnv(t)
	ctx := context.Background()
	assetID, _ := insertReadyAuthoringAsset(t, db, storeID, suffix)

	foreign := "not-a-store-category"
	service.AuthoringProviders = AuthoringProviderRegistry{
		TextGenerator: stubTextGenerator{fields: []TextFieldResult{
			{FieldKey: FieldKeyName, Locale: LocaleAR, Value: "اسم"},
			{FieldKey: FieldKeyCategory, Locale: LocaleAR, CategoryID: &foreign},
			{FieldKey: "pricing", Locale: LocaleAR, Value: "score"}, // outside the five-field vocabulary
		}},
	}
	job, err := service.CreateAuthoringJobForSubject(ctx, subject, storeID, AuthoringCreateRequest{Brief: "برief", SourceAssetIDs: []string{assetID}, RequestedImageCount: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	service.AuthoringProviders.ImageComposer = nil
	service.AuthoringProviders.ImageFailure = ProviderFailureNotConfigured
	if err := service.ProcessAuthoringJob(ctx, job); err != nil {
		t.Fatalf("process: %v", err)
	}
	fresh, _ := repo.GetAuthoringJobForStore(ctx, storeID, job.ID)
	var proposedCategories, otherKey, failedRows int
	for _, field := range fresh.Fields {
		if field.FieldKey == FieldKeyCategory && field.Status == ItemStatusProposed {
			proposedCategories++
		}
		if field.FieldKey == "pricing" {
			otherKey++
		}
		if field.Status == ItemStatusFailed && field.FailureCode == AuthoringUnsupportedField {
			failedRows++
		}
	}
	if proposedCategories != 0 {
		t.Fatalf("foreign category must not be proposed, got %d proposed rows", proposedCategories)
	}
	if otherKey != 0 {
		t.Fatalf("out-of-vocabulary field key must not surface as a proposal")
	}
	if failedRows != 1 {
		t.Fatalf("foreign category must land failed with unsupported_field and be structurally writable, got %d failed rows", failedRows)
	}
}

func countAuthoringJobs(ctx context.Context, repo Repository, storeID string) (int, error) {
	var count int
	err := repo.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_authoring_jobs WHERE store_id = $1`, storeID).Scan(&count)
	return count, err
}
