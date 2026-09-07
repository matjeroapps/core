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

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/internal/coreapi"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/internal/marketplace_finance"
	"github.com/matjeroapps/core/internal/payments"
	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/settlement"
	"github.com/matjeroapps/core/internal/shipping"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/modules/markets"
	"github.com/matjeroapps/core/modules/storefront"
	"github.com/matjeroapps/core/modules/themes"
	"github.com/matjeroapps/core/packages/config"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/httpx"
	"github.com/matjeroapps/core/packages/logging"
	"github.com/matjeroapps/core/packages/observability"
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

	var s3Storage *commerce.S3Storage
	if cfg.MediaS3Bucket != "" {
		s3Storage = commerce.NewS3Storage(commerce.S3Config{
			Endpoint:        cfg.MediaS3Endpoint,
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
		Shipping:           shipping.NewService(shipping.NewRepository(db.Pool)),
		Payments:           payments.NewService(payments.NewRepository(db.Pool)),
		Finance:            finService,
		Balance:            balService,
		Settlement:         settleService,
		MarketplaceFinance: mfService,
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
			serviceauth.CallerSeller:   cfg.InternalSellerToken,
			serviceauth.CallerAdmin:    cfg.InternalAdminToken,
			serviceauth.CallerSupplier: cfg.InternalSupplierToken,
		},
	}
}
