package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CreateAuthoringJobRecord struct {
	StoreID             string
	CreatedBySubject    string
	Brief               string
	RequestedImageCount int
	EffectiveImageLimit int
	IDempotencyKey      string
	SourceAssetIDs      []string
}

func (r Repository) CreateAuthoringJob(ctx context.Context, rec CreateAuthoringJobRecord) (AuthoringJob, error) {
	if rec.StoreID == "" || rec.CreatedBySubject == "" || rec.RequestedImageCount < 1 || rec.EffectiveImageLimit < 1 {
		return AuthoringJob{}, ErrInvalidInput
	}
	var out AuthoringJob
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if rec.IDempotencyKey != "" {
			existing, err := getAuthoringJobForStoreTx(ctx, tx, rec.StoreID, "", rec.IDempotencyKey)
			if err == nil {
				out = existing
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO ai_authoring_jobs (id, store_id, created_by_subject, brief, requested_image_count, effective_image_limit, status, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, 'pending', NULLIF($7, ''))
			RETURNING id, store_id, created_by_subject, brief, requested_image_count, effective_image_limit, status, text_status, image_status, COALESCE(idempotency_key, ''), attempts, error_code, error_message, created_at, updated_at, started_at, completed_at
		`, id, rec.StoreID, rec.CreatedBySubject, rec.Brief, rec.RequestedImageCount, rec.EffectiveImageLimit, rec.IDempotencyKey).Scan(scanAuthoringJob(&out)...); err != nil {
			return translatePGError(err, "create ai authoring job")
		}
		for i, assetID := range rec.SourceAssetIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ai_authoring_source_assets (job_id, asset_id, store_id, source_order, is_primary_reference)
				VALUES ($1, $2, $3, $4, $5)
			`, id, assetID, rec.StoreID, i, i == 0); err != nil {
				return translatePGError(err, "create ai authoring source asset")
			}
		}
		return nil
	})
	if err != nil {
		return AuthoringJob{}, err
	}
	return r.GetAuthoringJobForStore(ctx, rec.StoreID, out.ID)
}

func (r Repository) GetAuthoringJobForStore(ctx context.Context, storeID, jobID string) (AuthoringJob, error) {
	if storeID == "" || jobID == "" {
		return AuthoringJob{}, ErrInvalidInput
	}
	job, err := getAuthoringJobForStoreTx(ctx, r.pool, storeID, jobID, "")
	if err != nil {
		return AuthoringJob{}, err
	}
	fields, err := r.listAuthoringFields(ctx, storeID, job.ID)
	if err != nil {
		return AuthoringJob{}, err
	}
	candidates, err := r.listAuthoringCandidates(ctx, storeID, job.ID)
	if err != nil {
		return AuthoringJob{}, err
	}
	job.Fields = fields
	job.ImageCandidates = candidates
	return job, nil
}

func (r Repository) ClaimPendingAuthoringJobs(ctx context.Context, batch int) ([]AuthoringJob, error) {
	if batch <= 0 {
		batch = 1
	}
	claimed := []AuthoringJob{}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id
			FROM ai_authoring_jobs
			WHERE status = 'pending'
			ORDER BY created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		`, batch)
		if err != nil {
			return translatePGError(err, "claim ai authoring jobs")
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		for _, id := range ids {
			var job AuthoringJob
			if err := tx.QueryRow(ctx, `
				UPDATE ai_authoring_jobs
				SET status = 'processing', text_status = 'processing', image_status = 'processing', attempts = attempts + 1, started_at = COALESCE(started_at, now()), updated_at = now()
				WHERE id = $1
				RETURNING id, store_id, created_by_subject, brief, requested_image_count, effective_image_limit, status, text_status, image_status, COALESCE(idempotency_key, ''), attempts, error_code, error_message, created_at, updated_at, started_at, completed_at
			`, id).Scan(scanAuthoringJob(&job)...); err != nil {
				return translatePGError(err, "mark ai authoring job processing")
			}
			claimed = append(claimed, job)
		}
		return nil
	})
	return claimed, err
}

func (r Repository) UpdateAuthoringJobStatuses(ctx context.Context, jobID string, status JobStatus, textStatus, imageStatus BranchStatus, errorCode, errorMessage string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_authoring_jobs
		SET status = $2, text_status = $3, image_status = $4, error_code = $5, error_message = $6, completed_at = CASE WHEN $2 IN ('partial','succeeded','failed') THEN now() ELSE completed_at END, updated_at = now()
		WHERE id = $1
	`, jobID, status, textStatus, imageStatus, errorCode, errorMessage)
	return translatePGError(err, "update ai authoring job statuses")
}

