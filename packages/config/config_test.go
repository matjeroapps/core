package config_test

import (
	"os"
	"testing"
	"time"

	"core/packages/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.ServiceName != "test-service" {
		t.Errorf("expected ServiceName 'test-service', got %s", cfg.ServiceName)
	}
	if cfg.CheckoutSessionLifetime != 30*time.Minute {
		t.Errorf("expected CheckoutSessionLifetime 30m, got %v", cfg.CheckoutSessionLifetime)
	}
	if cfg.OrderConfirmationDuration != 15*time.Minute {
		t.Errorf("expected OrderConfirmationDuration 15m, got %v", cfg.OrderConfirmationDuration)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.StoreDefaultMaxActiveStores != 1 {
		t.Errorf("expected StoreDefaultMaxActiveStores 1, got %d", cfg.StoreDefaultMaxActiveStores)
	}
}

func TestLoadCustomStoreDefaultMaxActiveStores(t *testing.T) {
	t.Setenv("STORE_DEFAULT_MAX_ACTIVE_STORES", "3")

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.StoreDefaultMaxActiveStores != 3 {
		t.Errorf("expected StoreDefaultMaxActiveStores 3, got %d", cfg.StoreDefaultMaxActiveStores)
	}
}

func TestLoadReadsPlatformInternalToken(t *testing.T) {
	t.Setenv("CORE_INTERNAL_PLATFORM_TOKEN", "platform-token")

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.InternalPlatformToken != "platform-token" {
		t.Fatalf("InternalPlatformToken = %q, want %q", cfg.InternalPlatformToken, "platform-token")
	}
}

func TestLoadReadsMediaS3PresignEndpoint(t *testing.T) {
	t.Setenv("MEDIA_S3_ENDPOINT", "http://minio:9000")
	t.Setenv("MEDIA_S3_PRESIGN_ENDPOINT", "http://localhost:9000")

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.MediaS3Endpoint != "http://minio:9000" {
		t.Fatalf("MediaS3Endpoint = %q", cfg.MediaS3Endpoint)
	}
	if cfg.MediaS3PresignEndpoint != "http://localhost:9000" {
		t.Fatalf("MediaS3PresignEndpoint = %q", cfg.MediaS3PresignEndpoint)
	}
}

func TestLoadRejectsInvalidStoreDefaultMaxActiveStores(t *testing.T) {
	invalidValues := []string{"not-an-int", "0", "-1", "-5"}
	for _, val := range invalidValues {
		t.Setenv("STORE_DEFAULT_MAX_ACTIVE_STORES", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for STORE_DEFAULT_MAX_ACTIVE_STORES=%q, got nil", val)
		}
	}
}

func TestLoadCustomOrderConfirmationDuration(t *testing.T) {
	t.Setenv("ORDER_CONFIRMATION_DURATION", "45m")

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.OrderConfirmationDuration != 45*time.Minute {
		t.Errorf("expected OrderConfirmationDuration 45m, got %v", cfg.OrderConfirmationDuration)
	}
}

func TestLoadRejectsInvalidOrderConfirmationDuration(t *testing.T) {
	invalidValues := []string{"not-a-duration", "0", "-5m", "0s"}
	for _, val := range invalidValues {
		t.Setenv("ORDER_CONFIRMATION_DURATION", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for ORDER_CONFIRMATION_DURATION=%q, got nil", val)
		}
	}
}

func TestLoadRejectsInvalidCheckoutSessionLifetime(t *testing.T) {
	invalidValues := []string{"invalid", "0", "-10m", "0s"}
	for _, val := range invalidValues {
		t.Setenv("CHECKOUT_SESSION_LIFETIME", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for CHECKOUT_SESSION_LIFETIME=%q, got nil", val)
		}
	}
}

func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	invalidValues := []string{"invalid", "0", "-5"}
	for _, val := range invalidValues {
		t.Setenv("SHUTDOWN_TIMEOUT_SECONDS", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for SHUTDOWN_TIMEOUT_SECONDS=%q, got nil", val)
		}
	}
}

func TestConfigOutboxDefaults(t *testing.T) {
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean config load, got: %v", err)
	}

	if cfg.OutboxClaimLeaseDuration != 30*time.Second {
		t.Errorf("expected lease duration 30s, got %v", cfg.OutboxClaimLeaseDuration)
	}
	if cfg.OutboxClaimRenewalMargin != 10*time.Second {
		t.Errorf("expected renewal margin 10s, got %v", cfg.OutboxClaimRenewalMargin)
	}
	if cfg.RabbitMQPublishConfirmTimeout != 5*time.Second {
		t.Errorf("expected confirm timeout 5s, got %v", cfg.RabbitMQPublishConfirmTimeout)
	}
	if cfg.OutboxBatchSize != 50 {
		t.Errorf("expected batch size 50, got %d", cfg.OutboxBatchSize)
	}
	if cfg.OutboxPollInterval != 500*time.Millisecond {
		t.Errorf("expected poll interval 500ms, got %v", cfg.OutboxPollInterval)
	}
}

