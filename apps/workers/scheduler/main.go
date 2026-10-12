package main

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"core/modules/commerce"
	"core/packages/config"
	"core/packages/database"
	"core/packages/logging"
	"core/packages/observability"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load("scheduler")
	if err != nil {
		return err
	}
	logger := logging.New(cfg)
	shutdown, err := observability.Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = shutdown(context.Background()) }()

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	repo := commerce.NewRepository(db.Pool)
	service := commerce.NewService(repo)
	service.AuthoringLimitResolver = commerce.NewConfigImageLimitResolver(cfg.AIAuthoringMaxImages)
	service.AuthoringMaxRetries = cfg.AIAuthoringMaxRetries
	service.AuthoringProviders = commerce.NewAuthoringProviderRegistry(commerce.AuthoringProviderConfig{
		TextProvider: cfg.AITextProvider, TextModel: cfg.AITextModel, TextAPIBaseURL: cfg.AITextAPIBaseURL, TextAPIKey: cfg.AITextAPIKey,
		ImageProvider: cfg.AIImageProvider, ImageModel: cfg.AIImageModel, ImageAPIBaseURL: cfg.AIImageAPIBaseURL, ImageAPIKey: cfg.AIImageAPIKey,
		RequestTimeout: cfg.AIRequestTimeout,
	})

	expiryWorker := NewExpiryWorker(service, logger, 100, 5*time.Second)
	go expiryWorker.Run(ctx)
	authoringWorker := NewAuthoringWorker(service, logger, 10, 5*time.Second)
	go authoringWorker.Run(ctx)
	retentionWorker := NewRetentionWorker(service, logger, 20, time.Hour, cfg.AIAuthoringCandidateRetention)
	go retentionWorker.Run(ctx)

	logger.Info("scheduler ready", slog.String("service", cfg.ServiceName))
	<-ctx.Done()
	logger.Info("scheduler shutting down")
	return nil
}
