// Command core-api serves the Core internal HTTP API.
//
// This is the runtime business capability boundary introduced by ADR-017. Actor
// repositories call it over HTTP instead of importing Core Go packages, so each
// Matjero repository stays independently buildable and deployable.
//
// The API is internal: it listens on a private service network address, is never
// exposed through the public storefront domain, and has no browser CORS. Every
// /internal/v1 request must present a per-caller service token.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"core/internal/balance"
	"core/internal/coreapi"
	"core/internal/finance"
	"core/internal/integration"
	"core/internal/inventory"
	"core/internal/listings"
	"core/internal/marketplace"
	"core/internal/marketplace_finance"
	"core/internal/merchants"
	"core/internal/payments"
	"core/internal/serviceauth"
	"core/internal/settlement"
	"core/internal/shipping"
	"core/modules/commerce"
	"core/modules/markets"
	"core/modules/storefront"
	"core/modules/themes"
	"core/packages/config"
	"core/packages/database"
	"core/packages/httpx"
	"core/packages/logging"
	"core/packages/observability"
)

// ErrNoServiceCredentials is returned when the internal API would start without
// any configured caller token. Serving in that state would expose every Core
// business capability to unauthenticated callers on the service network, so the
// process refuses to start instead.
var ErrNoServiceCredentials = errors.New("no internal service credentials configured")

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load("core-api")
	if err != nil {
		return err
	}

	logger := logging.New(cfg)
	shutdown, err := observability.Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = shutdown(context.Background()) }()

	authCfg := serviceAuthConfig(cfg)
	if !authCfg.Enabled() {
		return ErrNoServiceCredentials
	}
	// Log which callers are configured, never the token values.
	logger.Info("internal service credentials loaded",
		"callers", fmt.Sprint(authCfg.CallersWithTokens()),
	)

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	repo := commerce.NewRepository(db.Pool)
	repo.OrderConfirmationDuration = cfg.OrderConfirmationDuration
	service := commerce.NewService(repo)
	service.CheckoutSessionLifetime = cfg.CheckoutSessionLifetime
	service.OrderConfirmationDuration = cfg.OrderConfirmationDuration
	service.PlatformDomain = cfg.PlatformDomain
	service.ReservedSubdomains = cfg.ReservedSubdomains
	service.StoreEntitlement = commerce.NewStoreEntitlementPolicy(cfg.StoreDefaultMaxActiveStores)

	var s3Storage *commerce.S3Storage
	if cfg.MediaS3Bucket != "" {
		s3Storage = commerce.NewS3Storage(commerce.S3Config{
			Endpoint:        cfg.MediaS3Endpoint,
			PresignEndpoint: cfg.MediaS3PresignEndpoint,
			Region:          cfg.MediaS3Region,
			Bucket:          cfg.MediaS3Bucket,
			AccessKeyID:     cfg.MediaS3AccessKeyID,
			SecretAccessKey: cfg.MediaS3SecretAccessKey,
			ForcePathStyle:  cfg.MediaS3ForcePathStyle,
			PublicBaseURL:   cfg.MediaPublicBaseURL,
			MaxBytes:        cfg.MediaUploadMaxBytes,
			URLTTL:          cfg.MediaPresignTTL,
		})
		logger.Info("media S3 storage configured", "bucket", cfg.MediaS3Bucket)
	} else if cfg.Environment == "production" {
		return fmt.Errorf("MEDIA_S3_BUCKET is required in production")
	} else {
		logger.Info("media S3 storage not configured: presigned uploads disabled")
	}
	service.S3Storage = s3Storage

	resolver := storefront.NewStoreResolver(repo)

	finService := finance.NewService(finance.NewRepository(db.Pool))
	balService := balance.NewService(balance.NewRepository(db.Pool))
	settleService := settlement.NewService(settlement.NewRepository(db.Pool), balService)
	mfService := marketplace_finance.NewService(marketplace_finance.NewRepository(db.Pool))
	merchantService := merchants.NewService(merchants.NewPostgresRepository(db.Pool))
	merchantValidationProvisioner := merchants.NewValidationProvisioner(db.Pool, cfg.LocalValidationProvisioningEnabled, cfg.PlatformDomain)
	merchantAuthorizer := merchants.NewAuthorizer(merchants.NewPostgresRepository(db.Pool), merchants.AuthModeShadow)
	merchantIntegrationRepo := integration.NewMerchantRepository(db.Pool)
	merchantIntegrationService := integration.NewMerchantService(merchantIntegrationRepo, db.Pool)

	deps := coreapi.Dependencies{
		Commerce:  service,
		Repo:      repo,
		Markets:   markets.NewService(markets.NewRepository(db.Pool)),
		Catalog:   storefront.NewCatalogRepository(db.Pool),
		Stores:    resolver,
		Revisions: storefront.NewRevisionReader(resolver, repo),
		Themes: themes.NewService(themes.NewRepository(db.Pool), repo, themes.Options{
			PreviewSecret: []byte(cfg.ThemePreviewSecret),
		}),
		Shipping:            shipping.NewService(shipping.NewRepository(db.Pool)),
		Payments:            payments.NewService(payments.NewRepository(db.Pool)),
		Finance:             finService,
		Balance:             balService,
		Settlement:          settleService,
		MarketplaceFinance:  mfService,
		Inventory:           inventory.NewService(inventory.NewRepository(db.Pool)),
		Listings:            listings.NewService(listings.NewPostgresRepository(db.Pool)),
		Merchants:           merchantService,
		MerchantBootstrap:   merchants.NewBootstrapService(db.Pool, merchants.NewPostgresRepository(db.Pool)),
		MerchantValidation:  merchantValidationProvisioner,
		MerchantIntegration: merchantIntegrationService,
		MerchantAuthorizer:  merchantAuthorizer,
		SupplyIntegration:   integration.NewSupplyService(db.Pool),
		Marketplace:         marketplace.NewService(marketplace.NewRepository(db.Pool)),
		Integration:         integration.NewService(integration.NewRepository(db.Pool)),
	}

	appCfg := httpx.ConfigFrom(cfg)
	router := httpx.NewRouter(httpx.App{
		Config: appCfg,
		Logger: logger,
		Ready: func(ctx context.Context) error {
			return db.Ping(ctx)
		},
	})

	// Service authentication wraps the internal router. Operational health
	// endpoints are registered on the parent router before this mount, so they
	// stay exempt and an orchestrator can probe them without holding a service
	// credential.
	router.Mount("/", serviceauth.Middleware(authCfg)(coreapi.NewRouter(deps)))

	return httpx.Run(ctx, appCfg, logger, router)
}

// serviceAuthConfig maps the configured caller tokens onto the service-auth
// contract. A caller with an empty token is simply absent from the map, which
// makes it unauthenticatable.
func serviceAuthConfig(cfg config.Config) serviceauth.Config {
	return serviceauth.Config{
		Tokens: map[serviceauth.Caller]string{
			serviceauth.CallerPlatform: cfg.InternalPlatformToken,
			serviceauth.CallerSeller:   cfg.InternalSellerToken,
			serviceauth.CallerAdmin:    cfg.InternalAdminToken,
			serviceauth.CallerSupplier: cfg.InternalSupplierToken,
		},
	}
}