func TestConfigValidatesRenewalMarginLessThanLease(t *testing.T) {
	t.Setenv("OUTBOX_CLAIM_LEASE_DURATION", "10s")
	t.Setenv("OUTBOX_CLAIM_RENEWAL_MARGIN", "10s")

	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error when renewal margin equals lease duration, got nil")
	}
}

func TestConfigValidatesRenewalMarginNonPositive(t *testing.T) {
	for _, val := range []string{"0", "0s", "-1s"} {
		t.Setenv("OUTBOX_CLAIM_RENEWAL_MARGIN", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for OUTBOX_CLAIM_RENEWAL_MARGIN=%q, got nil", val)
		}
	}
}

func TestConfigValidatesConfirmTimeoutLessThanLease(t *testing.T) {
	t.Setenv("OUTBOX_CLAIM_LEASE_DURATION", "5s")
	t.Setenv("RABBITMQ_PUBLISH_CONFIRM_TIMEOUT", "10s")

	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error when confirm timeout exceeds lease duration, got nil")
	}
}

func TestConfigValidatesConfirmTimeoutNonPositive(t *testing.T) {
	for _, val := range []string{"0", "0s", "-1s"} {
		t.Setenv("RABBITMQ_PUBLISH_CONFIRM_TIMEOUT", val)
		_, err := config.Load("test-service")
		if err == nil {
			t.Fatalf("expected error for RABBITMQ_PUBLISH_CONFIRM_TIMEOUT=%q, got nil", val)
		}
	}
}

func TestConfigValidatesLeaseDurationPositive(t *testing.T) {
	t.Setenv("OUTBOX_CLAIM_LEASE_DURATION", "-5s")

	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error for non-positive lease duration, got nil")
	}
}

func TestConfigValidatesBatchSizePositive(t *testing.T) {
	t.Setenv("OUTBOX_BATCH_SIZE", "0")

	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error for non-positive batch size, got nil")
	}
}

func TestConfigValidatesPollIntervalPositive(t *testing.T) {
	t.Setenv("OUTBOX_POLL_INTERVAL", "0s")

	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error for non-positive poll interval, got nil")
	}
}

func TestProductionConfigValidation(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	// Missing production database URL triggers fail-fast error
	_, err := config.Load("test-service")
	if err == nil {
		t.Fatal("expected error for production config with default localhost database, got nil")
	}

	setValidBaseProdEnv := func() {
		t.Setenv("DATABASE_URL", "postgres://user:pass@prod-db.internal:5432/commerce?sslmode=require")
		t.Setenv("RABBITMQ_URL", "amqp://user:pass@prod-mq.internal:5672/")
		t.Setenv("ZITADEL_ISSUER", "https://auth.matjero.com")
		t.Setenv("CORE_INTERNAL_PLATFORM_TOKEN", "secret-platform")
		t.Setenv("CORE_INTERNAL_SELLER_TOKEN", "secret-seller")
		t.Setenv("CORE_INTERNAL_ADMIN_TOKEN", "secret-admin")
		t.Setenv("CORE_INTERNAL_SUPPLIER_TOKEN", "secret-supplier")
		t.Setenv("THEME_PREVIEW_SECRET", "secret-theme-preview")
	}

	setValidS3Env := func() {
		t.Setenv("MEDIA_S3_BUCKET", "prod-media")
		t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "key")
		t.Setenv("MEDIA_S3_SECRET_ACCESS_KEY", "secret")
		t.Setenv("MEDIA_PUBLIC_BASE_URL", "https://cdn.matjero.com")
	}

	// 1. Missing bucket -> FAIL
	setValidBaseProdEnv()
	if _, err := config.Load("test-service"); err == nil {
		t.Fatal("expected error for missing MEDIA_S3_BUCKET in production, got nil")
	}

	// 2. Missing access key -> FAIL
	setValidBaseProdEnv()
	t.Setenv("MEDIA_S3_BUCKET", "prod-media")
	if _, err := config.Load("test-service"); err == nil {
		t.Fatal("expected error for missing MEDIA_S3_ACCESS_KEY_ID in production, got nil")
	}

	// 3. Missing secret -> FAIL
	setValidBaseProdEnv()
	t.Setenv("MEDIA_S3_BUCKET", "prod-media")
	t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "key")
	if _, err := config.Load("test-service"); err == nil {
		t.Fatal("expected error for missing MEDIA_S3_SECRET_ACCESS_KEY in production, got nil")
	}

	// 4. Missing public base URL -> FAIL
	setValidBaseProdEnv()
	t.Setenv("MEDIA_S3_BUCKET", "prod-media")
	t.Setenv("MEDIA_S3_ACCESS_KEY_ID", "key")
	t.Setenv("MEDIA_S3_SECRET_ACCESS_KEY", "secret")
	if _, err := config.Load("test-service"); err == nil {
		t.Fatal("expected error for missing MEDIA_PUBLIC_BASE_URL in production, got nil")
	}

	// 5. Valid production S3 config -> PASS
	setValidBaseProdEnv()
	setValidS3Env()
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected valid production config load, got: %v", err)
	}
	if cfg.Environment != "production" {
		t.Errorf("expected Environment 'production', got %s", cfg.Environment)
	}
}

