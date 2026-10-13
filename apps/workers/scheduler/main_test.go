package main

import (
	"io"
	"log/slog"
	"testing"

	"core/modules/commerce"
	"core/packages/config"
)

func TestConfigureMediaStorageInitializesWorkerStorage(t *testing.T) {
	service := &commerce.Service{}
	cfg := config.Config{
		Environment:            "development",
		MediaS3Endpoint:        "http://127.0.0.1:9000",
		MediaS3PresignEndpoint: "http://localhost:9000",
		MediaS3Region:          "us-east-1",
		MediaS3Bucket:          "media-test",
		MediaS3AccessKeyID:     "test-access",
		MediaS3SecretAccessKey: "test-secret",
		MediaS3ForcePathStyle:  true,
		MediaPublicBaseURL:     "http://localhost:9000/media-test",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := configureMediaStorage(service, cfg, logger); err != nil {
		t.Fatalf("configureMediaStorage() error = %v", err)
	}
	if service.S3Storage == nil {
		t.Fatal("scheduler service has no media storage")
	}
	got := service.S3Storage.Config()
	if got.Bucket != cfg.MediaS3Bucket || got.Endpoint != cfg.MediaS3Endpoint || got.AccessKeyID != cfg.MediaS3AccessKeyID {
		t.Fatalf("scheduler media storage config = %+v", got)
	}
}

func TestConfigureMediaStorageAllowsMissingBucketOutsideProduction(t *testing.T) {
	service := &commerce.Service{}
	cfg := config.Config{Environment: "development"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := configureMediaStorage(service, cfg, logger); err != nil {
		t.Fatalf("configureMediaStorage() error = %v, want nil in development", err)
	}
	if service.S3Storage != nil {
		t.Fatal("configureMediaStorage() configured storage without a bucket")
	}
}

func TestConfigureMediaStorageRequiresBucketInProduction(t *testing.T) {
	service := &commerce.Service{}
	cfg := config.Config{Environment: "production"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	err := configureMediaStorage(service, cfg, logger)
	if err == nil || err.Error() != "MEDIA_S3_BUCKET is required in production" {
		t.Fatalf("configureMediaStorage() error = %v, want missing production bucket error", err)
	}
	if service.S3Storage != nil {
		t.Fatal("configureMediaStorage() configured storage without a bucket")
	}
}
