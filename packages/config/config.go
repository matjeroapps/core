package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServiceName               string
	Environment               string
	HTTPAddr                  string
	DatabaseURL               string
	RedisAddr                 string
	RabbitMQURL               string
	ZitadelIssuer             string
	ZitadelAudience           string
	OpenAPIDocsEnabled        bool
	ShutdownTimeout           time.Duration
	CheckoutSessionLifetime   time.Duration
	OrderConfirmationDuration time.Duration

	// PlatformDomain is the base domain under which platform-generated store
	// subdomains are allocated (e.g. "<store-code>.matjero.com"). It is
	// configuration-driven and never hardcoded in application code.
	PlatformDomain string
	// TrustedForwardedHost enables honoring the X-Forwarded-Host header for
	// tenant resolution. It must only be enabled behind an explicitly trusted
	// reverse proxy; otherwise the request Host header is authoritative.
	TrustedForwardedHost bool
	// ReservedSubdomains are subdomain labels that sellers may not claim as a
	// store code (e.g. www, api, admin).
	ReservedSubdomains []string
	// ThemePreviewSecret is the server-side HMAC signing key for short-lived,
	// store-scoped theme draft preview tokens. It must be configuration-driven
	// and is never hardcoded in application code.
	ThemePreviewSecret string

	// Media S3-compatible storage for seller product images.
	// Set MEDIA_S3_BUCKET (plus credentials) to enable presigned uploads.
	// In production the bucket name is required; startup refuses without it.
	MediaS3Endpoint        string
	MediaS3Region          string
	MediaS3Bucket          string
	MediaS3AccessKeyID     string
	MediaS3SecretAccessKey string
	MediaS3ForcePathStyle  bool
	MediaPublicBaseURL     string
	MediaUploadMaxBytes    int64
	MediaPresignTTL        time.Duration

	// Internal service credentials for the Core internal API (ADR-017). Each
	// actor service presents its own bearer token; a caller with no configured
	// token cannot authenticate. These are secrets and must never be committed,
	// logged, or embedded in an image layer.
	InternalSellerToken   string
	InternalAdminToken    string
	InternalSupplierToken string

	OutboxClaimLeaseDuration      time.Duration
	OutboxClaimRenewalMargin      time.Duration
	RabbitMQPublishConfirmTimeout time.Duration
	OutboxBatchSize               int
	OutboxPollInterval            time.Duration
}