func TestProductionConfigValidationRequiresAllInternalTokens(t *testing.T) {
	baseConfig := config.Config{
		Environment:            "production",
		DatabaseURL:            "postgres://user:pass@prod-db.internal:5432/commerce?sslmode=require",
		RabbitMQURL:            "amqp://user:pass@prod-mq.internal:5672/",
		ZitadelIssuer:          "https://auth.matjero.com",
		InternalPlatformToken:  "secret-platform",
		InternalSellerToken:    "secret-seller",
		InternalAdminToken:     "secret-admin",
		InternalSupplierToken:  "secret-supplier",
		ThemePreviewSecret:     "secret-theme-preview",
		MediaS3Bucket:          "prod-media",
		MediaS3AccessKeyID:     "key",
		MediaS3SecretAccessKey: "secret",
		MediaPublicBaseURL:     "https://cdn.matjero.com",
	}

	tokenFields := []struct {
		name  string
		clear func(*config.Config)
	}{
		{name: "platform", clear: func(cfg *config.Config) { cfg.InternalPlatformToken = "" }},
		{name: "seller", clear: func(cfg *config.Config) { cfg.InternalSellerToken = "" }},
		{name: "admin", clear: func(cfg *config.Config) { cfg.InternalAdminToken = "" }},
		{name: "supplier", clear: func(cfg *config.Config) { cfg.InternalSupplierToken = "" }},
	}

	for _, tokenField := range tokenFields {
		t.Run(tokenField.name, func(t *testing.T) {
			cfg := baseConfig
			tokenField.clear(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("expected missing %s token to fail production validation", tokenField.name)
			}
		})
	}
}

func TestProductionConfigRejectsLocalValidationProvisioning(t *testing.T) {
	cfg := config.Config{
		Environment:                        "production",
		DatabaseURL:                        "postgres://user:pass@prod-db.internal:5432/commerce?sslmode=require",
		RabbitMQURL:                        "amqp://user:pass@prod-mq.internal:5672/",
		ZitadelIssuer:                      "https://auth.matjero.com",
		InternalPlatformToken:              "secret-platform",
		InternalSellerToken:                "secret-seller",
		InternalAdminToken:                 "secret-admin",
		InternalSupplierToken:              "secret-supplier",
		ThemePreviewSecret:                 "secret-theme-preview",
		MediaS3Bucket:                      "prod-media",
		MediaS3AccessKeyID:                 "key",
		MediaS3SecretAccessKey:             "secret",
		MediaPublicBaseURL:                 "https://cdn.matjero.com",
		LocalValidationProvisioningEnabled: true,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production validation to reject local validation provisioning")
	}
}

func TestDevelopmentConfigWithoutS3(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("expected clean development config load without S3, got: %v", err)
	}
	if cfg.MediaS3Bucket != "" {
		t.Errorf("expected empty default MediaS3Bucket in dev, got %q", cfg.MediaS3Bucket)
	}
}

