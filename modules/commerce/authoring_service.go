package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AuthoringCreateRequest struct {
	Brief               string   `json:"brief"`
	SourceAssetIDs      []string `json:"source_asset_ids"`
	RequestedImageCount int      `json:"requested_image_count"`
	IDempotencyKey      string   `json:"-"`
}

type AuthoringLimits struct {
	EffectiveLimit int `json:"effective_limit"`
	DefaultCount   int `json:"default_count"`
}

type AuthoringFieldDecisionRequest struct {
	Decision string `json:"decision"`
	Value    string `json:"value"`
}

type AuthoringAcceptImageRequest struct {
	IsVisible bool   `json:"is_visible"`
	AltText   string `json:"alt_text"`
}

type AuthoringRetryRequest struct {
	Parts        []string `json:"parts"`
	CandidateIDs []string `json:"candidate_ids"`
}

func (s Service) GetAuthoringLimitsForSubject(ctx context.Context, subject, storeID string) (AuthoringLimits, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringLimits{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringLimits{}, err
	}
	resolver := s.AuthoringLimitResolver
	if resolver == nil {
		resolver = NewConfigImageLimitResolver(7)
	}
	limit, err := resolver.EffectiveImageLimit(ctx, storeID)
	if err != nil {
		return AuthoringLimits{}, err
	}
	if limit > MaxAuthoringImages {
		limit = MaxAuthoringImages
	}
	if limit < 1 {
		limit = 1
	}
	return AuthoringLimits{EffectiveLimit: limit, DefaultCount: DefaultAuthoringImageCount(limit)}, nil
}

func (s Service) CreateAuthoringJobForSubject(ctx context.Context, subject, storeID string, req AuthoringCreateRequest) (AuthoringJob, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringJob{}, err
	}
	if _, err := s.RequireSellerManagerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringJob{}, err
	}
	if strings.TrimSpace(req.Brief) == "" && len(req.SourceAssetIDs) == 0 {
		return AuthoringJob{}, ErrInvalidInput
	}
	limits, err := s.GetAuthoringLimitsForSubject(ctx, subject, storeID)
	if err != nil {
		return AuthoringJob{}, err
	}
	if req.RequestedImageCount < 1 {
		return AuthoringJob{}, ErrInvalidInput
	}
	if req.RequestedImageCount > limits.EffectiveLimit {
		return AuthoringJob{}, fmt.Errorf("%w: effective limit is %d", ErrAILimitExceeded, limits.EffectiveLimit)
	}
	for _, assetID := range req.SourceAssetIDs {
		if err := s.validateAuthoringSourceAsset(ctx, storeID, assetID); err != nil {
			return AuthoringJob{}, err
		}
	}
	return s.repo.CreateAuthoringJob(ctx, CreateAuthoringJobRecord{StoreID: storeID, CreatedBySubject: subject, Brief: req.Brief, RequestedImageCount: req.RequestedImageCount, EffectiveImageLimit: limits.EffectiveLimit, IDempotencyKey: req.IDempotencyKey, SourceAssetIDs: req.SourceAssetIDs})
}

func (s Service) GetAuthoringJobForSubject(ctx context.Context, subject, storeID, jobID string) (AuthoringJob, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringJob{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringJob{}, err
	}
	job, err := s.repo.GetAuthoringJobForStore(ctx, storeID, jobID)
	if err != nil {
		return AuthoringJob{}, err
	}
	return s.decorateAuthoringJobURLs(job), nil
}

func (s Service) UpdateAuthoringFieldForSubject(ctx context.Context, subject, storeID, jobID, fieldID string, req AuthoringFieldDecisionRequest) (AuthoringFieldProposal, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringFieldProposal{}, err
	}
	if _, err := s.RequireSellerManagerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringFieldProposal{}, err
	}
	if _, err := s.repo.GetAuthoringJobForStore(ctx, storeID, jobID); err != nil {
		return AuthoringFieldProposal{}, err
	}
	return s.repo.UpdateAuthoringFieldDecision(ctx, storeID, jobID, fieldID, subject, req.Decision, req.Value)
}