func (r Repository) InsertAuthoringFieldProposal(ctx context.Context, jobID, storeID string, field TextFieldResult, status ItemStatus, failureCode string) error {
	if !IsValidFieldKey(field.FieldKey) || !IsValidLocale(field.Locale) {
		return ErrUnsupportedAuthoringField
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_authoring_field_proposals (job_id, store_id, field_key, locale, value, attribute_code, category_id, status, failure_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (job_id, field_key, locale, attribute_code)
		DO UPDATE SET value = EXCLUDED.value, category_id = EXCLUDED.category_id, status = EXCLUDED.status, failure_code = EXCLUDED.failure_code, updated_at = now()
	`, jobID, storeID, field.FieldKey, field.Locale, field.Value, field.AttributeCode, field.CategoryID, status, failureCode)
	return translatePGError(err, "insert ai authoring field proposal")
}

func (r Repository) UpdateAuthoringFieldDecision(ctx context.Context, storeID, jobID, fieldID, subject, decision, value string) (AuthoringFieldProposal, error) {
	status := ItemStatusAccepted
	if decision == "reject" {
		status = ItemStatusRejected
	} else if decision != "accept" {
		return AuthoringFieldProposal{}, ErrInvalidInput
	}
	var out AuthoringFieldProposal
	err := r.pool.QueryRow(ctx, `
		UPDATE ai_authoring_field_proposals
		SET status = $4, value = CASE WHEN $4 = 'accepted' THEN $5 ELSE value END, edited_by_subject = $6, updated_at = now()
		WHERE id = $1 AND job_id = $2 AND store_id = $3 AND status = 'proposed'
		RETURNING id, job_id, store_id, field_key, locale, value, attribute_code, category_id, status, edited_by_subject, failure_code, updated_at
	`, fieldID, jobID, storeID, status, value, subject).Scan(scanAuthoringField(&out)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthoringFieldProposal{}, ErrConflict
	}
	return out, translatePGError(err, "update ai authoring field decision")
}

func scanAuthoringJob(job *AuthoringJob) []any {
	return []any{&job.ID, &job.StoreID, &job.CreatedBySubject, &job.Brief, &job.RequestedImageCount, &job.EffectiveImageLimit, &job.Status, &job.TextStatus, &job.ImageStatus, &job.IDempotencyKey, &job.Attempts, &job.ErrorCode, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt, &job.StartedAt, &job.CompletedAt}
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getAuthoringJobForStoreTx(ctx context.Context, q queryRower, storeID, jobID, idempotencyKey string) (AuthoringJob, error) {
	var job AuthoringJob
	where := "store_id = $1 AND id = $2"
	arg := jobID
	if idempotencyKey != "" {
		where = "store_id = $1 AND idempotency_key = $2"
		arg = idempotencyKey
	}
	err := q.QueryRow(ctx, fmt.Sprintf(`
		SELECT id, store_id, created_by_subject, brief, requested_image_count, effective_image_limit, status, text_status, image_status, COALESCE(idempotency_key, ''), attempts, error_code, error_message, created_at, updated_at, started_at, completed_at
		FROM ai_authoring_jobs
		WHERE %s
	`, where), storeID, arg).Scan(scanAuthoringJob(&job)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthoringJob{}, ErrNotFound
	}
	return job, translatePGError(err, "get ai authoring job")
}

func (r Repository) listAuthoringFields(ctx context.Context, storeID, jobID string) ([]AuthoringFieldProposal, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, job_id, store_id, field_key, locale, value, attribute_code, category_id, status, edited_by_subject, failure_code, updated_at
		FROM ai_authoring_field_proposals
		WHERE store_id = $1 AND job_id = $2
		ORDER BY created_at ASC
	`, storeID, jobID)
	if err != nil {
		return nil, translatePGError(err, "list ai authoring fields")
	}
	defer rows.Close()
	var out []AuthoringFieldProposal
	for rows.Next() {
		var f AuthoringFieldProposal
		if err := rows.Scan(scanAuthoringField(&f)...); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanAuthoringField(f *AuthoringFieldProposal) []any {
	return []any{&f.ID, &f.JobID, &f.StoreID, &f.FieldKey, &f.Locale, &f.Value, &f.AttributeCode, &f.CategoryID, &f.Status, &f.EditedBySubject, &f.FailureCode, &f.UpdatedAt}
}

func (r Repository) listAuthoringCandidates(ctx context.Context, storeID, jobID string) ([]AuthoringImageCandidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, job_id, store_id, source_asset_id, scene_key, storage_key, foreground_storage_key, content_type, byte_size, visibility, status, asset_id, product_id, failure_code, retry_count, COALESCE(alt_text, ''), updated_at
		FROM ai_authoring_image_candidates
		WHERE store_id = $1 AND job_id = $2
		ORDER BY created_at ASC
	`, storeID, jobID)
	if err != nil {
		return nil, translatePGError(err, "list ai authoring candidates")
	}
	defer rows.Close()
	var out []AuthoringImageCandidate
	for rows.Next() {
		var c AuthoringImageCandidate
		if err := rows.Scan(scanAuthoringCandidate(&c)...); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanAuthoringCandidate(c *AuthoringImageCandidate) []any {
	return []any{&c.ID, &c.JobID, &c.StoreID, &c.SourceAssetID, &c.SceneKey, &c.StorageKey, &c.ForegroundStorageKey, &c.ContentType, &c.ByteSize, &c.Visibility, &c.Status, &c.AssetID, &c.ProductID, &c.FailureCode, &c.RetryCount, &c.AltText, &c.UpdatedAt}
}

func (r Repository) ClaimExpiredAuthoringCandidates(ctx context.Context, batchSize int, retention time.Duration) ([]AuthoringImageCandidate, error) {
	if batchSize <= 0 {
		batchSize = 20
	}
	claimed := []AuthoringImageCandidate{}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, job_id, store_id, source_asset_id, scene_key, storage_key, foreground_storage_key, content_type, byte_size, visibility, status, asset_id, product_id, failure_code, retry_count, updated_at
			FROM ai_authoring_image_candidates
			WHERE status IN ('proposed','rejected','failed') AND updated_at < now() - $1::interval
			ORDER BY updated_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		`, retention.String(), batchSize)
		if err != nil {
			return translatePGError(err, "claim expired ai authoring candidates")
		}
		defer rows.Close()
		for rows.Next() {
			var c AuthoringImageCandidate
			if err := rows.Scan(scanAuthoringCandidate(&c)...); err != nil {
				return err
			}
			claimed = append(claimed, c)
		}
		return rows.Err()
	})
	return claimed, err
}