// The AI vendor settings are per-branch, server-only, and have no defaults:
// a missing provider or model must not invent one at load time; numeric and
// duration settings degrade to their safe defaults with a warning instead of
// failing boot.
func TestLoadAuthoringSettingsDefaultsAndTolerance(t *testing.T) {
	clearEnv := []string{
		"AI_AUTHORING_MAX_IMAGES", "AI_AUTHORING_CANDIDATE_RETENTION", "AI_AUTHORING_MAX_RETRIES", "AI_REQUEST_TIMEOUT",
		"AI_TEXT_PROVIDER", "AI_TEXT_MODEL", "AI_TEXT_API_BASE_URL", "AI_TEXT_API_KEY",
		"AI_IMAGE_PROVIDER", "AI_IMAGE_MODEL", "AI_IMAGE_API_BASE_URL", "AI_IMAGE_API_KEY",
	}
	previous := make(map[string]string, len(clearEnv))
	for _, key := range clearEnv {
		previous[key] = os.Getenv(key)
		_ = os.Unsetenv(key)
	}
	defer func() {
		for key, value := range previous {
			_ = os.Setenv(key, value)
		}
	}()

	cfg, err := config.Load("test-service")
	if err != nil {
		t.Fatalf("missing AI settings must not fail boot: %v", err)
	}
	if cfg.AIAuthoringMaxImages != 7 || cfg.AIAuthoringCandidateRetention != 168*time.Hour || cfg.AIAuthoringMaxRetries != 3 || cfg.AIRequestTimeout != 30*time.Second {
		t.Fatalf("authoring defaults = %d/%s/%d/%s", cfg.AIAuthoringMaxImages, cfg.AIAuthoringCandidateRetention, cfg.AIAuthoringMaxRetries, cfg.AIRequestTimeout)
	}
	// No provider or model default exists: the fields stay empty and choosing
	// an adapter is an explicit operator decision only.
	if cfg.AITextProvider != "" || cfg.AITextModel != "" || cfg.AITextAPIBaseURL != "" || cfg.AITextAPIKey != "" {
		t.Fatalf("text defaults = %q/%q/%q/%q, want all empty", cfg.AITextProvider, cfg.AITextModel, cfg.AITextAPIBaseURL, cfg.AITextAPIKey)
	}
	if cfg.AIImageProvider != "" || cfg.AIImageModel != "" || cfg.AIImageAPIBaseURL != "" || cfg.AIImageAPIKey != "" {
		t.Fatalf("image defaults = %q/%q/%q/%q, want all empty", cfg.AIImageProvider, cfg.AIImageModel, cfg.AIImageAPIBaseURL, cfg.AIImageAPIKey)
	}

	// Custom values propagate; per-branch model/provider/data are independent.
	t.Setenv("AI_AUTHORING_MAX_IMAGES", "2")
	t.Setenv("AI_AUTHORING_CANDIDATE_RETENTION", "24h")
	t.Setenv("AI_AUTHORING_MAX_RETRIES", "5")
	t.Setenv("AI_REQUEST_TIMEOUT", "90s")
	t.Setenv("AI_TEXT_PROVIDER", "openai")
	t.Setenv("AI_TEXT_MODEL", "gpt-4o-mini")
	t.Setenv("AI_IMAGE_PROVIDER", "openai")
	t.Setenv("AI_IMAGE_MODEL", "gpt-image-1")
	cfg, err = config.Load("test-service")
	if err != nil {
		t.Fatalf("custom AI settings load: %v", err)
	}
	if cfg.AIAuthoringMaxImages != 2 || cfg.AIAuthoringCandidateRetention != 24*time.Hour || cfg.AIAuthoringMaxRetries != 5 || cfg.AIRequestTimeout != 90*time.Second {
		t.Fatalf("custom authoring values = %d/%s/%d/%s", cfg.AIAuthoringMaxImages, cfg.AIAuthoringCandidateRetention, cfg.AIAuthoringMaxRetries, cfg.AIRequestTimeout)
	}
	if cfg.AITextModel != "gpt-4o-mini" || cfg.AIImageModel != "gpt-image-1" {
		t.Fatalf("per-branch models = %q/%q", cfg.AITextModel, cfg.AIImageModel)
	}

	// Invalid numeric values degrade to the safe default with a logged
	// warning instead of failing boot; and the hard ceiling of 7 still holds
	// for any larger configured value.
	for _, invalid := range []string{"abc", "0", "-3"} {
		t.Setenv("AI_AUTHORING_MAX_IMAGES", invalid)
		cfg, err = config.Load("test-service")
		if err != nil {
			t.Fatalf("invalid %q must not fail boot: %v", invalid, err)
		}
		if cfg.AIAuthoringMaxImages != 7 {
			t.Fatalf("invalid %q limit = %d, want the safe default 7", invalid, cfg.AIAuthoringMaxImages)
		}
	}
	t.Setenv("AI_AUTHORING_MAX_IMAGES", "10")
	cfg, err = config.Load("test-service")
	if err != nil {
		t.Fatalf("config load for ceiling case: %v", err)
	}
	if cfg.AIAuthoringMaxImages != 7 {
		t.Fatalf("AI_AUTHORING_MAX_IMAGES=10 yielded %d, want the ceiling 7", cfg.AIAuthoringMaxImages)
	}
	// Invalid durations degrade to their defaults too.
	t.Setenv("AI_AUTHORING_MAX_IMAGES", "2")
	t.Setenv("AI_AUTHORING_CANDIDATE_RETENTION", "abc")
	cfg, err = config.Load("test-service")
	if err != nil {
		t.Fatalf("invalid retention must not fail boot: %v", err)
	}
	if cfg.AIAuthoringCandidateRetention != 168*time.Hour {
		t.Fatalf("invalid retention resolved to %s, want 168h", cfg.AIAuthoringCandidateRetention)
	}
}