func Load(serviceName string) (Config, error) {
	if serviceName == "" {
		return Config{}, fmt.Errorf("service name is required")
	}

	timeoutSeconds, err := intEnv("SHUTDOWN_TIMEOUT_SECONDS", 10)
	if err != nil {
		return Config{}, err
	}
	checkoutSessionLifetime, err := durationEnv("CHECKOUT_SESSION_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	orderConfirmationDuration, err := durationEnv("ORDER_CONFIRMATION_DURATION", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	outboxClaimLeaseDuration, err := durationEnv("OUTBOX_CLAIM_LEASE_DURATION", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	outboxClaimRenewalMargin, err := durationEnv("OUTBOX_CLAIM_RENEWAL_MARGIN", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	rabbitMQPublishConfirmTimeout, err := durationEnv("RABBITMQ_PUBLISH_CONFIRM_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	outboxBatchSize, err := intEnv("OUTBOX_BATCH_SIZE", 50)
	if err != nil {
		return Config{}, err
	}
	outboxPollInterval, err := durationEnv("OUTBOX_POLL_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}

	if outboxClaimLeaseDuration <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_CLAIM_LEASE_DURATION must be greater than zero")
	}
	if outboxClaimRenewalMargin <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_CLAIM_RENEWAL_MARGIN must be greater than zero")
	}
	if outboxClaimRenewalMargin >= outboxClaimLeaseDuration {
		return Config{}, fmt.Errorf("OUTBOX_CLAIM_RENEWAL_MARGIN must be less than OUTBOX_CLAIM_LEASE_DURATION")
	}
	if rabbitMQPublishConfirmTimeout <= 0 {
		return Config{}, fmt.Errorf("RABBITMQ_PUBLISH_CONFIRM_TIMEOUT must be greater than zero")
	}
	if rabbitMQPublishConfirmTimeout >= outboxClaimLeaseDuration {
		return Config{}, fmt.Errorf("RABBITMQ_PUBLISH_CONFIRM_TIMEOUT must be less than OUTBOX_CLAIM_LEASE_DURATION")
	}
	if outboxBatchSize <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_BATCH_SIZE must be greater than zero")
	}
	if outboxPollInterval <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_POLL_INTERVAL must be greater than zero")
	}

	mediaUploadMaxBytes, err := int64Env("MEDIA_UPLOAD_MAX_BYTES", 10*1024*1024)
	if err != nil {
		return Config{}, err
	}
	mediaPresignTTL, err := durationEnv("MEDIA_PRESIGN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ServiceName:               serviceName,
		Environment:               stringEnv("APP_ENV", "development"),
		HTTPAddr:                  stringEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:               stringEnv("DATABASE_URL", "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"),
		RedisAddr:                 stringEnv("REDIS_ADDR", "localhost:6379"),
		RabbitMQURL:               stringEnv("RABBITMQ_URL", "amqp://commerce:commerce@localhost:5672/"),
		ZitadelIssuer:             stringEnv("ZITADEL_ISSUER", "http://localhost:8081"),
		ZitadelAudience:           stringEnv("ZITADEL_AUDIENCE", serviceName),
		OpenAPIDocsEnabled:        boolEnv("OPENAPI_DOCS_ENABLED", stringEnv("APP_ENV", "development") != "production"),
		ShutdownTimeout:           time.Duration(timeoutSeconds) * time.Second,
		CheckoutSessionLifetime:   checkoutSessionLifetime,
		OrderConfirmationDuration: orderConfirmationDuration,
		PlatformDomain:            stringEnv("PLATFORM_DOMAIN", "matjero.com"),
		TrustedForwardedHost:      boolEnv("TRUSTED_FORWARDED_HOST", false),
		ReservedSubdomains:        stringSliceEnv("RESERVED_SUBDOMAINS", []string{"www", "api", "admin", "app", "cdn", "mail", "seller", "supplier", "static", "assets"}),
		ThemePreviewSecret:        stringEnv("THEME_PREVIEW_SECRET", ""),
		MediaS3Endpoint:           stringEnv("MEDIA_S3_ENDPOINT", ""),
		MediaS3Region:             stringEnv("MEDIA_S3_REGION", "us-east-1"),
		MediaS3Bucket:             stringEnv("MEDIA_S3_BUCKET", ""),
		MediaS3AccessKeyID:        stringEnv("MEDIA_S3_ACCESS_KEY_ID", ""),
		MediaS3SecretAccessKey:    stringEnv("MEDIA_S3_SECRET_ACCESS_KEY", ""),
		MediaS3ForcePathStyle:     boolEnv("MEDIA_S3_FORCE_PATH_STYLE", false),
		MediaPublicBaseURL:        stringEnv("MEDIA_PUBLIC_BASE_URL", ""),
		MediaUploadMaxBytes:       mediaUploadMaxBytes,
		MediaPresignTTL:           mediaPresignTTL,

		InternalSellerToken:   stringEnv("CORE_INTERNAL_SELLER_TOKEN", ""),
		InternalAdminToken:    stringEnv("CORE_INTERNAL_ADMIN_TOKEN", ""),
		InternalSupplierToken: stringEnv("CORE_INTERNAL_SUPPLIER_TOKEN", ""),

		OutboxClaimLeaseDuration:      outboxClaimLeaseDuration,
		OutboxClaimRenewalMargin:      outboxClaimRenewalMargin,
		RabbitMQPublishConfirmTimeout: rabbitMQPublishConfirmTimeout,
		OutboxBatchSize:               outboxBatchSize,
		OutboxPollInterval:            outboxPollInterval,
	}, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return parsed, nil
}

// stringSliceEnv reads a comma-separated environment variable, trimming
// whitespace from each entry and dropping empty values.
func stringSliceEnv(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func stringEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func intEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}

	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
