package commerce

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func createTestStoreAndSubject(t *testing.T, service Service, repo Repository, name string) (string, string, string) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d-%s", time.Now().UnixNano(), name)
	if len(suffix) > 30 {
		suffix = suffix[:30]
	}
	subject := "sub-" + suffix
	seller, err := repo.CreateSeller(ctx, "sel-"+suffix, "Seller "+name, "active", nil)
	require.NoError(t, err)

	_, err = repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active")
	require.NoError(t, err)

	store, err := repo.CreateStore(ctx, seller.ID, "EG", "st-"+suffix, "Store "+name, "active", nil)
	require.NoError(t, err)

	return subject, seller.ID, store.ID
}

func TestStoreMediaAsset_DeduplicationPerStore(t *testing.T) {
	_, service, repo, _ := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub1, _, store1 := createTestStoreAndSubject(t, service, repo, "store1")
	sub2, _, store2 := createTestStoreAndSubject(t, service, repo, "store2")

	content := []byte("fake image content for test deduplication")
	hash := sha256.Sum256(content)
	checksumHex := hex.EncodeToString(hash[:])

	// S3 mock returning content
	deletedKeys := make(map[string]bool)
	service.S3Storage = &S3Storage{
		cfg: S3Config{URLTTL: 15 * time.Minute},
		MockGetObject: func(ctx context.Context, storageKey string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(content)), nil
		},
		MockPresignPutObject: func(ctx context.Context, storageKey, contentType string) (string, error) {
			return "https://s3.test/" + storageKey, nil
		},
		MockDeleteObject: func(ctx context.Context, storageKey string) error {
			deletedKeys[storageKey] = true
			return nil
		},
	}

	// 1. Store 1 presigns upload for content
	presign1, err := service.PresignStoreMediaUpload(ctx, sub1, store1, PresignMediaUploadRequest{
		ClientUploadID: "cl-upload-1",
		Filename:       "test1.png",
		ContentType:    "image/webp",
		SizeBytes:      int64(len(content)),
		ChecksumSHA256: checksumHex,
	})
	require.NoError(t, err)
	require.Equal(t, "upload", presign1.Mode)
	require.NotEmpty(t, presign1.IntentID)

	// Complete upload for Store 1
	asset1, err := service.CompleteStoreMediaUpload(ctx, sub1, store1, presign1.IntentID, CompleteMediaUploadRequest{
		UploadToken: presign1.UploadToken,
		StorageKey:  presign1.StorageKey,
	})
	require.NoError(t, err)
	require.Equal(t, store1, asset1.StoreID)
	require.Equal(t, checksumHex, asset1.ChecksumSHA256)
	require.Equal(t, "ready", asset1.Status)

	// 2. Store 1 presigns upload for SAME content again -> should return REUSE mode!
	presign1Reuse, err := service.PresignStoreMediaUpload(ctx, sub1, store1, PresignMediaUploadRequest{
		ClientUploadID: "cl-upload-2",
		Filename:       "test1-dup.png",
		ContentType:    "image/webp",
		SizeBytes:      int64(len(content)),
		ChecksumSHA256: checksumHex,
	})
	require.NoError(t, err)
	require.Equal(t, "reuse", presign1Reuse.Mode)
	require.NotNil(t, presign1Reuse.Asset)
	require.Equal(t, asset1.ID, presign1Reuse.Asset.ID)

	// 3. Store 2 presigns upload for SAME content -> should return UPLOAD mode (no cross-store dedup)!
	presign2, err := service.PresignStoreMediaUpload(ctx, sub2, store2, PresignMediaUploadRequest{
		ClientUploadID: "cl-upload-store2",
		Filename:       "test-store2.png",
		ContentType:    "image/webp",
		SizeBytes:      int64(len(content)),
		ChecksumSHA256: checksumHex,
	})
	require.NoError(t, err)
	require.Equal(t, "upload", presign2.Mode)
	require.NotEmpty(t, presign2.IntentID)

	asset2, err := service.CompleteStoreMediaUpload(ctx, sub2, store2, presign2.IntentID, CompleteMediaUploadRequest{
		UploadToken: presign2.UploadToken,
		StorageKey:  presign2.StorageKey,
	})
	require.NoError(t, err)
	require.Equal(t, store2, asset2.StoreID)
	require.NotEqual(t, asset1.ID, asset2.ID)
}