func (s Service) RetryAuthoringJobForSubject(ctx context.Context, subject, storeID, jobID string, req AuthoringRetryRequest) (AuthoringJob, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringJob{}, err
	}
	if _, err := s.RequireSellerManagerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringJob{}, err
	}
	job, err := s.repo.GetAuthoringJobForStore(ctx, storeID, jobID)
	if err != nil {
		return AuthoringJob{}, err
	}
	if job.Status == JobStatusPending || job.Status == JobStatusProcessing {
		return AuthoringJob{}, ErrAIJobConflict
	}
	maxRetries := s.AuthoringMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if job.Attempts >= maxRetries {
		return AuthoringJob{}, ErrAIJobNotRetryable
	}
	if len(req.Parts) == 0 && len(req.CandidateIDs) == 0 {
		return AuthoringJob{}, ErrInvalidInput
	}
	_ = s.repo.UpdateAuthoringJobStatuses(ctx, jobID, JobStatusPending, job.TextStatus, job.ImageStatus, "", "")
	return s.repo.GetAuthoringJobForStore(ctx, storeID, jobID)
}

func (s Service) ClaimPendingAuthoringJobs(ctx context.Context, batch int) ([]AuthoringJob, error) {
	return s.repo.ClaimPendingAuthoringJobs(ctx, batch)
}

func (s Service) ProcessAuthoringJob(ctx context.Context, job AuthoringJob) error {
	textStatus, textCode := s.runTextBranch(ctx, job)
	imageStatus, imageCode := s.runImageBranch(ctx, job)
	status := DeriveAuthoringJobStatus(textStatus, imageStatus)
	errorCode := ""
	if status == JobStatusFailed {
		errorCode = AuthoringProviderNotConfigured
		if textCode == ProviderFailureUnavailable || imageCode == ProviderFailureUnavailable {
			errorCode = ProviderFailureUnavailable
		}
	}
	return s.repo.UpdateAuthoringJobStatuses(ctx, job.ID, status, textStatus, imageStatus, errorCode, "")
}

func (s Service) runTextBranch(ctx context.Context, job AuthoringJob) (BranchStatus, string) {
	if s.AuthoringProviders.TextFailure != "" || s.AuthoringProviders.TextGenerator == nil {
		code := s.AuthoringProviders.TextFailure
		if code == "" {
			code = ProviderFailureNotConfigured
		}
		return BranchStatusFailed, code
	}
	res, err := s.AuthoringProviders.TextGenerator.GenerateProductText(ctx, TextRequest{Brief: job.Brief})
	if err != nil {
		return BranchStatusFailed, failureCode(err)
	}
	for _, field := range res.Fields {
		if !IsValidFieldKey(field.FieldKey) {
			_ = s.repo.InsertAuthoringFieldProposal(ctx, job.ID, job.StoreID, field, ItemStatusFailed, AuthoringUnsupportedField)
			continue
		}
		// Third-party references must belong to the same store: a category
		// suggested for another store is never written as a proposal; it
		// lands as a failed row with the unsupported_field code.
		if field.CategoryID != nil && *field.CategoryID != "" {
			belongs, err := s.categoryBelongsToStore(ctx, job.StoreID, *field.CategoryID)
			if err != nil || !belongs {
				// Drop the out-of-store reference before writing: the failed
				// classification row must stay structurally valid (no foreign
				// category id breaches the composite foreign key).
				failed := field
				failed.CategoryID = nil
				_ = s.repo.InsertAuthoringFieldProposal(ctx, job.ID, job.StoreID, failed, ItemStatusFailed, AuthoringUnsupportedField)
				continue
			}
		}
		_ = s.repo.InsertAuthoringFieldProposal(ctx, job.ID, job.StoreID, field, ItemStatusProposed, "")
	}
	return BranchStatusSucceeded, ""
}

