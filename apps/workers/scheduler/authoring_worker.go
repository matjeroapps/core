package main

import (
	"context"
	"log/slog"
	"time"

	"core/modules/commerce"
)

type AuthoringWorker struct {
	service  commerce.Service
	logger   *slog.Logger
	batch    int
	interval time.Duration
}

func NewAuthoringWorker(service commerce.Service, logger *slog.Logger, batch int, interval time.Duration) *AuthoringWorker {
	return &AuthoringWorker{service: service, logger: logger, batch: batch, interval: interval}
}

func (w *AuthoringWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *AuthoringWorker) process(ctx context.Context) {
	jobs, err := w.service.ClaimPendingAuthoringJobs(ctx, w.batch)
	if err != nil {
		w.logger.Warn("ai authoring claim failed", "error", err)
		return
	}
	for _, job := range jobs {
		if err := w.service.ProcessAuthoringJob(ctx, job); err != nil {
			w.logger.Warn("ai authoring job failed", "job_id", job.ID, "store_id", job.StoreID, "error", err)
		}
	}
}