func TestPresignStoreMediaUpload_IdempotencyAndFingerprintConflict(t *testing.T) {
	_, service, repo, _ := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub, _, store := createTestStoreAndSubject(t, service, repo, "presign-idempotency")
	req := PresignMediaUploadRequest{
		ClientUploadID: "idempotent-client-id-1",
		Filename:       "hero.jpg",
		ContentType:    "image/jpeg",
		SizeBytes:      2048,
		ChecksumSHA256: "a1b2c3d4e5f60000000000000000000000000000000000000000000000000000",
	}

	service.S3Storage = &S3Storage{
		cfg: S3Config{URLTTL: 15 * time.Minute},
		MockPresignPutObject: func(ctx context.Context, storageKey, contentType string) (string, error) {
			return "https://s3.test/" + storageKey, nil
		},
	}

	// First presign call
	p1, err := service.PresignStoreMediaUpload(ctx, sub, store, req)
	require.NoError(t, err)

	// Second presign call with identical client_upload_id and params -> idempotent return
	p2, err := service.PresignStoreMediaUpload(ctx, sub, store, req)
	require.NoError(t, err)
	require.Equal(t, p1.IntentID, p2.IntentID)
	require.Equal(t, p1.StorageKey, p2.StorageKey)

	// Third presign call with same client_upload_id but DIFFERENT params -> fingerprint mismatch error
	reqMismatch := req
	reqMismatch.SizeBytes = 99999
	_, err = service.PresignStoreMediaUpload(ctx, sub, store, reqMismatch)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestCompleteStoreMediaUpload_ChecksumMismatchAndDeletion(t *testing.T) {
	_, service, repo, _ := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub, _, store := createTestStoreAndSubject(t, service, repo, "checksum-mismatch")

	correctContent := []byte("correct media content")
	correctHash := sha256.Sum256(correctContent)
	expectedChecksumHex := hex.EncodeToString(correctHash[:])

	badContent := []byte("corrupted or modified media content")

	deletedKeys := make(map[string]bool)
	service.S3Storage = &S3Storage{
		cfg: S3Config{URLTTL: 15 * time.Minute},
		MockGetObject: func(ctx context.Context, storageKey string) (io.ReadCloser, error) {
			// Return bad content that does not match expected checksum!
			return io.NopCloser(bytes.NewReader(badContent)), nil
		},
		MockPresignPutObject: func(ctx context.Context, storageKey, contentType string) (string, error) {
			return "https://s3.test/" + storageKey, nil
		},
		MockDeleteObject: func(ctx context.Context, storageKey string) error {
			deletedKeys[storageKey] = true
			return nil
		},
	}

	presign, err := service.PresignStoreMediaUpload(ctx, sub, store, PresignMediaUploadRequest{
		ClientUploadID: "checksum-test-1",
		Filename:       "test.png",
		ContentType:    "image/png",
		SizeBytes:      int64(len(correctContent)),
		ChecksumSHA256: expectedChecksumHex,
	})
	require.NoError(t, err)

	// Complete upload -> should fail with ErrChecksumMismatch and trigger S3 deletion!
	_, err = service.CompleteStoreMediaUpload(ctx, sub, store, presign.IntentID, CompleteMediaUploadRequest{
		UploadToken: presign.UploadToken,
		StorageKey:  presign.StorageKey,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrChecksumMismatch)
	require.True(t, deletedKeys[presign.StorageKey], "MinIO object should be deleted on checksum mismatch")
}

func createTestMediaAsset(t *testing.T, ctx context.Context, repo Repository, intentID string, asset StoreMediaAsset) StoreMediaAsset {
	t.Helper()
	_, err := repo.CreateMediaUploadIntent(ctx, MediaUploadIntent{
		ID:               intentID,
		StoreID:          asset.StoreID,
		ClientUploadID:   &intentID,
		OriginalFilename: asset.OriginalFilename,
		ContentType:      asset.ContentType,
		ByteSize:         asset.ByteSize,
		ChecksumSHA256:   asset.ChecksumSHA256,
		StorageKey:       asset.StorageKey,
		TokenDigest:      "dummy",
		ExpiresAt:        time.Now().Add(1 * time.Hour),
	})
	require.NoError(t, err)

	created, err := repo.CompleteMediaUploadAndCreateAsset(ctx, intentID, asset)
	require.NoError(t, err)
	return created
}

func TestProductMediaReferences_AttachPrimaryPromotionAndDetach(t *testing.T) {
	_, service, repo, suffix := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub, _, storeID := createTestStoreAndSubject(t, service, repo, "media-refs-"+suffix)

	// Create a product via service
	productDetail, err := service.CreateSellerProductForSubject(ctx, sub, storeID, SellerProductDraft{
		Slug: "prod-ref-test-" + suffix,
	})
	require.NoError(t, err)
	productID := productDetail.Product.ID

	// Create 2 ready store media assets
	assetA := createTestMediaAsset(t, ctx, repo, "intent-a-"+suffix, StoreMediaAsset{
		ID:               "asset-a-" + suffix,
		StoreID:          storeID,
		ChecksumSHA256:   "a1b2c3d4e5f60000000000000000000000000000000000000000000000000001",
		StorageKey:       "stores/" + storeID + "/a1.jpg",
		ContentType:      "image/jpeg",
		ByteSize:         1000,
		OriginalFilename: "a1.jpg",
		Status:           "ready",
		CreatedBySubject: sub,
	})

	assetB := createTestMediaAsset(t, ctx, repo, "intent-b-"+suffix, StoreMediaAsset{
		ID:               "asset-b-" + suffix,
		StoreID:          storeID,
		ChecksumSHA256:   "a1b2c3d4e5f60000000000000000000000000000000000000000000000000002",
		StorageKey:       "stores/" + storeID + "/b2.jpg",
		ContentType:      "image/jpeg",
		ByteSize:         2000,
		OriginalFilename: "b2.jpg",
		Status:           "ready",
		CreatedBySubject: sub,
	})

	// 1. Attach Asset A to product (is_primary = false)
	refA, err := service.AttachProductMediaReference(ctx, sub, storeID, productID, AttachMediaReferenceRequest{
		AssetID:   assetA.ID,
		AltText:   "Image A",
		SortOrder: 1,
		IsPrimary: false,
	})
	require.NoError(t, err)
	// First attached media automatically becomes primary!
	require.True(t, refA.IsPrimary)

	// 2. Attach Asset B to product (is_primary = true) -> should promote B and demote A
	refB, err := service.AttachProductMediaReference(ctx, sub, storeID, productID, AttachMediaReferenceRequest{
		AssetID:   assetB.ID,
		AltText:   "Image B",
		SortOrder: 2,
		IsPrimary: true,
	})
	require.NoError(t, err)
	require.True(t, refB.IsPrimary)

	// Verify Ref A was demoted to non-primary
	refs, err := service.ListProductMediaReferences(ctx, sub, storeID, productID)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	for _, r := range refs {
		if r.ID == refA.ID {
			require.False(t, r.IsPrimary)
		} else if r.ID == refB.ID {
			require.True(t, r.IsPrimary)
		}
	}

	// 3. Detach Ref B (the primary ref) -> Ref A should be automatically promoted to primary!
	err = service.DetachProductMediaReference(ctx, sub, storeID, productID, refB.ID)
	require.NoError(t, err)

	refsAfter, err := service.ListProductMediaReferences(ctx, sub, storeID, productID)
	require.NoError(t, err)
	require.Len(t, refsAfter, 1)
	require.Equal(t, refA.ID, refsAfter[0].ID)
	require.True(t, refsAfter[0].IsPrimary)
}

func TestDeleteStoreMediaAsset_InUseConflictAndAsyncDeletion(t *testing.T) {
	_, service, repo, suffix := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub, _, storeID := createTestStoreAndSubject(t, service, repo, "delete-asset-"+suffix)

	productDetail, err := service.CreateSellerProductForSubject(ctx, sub, storeID, SellerProductDraft{
		Slug: "prod-del-test-" + suffix,
	})
	require.NoError(t, err)
	productID := productDetail.Product.ID

	asset := createTestMediaAsset(t, ctx, repo, "intent-del-"+suffix, StoreMediaAsset{
		ID:               "asset-del-" + suffix,
		StoreID:          storeID,
		ChecksumSHA256:   "a1b2c3d4e5f60000000000000000000000000000000000000000000000000099",
		StorageKey:       "stores/" + storeID + "/del.jpg",
		ContentType:      "image/jpeg",
		ByteSize:         500,
		OriginalFilename: "del.jpg",
		Status:           "ready",
		CreatedBySubject: sub,
	})

	ref, err := service.AttachProductMediaReference(ctx, sub, storeID, productID, AttachMediaReferenceRequest{
		AssetID:   asset.ID,
		AltText:   "ToDelete",
		SortOrder: 0,
		IsPrimary: true,
	})
	require.NoError(t, err)

	// 1. Attempting to delete asset when referenced -> fails with ErrMediaInUse
	err = service.DeleteStoreMediaAsset(ctx, sub, storeID, asset.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMediaInUse)

	// 2. Detach reference first
	err = service.DetachProductMediaReference(ctx, sub, storeID, productID, ref.ID)
	require.NoError(t, err)

	// Track S3 deletes
	s3Deleted := make(map[string]bool)
	service.S3Storage = &S3Storage{
		cfg: S3Config{URLTTL: 15 * time.Minute},
		MockDeleteObject: func(ctx context.Context, storageKey string) error {
			s3Deleted[storageKey] = true
			return nil
		},
	}

	// 3. Delete asset -> succeeds and marks deleting!
	err = service.DeleteStoreMediaAsset(ctx, sub, storeID, asset.ID)
	require.NoError(t, err)

	// Verify status in DB is "deleting"
	updatedAsset, err := repo.GetStoreMediaAssetByID(ctx, storeID, asset.ID)
	require.NoError(t, err)
	require.Equal(t, "deleting", updatedAsset.Status)

	// 4. Process pending deletions async worker
	processedCount, err := service.ProcessPendingMediaDeletions(ctx, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, processedCount, 1)

	// Verify asset is now "deleted" and S3 delete was invoked!
	finalAsset, err := repo.GetStoreMediaAssetByID(ctx, storeID, asset.ID)
	require.NoError(t, err)
	require.Equal(t, "deleted", finalAsset.Status)
	require.True(t, s3Deleted[asset.StorageKey])
}

func TestTenantStoreAuthorization_CrossStoreAccessDenied(t *testing.T) {
	_, service, repo, suffix := setupSellerCatalogTestDB(t)
	ctx := context.Background()

	sub1, _, store1 := createTestStoreAndSubject(t, service, repo, "tenant1-"+suffix)
	sub2, _, store2 := createTestStoreAndSubject(t, service, repo, "tenant2-"+suffix)

	// Store 1 asset
	asset1 := createTestMediaAsset(t, ctx, repo, "intent-t1-"+suffix, StoreMediaAsset{
		ID:               "asset-t1-" + suffix,
		StoreID:          store1,
		ChecksumSHA256:   "1111111111111111111111111111111111111111111111111111111111111111",
		StorageKey:       "stores/" + store1 + "/t1.jpg",
		ContentType:      "image/jpeg",
		ByteSize:         500,
		OriginalFilename: "t1.jpg",
		Status:           "ready",
		CreatedBySubject: sub1,
	})

	// Store 2 product
	productDetail2, err := service.CreateSellerProductForSubject(ctx, sub2, store2, SellerProductDraft{
		Slug: "prod-t2-" + suffix,
	})
	require.NoError(t, err)
	productID2 := productDetail2.Product.ID

	// Subject 2 attempting to attach Store 1's Asset to Store 2's Product -> fails with ErrNotFound (compound FK / tenant boundary)
	_, err = service.AttachProductMediaReference(ctx, sub2, store2, productID2, AttachMediaReferenceRequest{
		AssetID:   asset1.ID,
		AltText:   "Cross-tenant attempt",
		SortOrder: 1,
		IsPrimary: true,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)

	// Subject 1 attempting to access Store 2's media endpoint -> fails with ErrNotFound
	_, err = service.ListStoreMediaAssets(ctx, sub1, store2, "", "", 10, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
}