// categoryBelongsToStore reports whether the suggested category is scoped to
// the job's store, using the composite (id, store_id) link.
func (s Service) categoryBelongsToStore(ctx context.Context, storeID, categoryID string) (bool, error) {
	if storeID == "" || categoryID == "" {
		return false, nil
	}
	var exists bool
	err := s.repo.Pool().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM store_categories WHERE store_id = $1 AND id = $2)`, storeID, categoryID).Scan(&exists)
	if err != nil {
		return false, translatePGError(err, "check ai authoring category scope")
	}
	return exists, nil
}

func (s Service) runImageBranch(ctx context.Context, job AuthoringJob) (BranchStatus, string) {
	if s.AuthoringProviders.ImageFailure != "" || s.AuthoringProviders.ImageComposer == nil {
		code := s.AuthoringProviders.ImageFailure
		if code == "" {
			code = ProviderFailureNotConfigured
		}
		return BranchStatusFailed, code
	}
	existing, err := s.repo.listAuthoringJobCandidates(ctx, job.StoreID, job.ID)
	if err != nil {
		return BranchStatusFailed, ProviderFailureUnavailable
	}
	alive := 0
	for _, candidate := range existing {
		if candidate.Status != ItemStatusFailed {
			alive++
		}
	}
	if alive >= job.RequestedImageCount {
		return BranchStatusSucceeded, ""
	}
	sourceAssets, err := s.repo.listAuthoringSourceAssets(ctx, job.StoreID, job.ID)
	if err != nil {
		return BranchStatusFailed, ProviderFailureUnavailable
	}
	assetByID := make(map[string]authoringSourceAssetRow, len(sourceAssets))
	sourceIDs := make([]string, 0, len(sourceAssets))
	for _, asset := range sourceAssets {
		assetByID[asset.AssetID] = asset
		sourceIDs = append(sourceIDs, asset.AssetID)
	}
	plannedSources := planAuthoringImageSources(job.RequestedImageCount, sourceIDs, existing)
	if len(plannedSources) == 0 {
		return BranchStatusFailed, ProviderFailureInvalidOutput
	}
	lastCode := ""
	for i, sourceID := range plannedSources {
		asset := assetByID[sourceID]
		code := s.produceAuthoringCandidate(ctx, job, asset, len(existing)+i+1)
		if code != "" {
			lastCode = code
		}
	}
	refreshed, err := s.repo.listAuthoringJobCandidates(ctx, job.StoreID, job.ID)
	if err != nil {
		return BranchStatusFailed, ProviderFailureUnavailable
	}
	alive = 0
	for _, candidate := range refreshed {
		if candidate.Status != ItemStatusFailed {
			alive++
		}
	}
	if alive >= job.RequestedImageCount {
		return BranchStatusSucceeded, ""
	}
	if lastCode == "" {
		lastCode = ProviderFailureInvalidOutput
	}
	return BranchStatusFailed, lastCode
}

// planAuthoringImageSources allocates one source for each missing image slot.
// It spreads new candidates across sources where possible, while allowing a
// single uploaded source to produce the full requested number of variants.
func planAuthoringImageSources(requested int, sourceIDs []string, existing []AuthoringImageCandidate) []string {
	if requested <= 0 || len(sourceIDs) == 0 {
		return nil
	}
	counts := make(map[string]int, len(sourceIDs))
	alive := 0
	for _, candidate := range existing {
		if candidate.Status == ItemStatusFailed {
			continue
		}
		alive++
		counts[candidate.SourceAssetID]++
	}
	remaining := requested - alive
	if remaining <= 0 {
		return nil
	}
	planned := make([]string, 0, remaining)
	for range remaining {
		selected := sourceIDs[0]
		for _, sourceID := range sourceIDs[1:] {
			if counts[sourceID] < counts[selected] {
				selected = sourceID
			}
		}
		planned = append(planned, selected)
		counts[selected]++
	}
	return planned
}

// produceAuthoringCandidate gives the complete source image to the configured
// image model as a visual reference, then stores its full landscape product
// photograph and a copy of the original for merchant comparison. Failures land
// on the candidate alone and never abort the branch.
func (s Service) produceAuthoringCandidate(ctx context.Context, job AuthoringJob, asset authoringSourceAssetRow, variation int) string {
	failCandidate := func(c AuthoringImageCandidate, code string) string {
		c.Status = ItemStatusFailed
		c.FailureCode = code
		if err := s.repo.insertAuthoringImageCandidate(ctx, c); err != nil {
			_ = err
		}
		return code
	}
	base := AuthoringImageCandidate{ID: uuid.NewString(), JobID: job.ID, StoreID: job.StoreID, SourceAssetID: asset.AssetID, SceneKey: s.sceneKey(job.Brief), ContentType: asset.ContentType}
	if base.ContentType == "" {
		base.ContentType = "image/png"
	}
	sourceContentType := asset.ContentType
	if sourceContentType == "" {
		sourceContentType = base.ContentType
	}

	var sourceBytes []byte
	if s.S3Storage != nil {
		reader, err := s.S3Storage.GetObject(ctx, asset.StorageKey)
		if err != nil {
			return failCandidate(base, ProviderFailureUnavailable)
		}
		defer reader.Close()
		sourceBytes, err = io.ReadAll(io.LimitReader(reader, s.maxAuthoringSourceBytes(asset.ByteSize)))
		if err != nil {
			return failCandidate(base, ProviderFailureUnavailable)
		}
	} else {
		// Without an object store the source cannot be proven; the branch
		// fails classified instead of silently succeeding.
		return failCandidate(base, ProviderFailureUnavailable)
	}
	scene, err := s.AuthoringProviders.ImageComposer.GenerateProductImage(ctx, ProductImageRequest{SourceAssetID: asset.AssetID, ContentType: sourceContentType, SourceBytes: sourceBytes, SceneKey: base.SceneKey, Variation: variation})
	if err != nil {
		return failCandidate(base, failureCode(err))
	}
	if s.S3Storage == nil {
		return failCandidate(base, ProviderFailureUnavailable)
	}
	id := uuid.NewString()
	base.ID = id
	base.ContentType = scene.ContentType
	base.ByteSize = int64(len(scene.Bytes))
	base.StorageKey = storageKeyAuthoringCandidate(job.StoreID, job.ID, id, "scene.png")
	base.ForegroundStorageKey = storageKeyAuthoringCandidate(job.StoreID, job.ID, id, "source."+imageFileExtension(sourceContentType))
	if err := s.S3Storage.PutObject(ctx, base.StorageKey, scene.ContentType, scene.Bytes); err != nil {
		return failCandidate(base, ProviderFailureUnavailable)
	}
	if err := s.S3Storage.PutObject(ctx, base.ForegroundStorageKey, sourceContentType, sourceBytes); err != nil {
		return failCandidate(base, ProviderFailureUnavailable)
	}
	base.Status = ItemStatusProposed
	base.Visibility = "hidden"
	if err := s.repo.insertAuthoringImageCandidate(ctx, base); err != nil {
		return ProviderFailureUnavailable
	}
	return ""
}

func imageFileExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}

// maxAuthoringSourceBytes caps how much of a source asset is read into memory
// for provider image requests.
func (s Service) maxAuthoringSourceBytes(actual int64) int64 {
	limit := actual
	if limit <= 0 || limit > 32*1024*1024 {
		limit = 32 * 1024 * 1024
	}
	return limit
}

// sceneKey derives the scene hint in Core from the brief so the merchant is
// never asked to describe the scene.
func (s Service) sceneKey(brief string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(brief)))
	scene := make([]string, 0, 4)
	for _, w := range words {
		clean := strings.Trim(w, "«».,،؛:!؟\u060c\"'()")
		if clean == "" {
			continue
		}
		scene = append(scene, clean)
		if len(scene) == 4 {
			break
		}
	}
	return strings.Join(scene, "-")
}

// storageKeyAuthoringCandidate keeps generated objects under an explicit
// namespace; raw keys stay inside Core and are never returned by the API.
func storageKeyAuthoringCandidate(storeID, jobID, candidateID, name string) string {
	return "ai-authoring/" + storeID + "/" + jobID + "/" + candidateID + "/" + name
}

// AcceptAuthoringCandidateForSubject accepts one proposed image candidate.
// Acceptance is idempotent: an already-accepted candidate returns unchanged.
// The created media-library asset carries origin 'ai_generated' and stays
// linked to the candidate only; product_id remains untouched so linking to a
// product happens through the existing media-reference flow after product
// creation. Raw storage keys stay inside Core.
func (s Service) AcceptAuthoringCandidateForSubject(ctx context.Context, subject, storeID, jobID, candidateID string, req AuthoringAcceptImageRequest) (AuthoringImageCandidate, error) {
	if subject == "" {
		return AuthoringImageCandidate{}, ErrUnauthorized
	}
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	if _, err := s.RequireSellerManagerAccess(ctx, subject, store.SellerID); err != nil {
		return AuthoringImageCandidate{}, err
	}
	job, err := s.repo.GetAuthoringJobForStore(ctx, storeID, jobID)
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	candidate, err := s.repo.getAuthoringCandidateForStore(ctx, storeID, jobID, candidateID)
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	if candidate.Status == ItemStatusAccepted {
		return s.decorateAuthoringCandidate(ctx, candidate), nil
	}
	if candidate.Status != ItemStatusProposed {
		return AuthoringImageCandidate{}, ErrConflict
	}
	accepted, err := s.repo.countAuthoringCandidates(ctx, job.ID, ItemStatusAccepted)
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	if accepted >= job.RequestedImageCount {
		return AuthoringImageCandidate{}, fmt.Errorf("%w: accepted image count reached the requested limit", ErrAIJobConflict)
	}
	checksum := ""
	if s.S3Storage != nil {
		reader, err := s.S3Storage.GetObject(ctx, candidate.StorageKey)
		if err != nil {
			return AuthoringImageCandidate{}, ErrNotFound
		}
		defer reader.Close()
		body, err := io.ReadAll(io.LimitReader(reader, s.maxAuthoringSourceBytes(candidate.ByteSize)))
		if err != nil {
			return AuthoringImageCandidate{}, ErrUnavailable
		}
		sum := sha256.Sum256(body)
		checksum = hex.EncodeToString(sum[:])
	}
	if req.IsVisible {
		// Visibility is recorded on the candidate row; the accepted asset is
		// still hidden until it is attached to a product with an explicit
		// is_visible decision.
		candidate.Visibility = "visible"
	}
	updated, err := s.repo.acceptAuthoringCandidate(ctx, storeID, job.ID, candidateID, candidate.StorageKey, candidate.ContentType, candidate.ByteSize, checksum, subject, req.AltText)
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	return s.decorateAuthoringCandidate(ctx, updated), nil
}

func (s Service) decorateAuthoringCandidate(ctx context.Context, c AuthoringImageCandidate) AuthoringImageCandidate {
	c.URL = ""
	c.ForegroundURL = ""
	if s.S3Storage != nil {
		c.URL = s.S3Storage.ResolvePublicURI(c.StorageKey)
		if c.ForegroundStorageKey != "" {
			c.ForegroundURL = s.S3Storage.ResolvePublicURI(c.ForegroundStorageKey)
		}
	}
	return c
}

func (s Service) validateAuthoringSourceAsset(ctx context.Context, storeID, assetID string) error {
	if storeID == "" || assetID == "" {
		return ErrInvalidInput
	}
	var status, contentType string
	var byteSize int64
	err := s.repo.Pool().QueryRow(ctx, `SELECT status, content_type, byte_size FROM store_media_assets WHERE id = $1 AND store_id = $2`, assetID, storeID).Scan(&status, &contentType, &byteSize)
	if err != nil {
		return translatePGError(err, "get ai authoring source asset")
	}
	if status != "ready" {
		return ErrNotFound
	}
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return ErrInvalidInput
	}
	if byteSize < 1 || byteSize > 10*1024*1024*10 {
		return ErrInvalidInput
	}
	return nil
}

func (s Service) decorateAuthoringJobURLs(job AuthoringJob) AuthoringJob {
	if s.S3Storage == nil {
		return job
	}
	for i := range job.ImageCandidates {
		job.ImageCandidates[i].URL = s.S3Storage.ResolvePublicURI(job.ImageCandidates[i].StorageKey)
		job.ImageCandidates[i].ForegroundURL = s.S3Storage.ResolvePublicURI(job.ImageCandidates[i].ForegroundStorageKey)
	}
	return job
}

func (s Service) PurgeExpiredAuthoringCandidates(ctx context.Context, batchSize int, retention time.Duration) (int, error) {
	candidates, err := s.repo.ClaimExpiredAuthoringCandidates(ctx, batchSize, retention)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, candidate := range candidates {
		if s.S3Storage != nil {
			if candidate.StorageKey != "" {
				_ = s.S3Storage.DeleteObject(ctx, candidate.StorageKey)
			}
			if candidate.ForegroundStorageKey != "" {
				_ = s.S3Storage.DeleteObject(ctx, candidate.ForegroundStorageKey)
			}
		}
		if err := s.repo.DeleteAuthoringCandidate(ctx, candidate.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
