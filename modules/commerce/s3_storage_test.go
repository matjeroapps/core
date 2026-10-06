package commerce

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestS3StoragePresignUsesBrowserReachableEndpointWhenConfigured(t *testing.T) {
	storage := NewS3Storage(S3Config{
		Endpoint:        "http://minio:9000",
		PresignEndpoint: "http://localhost:9000",
		Region:          "us-east-1",
		Bucket:          "media-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		ForcePathStyle:  true,
		PublicBaseURL:   "http://localhost:9000/media-bucket",
	})

	presignedURL, err := storage.PresignPutObject(context.Background(), "stores/store-1/media/test.png", "image/png")
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}

	parsed, err := url.Parse(presignedURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}

	if got, want := parsed.Scheme+"://"+parsed.Host, "http://localhost:9000"; got != want {
		t.Fatalf("presigned URL endpoint = %q, want %q; URL was %s", got, want, presignedURL)
	}
	if !strings.HasPrefix(parsed.Path, "/media-bucket/stores/store-1/media/test.png") {
		t.Fatalf("presigned URL path = %q, want path-style bucket/object path", parsed.Path)
	}
	if strings.Contains(presignedURL, "minio:9000") {
		t.Fatalf("presigned URL leaked container-only endpoint: %s", presignedURL)
	}
}

func TestS3StoragePresignFallsBackToInternalEndpoint(t *testing.T) {
	storage := NewS3Storage(S3Config{
		Endpoint:        "http://minio:9000",
		Region:          "us-east-1",
		Bucket:          "media-bucket",
		AccessKeyID:     "test-key",
		SecretAccessKey: "test-secret",
		ForcePathStyle:  true,
	})

	presignedURL, err := storage.PresignPutObject(context.Background(), "stores/store-1/media/test.png", "image/png")
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}

	parsed, err := url.Parse(presignedURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if got, want := parsed.Scheme+"://"+parsed.Host, "http://minio:9000"; got != want {
		t.Fatalf("presigned URL endpoint = %q, want fallback endpoint %q", got, want)
	}
}
