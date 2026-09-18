package main

import (
	"context"
	"log/slog"
	"time"

	"core/modules/commerce"
)

type ExpiryWorker struct {
	service   commerce.Service
	logger    *slog.Logger
	batchSize int
	interval  time.Duration
}

func NewExpiryWorker(service commerce.Service, logger *slog.Logger, batchSize int, interval time.Duration) *ExpiryWorker {
	if batchSize <= 0 {
		batchSize = 100
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &ExpiryWorker{
		service:   service,
		logger:    logger,
		batchSize: batchSize,
		interval:  interval,
	}
}

func (w *ExpiryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *ExpiryWorker) processBatch(ctx context.Context) {
	candidates, err := w.service.DiscoverExpiryCandidates(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("failed to discover expiry candidates", slog.String("error", err.Error()))
		return
	}

	for _, orderID := range candidates {
		if ctx.Err() != nil {
			return
		}
		_, err := w.service.ExpirePendingOrder(ctx, orderID)
		if err != nil {
			w.logger.Error("failed to expire pending order", slog.String("order_id", orderID), slog.String("error", err.Error()))
		}
	}
}