func (r Repository) DeleteAuthoringCandidate(ctx context.Context, candidateID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM ai_authoring_image_candidates WHERE id = $1 AND status <> 'accepted'`, candidateID)
	return translatePGError(err, "delete ai authoring candidate")
}

// authoringSourceAsset carries the joined media-library fields needed to run
// the image branch for one source asset.
type authoringSourceAssetRow struct {
	AssetID     string
	StorageKey  string
	ContentType string
	ByteSize    int64
}

func (r Repository) listAuthoringJobCandidates(ctx context.Context, storeID, jobID string) ([]AuthoringImageCandidate, error) {
	return r.listAuthoringCandidates(ctx, storeID, jobID)
}

func (r Repository) listAuthoringSourceAssets(ctx context.Context, storeID, jobID string) ([]authoringSourceAssetRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sa.asset_id, m.storage_key, m.content_type, m.byte_size
		FROM ai_authoring_source_assets sa
		JOIN store_media_assets m ON m.id = sa.asset_id AND m.store_id = sa.store_id
		WHERE sa.store_id = $1 AND sa.job_id = $2
		ORDER BY sa.source_order ASC
	`, storeID, jobID)
	if err != nil {
		return nil, translatePGError(err, "list ai authoring source assets")
	}
	defer rows.Close()
	out := []authoringSourceAssetRow{}
	for rows.Next() {
		var row authoringSourceAssetRow
		if err := rows.Scan(&row.AssetID, &row.StorageKey, &row.ContentType, &row.ByteSize); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r Repository) insertAuthoringImageCandidate(ctx context.Context, c AuthoringImageCandidate) error {
	if c.JobID == "" || c.StoreID == "" || c.SourceAssetID == "" || c.ContentType == "" {
		return ErrInvalidInput
	}
	if c.Status != ItemStatusFailed && c.StorageKey == "" {
		return ErrInvalidInput
	}
	if c.Status == "" {
		c.Status = ItemStatusProposed
	}
	if c.Visibility == "" {
		c.Visibility = "hidden"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_authoring_image_candidates (id, job_id, store_id, source_asset_id, scene_key, storage_key, foreground_storage_key, content_type, byte_size, visibility, status, failure_code, alt_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, c.ID, c.JobID, c.StoreID, c.SourceAssetID, c.SceneKey, c.StorageKey, c.ForegroundStorageKey, c.ContentType, c.ByteSize, c.Visibility, c.Status, c.FailureCode, c.AltText)
	return translatePGError(err, "insert ai authoring image candidate")
}

func (r Repository) getAuthoringCandidateForStore(ctx context.Context, storeID, jobID, candidateID string) (AuthoringImageCandidate, error) {
	return getAuthoringCandidateForStoreTx(ctx, r.pool, storeID, jobID, candidateID)
}

func (r Repository) countAuthoringCandidates(ctx context.Context, jobID string, statuses ...ItemStatus) (int, error) {
	if len(statuses) == 0 {
		return 0, ErrInvalidInput
	}
	args := []any{jobID}
	conds := make([]string, 0, len(statuses))
	for i, s := range statuses {
		args = append(args, s)
		conds = append(conds, "status = $"+fmt.Sprint(i+2))
	}
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM ai_authoring_image_candidates WHERE job_id = $1 AND (`+strings.Join(conds, " OR ")+`)
	`, args...).Scan(&count)
	return count, translatePGError(err, "count ai authoring image candidates")
}

// acceptAuthoringCandidate creates the media-library asset for an accepted
// candidate and links it to the candidate inside one transaction. product_id
// stays NULL: products may not exist yet, so linking happens only through the
// existing media-reference flow after product creation.
func (r Repository) acceptAuthoringCandidate(ctx context.Context, storeID, jobID, candidateID, storageKey, contentType string, byteSize int64, checksum, subject, altText string) (AuthoringImageCandidate, error) {
	var out AuthoringImageCandidate
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// Idempotency and state guard: only a still-proposed row transitions.
		tag, err := tx.Exec(ctx, `
			UPDATE ai_authoring_image_candidates
			SET status = 'accepted', visibility = 'hidden', alt_text = $4, updated_at = now()
			WHERE id = $1 AND job_id = $2 AND store_id = $3 AND status = 'proposed'
		`, candidateID, jobID, storeID, altText)
		if err != nil {
			return translatePGError(err, "accept ai authoring image candidate")
		}
		if tag.RowsAffected() == 0 {
			current, getErr := getAuthoringCandidateForStoreTx(ctx, tx, storeID, jobID, candidateID)
			if getErr != nil {
				return getErr
			}
			if current.Status == ItemStatusAccepted {
				out = current
				return nil
			}
			return ErrConflict
		}
		assetID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO store_media_assets (id, store_id, checksum_sha256, storage_key, content_type, byte_size, original_filename, status, created_by_subject, origin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'ready', $8, 'ai_generated')
		`, assetID, storeID, checksum, storageKey, contentType, byteSize, "ai-generated.png", subject); err != nil {
			return translatePGError(err, "create ai generated store media asset")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE ai_authoring_image_candidates SET asset_id = $4, updated_at = now()
			WHERE id = $1 AND job_id = $2 AND store_id = $3
		`, candidateID, jobID, storeID, assetID); err != nil {
			return translatePGError(err, "link asset to ai authoring candidate")
		}
		linked, linkErr := getAuthoringCandidateForStoreTx(ctx, tx, storeID, jobID, candidateID)
		if linkErr != nil {
			return linkErr
		}
		out = linked
		return nil
	})
	if err != nil {
		return AuthoringImageCandidate{}, err
	}
	return out, nil
}

func getAuthoringCandidateForStoreTx(ctx context.Context, q queryRower, storeID, jobID, candidateID string) (AuthoringImageCandidate, error) {
	var c AuthoringImageCandidate
	err := q.QueryRow(ctx, `
		SELECT id, job_id, store_id, source_asset_id, scene_key, storage_key, foreground_storage_key, content_type, byte_size, visibility, status, asset_id, product_id, failure_code, retry_count, COALESCE(alt_text, ''), updated_at
		FROM ai_authoring_image_candidates
		WHERE store_id = $1 AND job_id = $2 AND id = $3
	`, storeID, jobID, candidateID).Scan(scanAuthoringCandidate(&c)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthoringImageCandidate{}, ErrNotFound
	}
	return c, translatePGError(err, "get ai authoring image candidate")
}
