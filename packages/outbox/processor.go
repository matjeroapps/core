package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"core/packages/config"
	"core/packages/events"
	"core/packages/messaging"
)

var newClaimUUID = func() (uuid.UUID, error) {
	return uuid.NewRandom()
}

func SetNewClaimUUIDForTest(fn func() (uuid.UUID, error)) {
	newClaimUUID = fn
}

func ResetNewClaimUUIDForTest() {
	newClaimUUID = func() (uuid.UUID, error) {
		return uuid.NewRandom()
	}
}

var (
	testHookAfterClaimBatch      func(claimID string, eventIDs []string)
	testHookBeforeNextBatchEvent func(claimID string, eventID string, remainingIDs []string)
)

func SetTestHookAfterClaimBatchForTest(fn func(claimID string, eventIDs []string)) {
	testHookAfterClaimBatch = fn
}

func ResetTestHookAfterClaimBatchForTest() {
	testHookAfterClaimBatch = nil
}

func SetTestHookBeforeNextBatchEventForTest(fn func(claimID string, eventID string, remainingIDs []string)) {
	testHookBeforeNextBatchEvent = fn
}

func ResetTestHookBeforeNextBatchEventForTest() {
	testHookBeforeNextBatchEvent = nil
}

type EventPublisher interface {
	PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error
}

type Processor struct {
	cfg       config.Config
	db        DBExecutor
	publisher EventPublisher
	logger    *slog.Logger
	store     Store
}

func NewProcessor(cfg config.Config, db DBExecutor, publisher EventPublisher, logger *slog.Logger) *Processor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		cfg:       cfg,
		db:        db,
		publisher: publisher,
		logger:    logger,
		store:     NewStore(),
	}
}

// Run executes the outbox processing loop until ctx is canceled or a transport failure occurs.
func (p *Processor) Run(ctx context.Context) error {
	p.logger.Info("outbox processor loop started",
		slog.Duration("poll_interval", p.cfg.OutboxPollInterval),
		slog.Duration("lease_duration", p.cfg.OutboxClaimLeaseDuration),
		slog.Int("batch_size", p.cfg.OutboxBatchSize),
	)

	ticker := time.NewTicker(p.cfg.OutboxPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("outbox processor loop stopping")
			return nil
		default:
		}

		processedCount, err := p.ProcessBatch(ctx)
		if err != nil {
			if messaging.IsPublisherUnavailable(err) {
				return err
			}
			if !errors.Is(err, context.Canceled) {
				p.logger.Error("outbox batch processing error", slog.String("error", err.Error()))
			}
		}

		if processedCount == 0 || err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}

func (p *Processor) ProcessBatch(ctx context.Context) (int, error) {
	claimUUID, err := newClaimUUID()
	if err != nil {
		return 0, fmt.Errorf("generate claim uuid: %w", err)
	}
	claimID := claimUUID.String()

	claimedIDs, err := p.store.ClaimBatch(ctx, p.db, claimID, p.cfg.OutboxBatchSize, p.cfg.OutboxClaimLeaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim batch: %w", err)
	}
	if len(claimedIDs) == 0 {
		return 0, nil
	}

	if testHookAfterClaimBatch != nil {
		testHookAfterClaimBatch(claimID, claimedIDs)
	}

	p.logger.Info("claimed outbox batch",
		slog.String("claim_id", claimID),
		slog.Int("batch_size", len(claimedIDs)),
	)

	var transportErr error

	for i, eventID := range claimedIDs {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		remainingIDs := claimedIDs[i:]
		if err := p.store.RenewBatchNearExpiry(
			ctx,
			p.db,
			remainingIDs,
			claimID,
			p.cfg.OutboxClaimLeaseDuration,
			p.cfg.OutboxClaimRenewalMargin,
		); err != nil {
			return 0, fmt.Errorf("renew batch near expiry: %w", err)
		}

		if testHookBeforeNextBatchEvent != nil {
			testHookBeforeNextBatchEvent(claimID, eventID, remainingIDs)
		}

		envelope, err := p.store.RenewAndLoadEvent(ctx, p.db, eventID, claimID, p.cfg.OutboxClaimLeaseDuration)
		if err != nil {
			if errors.Is(err, ErrClaimLost) {
				p.logger.Warn("outbox claim lost before publish",
					slog.String("event_id", eventID),
					slog.String("claim_id", claimID),
				)
				continue
			}
			if errors.Is(err, ErrInvalidEnvelope) {
				p.logger.Error("malformed outbox event envelope, scheduling backoff",
					slog.String("event_id", eventID),
					slog.String("error", err.Error()),
				)
				if _, relErr := p.store.ReleaseWithBackoff(ctx, p.db, eventID, claimID); relErr != nil {
					return 0, fmt.Errorf("release malformed outbox event with backoff: %w", relErr)
				}
				continue
			}
			return 0, fmt.Errorf("renew and load outbox event: %w", err)
		}

		exchange, routingKey, err := ResolveRoutingKey(envelope.EventType)
		if err != nil {
			p.logger.Error("failed to resolve routing key",
				slog.String("event_id", eventID),
				slog.String("event_type", envelope.EventType),
				slog.String("error", err.Error()),
			)
			if _, relErr := p.store.ReleaseWithBackoff(ctx, p.db, eventID, claimID); relErr != nil {
				return 0, fmt.Errorf("release unknown routing outbox event with backoff: %w", relErr)
			}
			continue
		}

		pubCtx, cancel := context.WithTimeout(ctx, p.cfg.RabbitMQPublishConfirmTimeout)
		pubErr := p.publisher.PublishEvent(pubCtx, exchange, routingKey, *envelope)
		cancel()

		if pubErr != nil {
			p.logger.Error("outbox publish failed",
				slog.String("event_id", eventID),
				slog.String("event_type", envelope.EventType),
				slog.String("error", pubErr.Error()),
			)
			released, relErr := p.store.ReleaseWithBackoff(ctx, p.db, eventID, claimID)
			if relErr != nil {
				return 0, fmt.Errorf("release outbox event with backoff: %w", relErr)
			} else if !released {
				p.logger.Warn("stale release with backoff (claim lost)",
					slog.String("event_id", eventID),
					slog.String("claim_id", claimID),
				)
			}

			if messaging.IsPublisherUnavailable(pubErr) {
				transportErr = fmt.Errorf("%w: %v", messaging.ErrPublisherUnavailable, pubErr)
				break
			}
			continue
		}

		marked, markErr := p.store.MarkPublished(ctx, p.db, eventID, claimID)
		if markErr != nil {
			return 0, fmt.Errorf("mark outbox event published: %w", markErr)
		} else if !marked {
			p.logger.Warn("stale mark published (claim lost or already published)",
				slog.String("event_id", eventID),
				slog.String("claim_id", claimID),
			)
		} else {
			p.logger.Info("outbox event published",
				slog.String("event_id", eventID),
				slog.String("event_type", envelope.EventType),
			)
		}
	}

	if transportErr != nil {
		return len(claimedIDs), transportErr
	}

	return len(claimedIDs), nil
}
