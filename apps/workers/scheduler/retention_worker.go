package main

import (
	"context"
	"log/slog"
	"time"

	"core/modules/commerce"
)

type RetentionWorker struct {
	service   commerce.Service
	logger    *slog.Logger
	batch     int
	interval  time.Duration
	retention time.Duration
}

func NewRetentionWorker(service commerce.Service, logger *slog.Logger, batch int, interval, retention time.Duration) *RetentionWorker {
	return &RetentionWorker{service: service, logger: logger, batch: batch, interval: interval, retention: retention}
}

func (w *RetentionWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := w.service.PurgeExpiredAuthoringCandidates(ctx, w.batch, w.retention)
			if err != nil {
				w.logger.Warn("ai authoring retention failed", "error", err)
				continue
			}
			if deleted > 0 {
				w.logger.Info("ai authoring retention purged candidates", "count", deleted)
			}
		}
	}
}
