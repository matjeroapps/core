package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"core/packages/config"
	"core/packages/logging"
	"core/packages/messaging"
	"core/packages/observability"
	"core/packages/outbox"
)

type RabbitSetupFunc func(rabbitURL string) (*amqp.Connection, *amqp.Channel, messaging.Publisher, error)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load("general-worker")
	if err != nil {
		return err
	}
	logger := logging.New(cfg)
	shutdown, err := observability.Init(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = shutdown(context.Background()) }()

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db pool: %w", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping db pool: %w", err)
	}

	logger.Info("worker connected to postgres", slog.String("service", cfg.ServiceName))

	return runWorkerLoop(ctx, cfg, dbPool, logger, defaultSetupRabbit, 1*time.Second, waitContext)
}

func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type WaitFunc func(ctx context.Context, d time.Duration) error

func runWorkerLoop(ctx context.Context, cfg config.Config, dbPool outbox.DBExecutor, logger *slog.Logger, setupFn RabbitSetupFunc, initialBackoff time.Duration, waitFn WaitFunc) error {
	if initialBackoff <= 0 {
		initialBackoff = 1 * time.Second
	}
	if waitFn == nil {
		waitFn = waitContext
	}
	backoff := initialBackoff
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker shutting down")
			return nil
		default:
		}

		conn, ch, pub, err := setupFn(cfg.RabbitMQURL)
		if err != nil {
			logger.Error("rabbitmq connection failed, retrying...", slog.String("error", err.Error()), slog.Duration("backoff", backoff))
			if waitErr := waitFn(ctx, backoff); waitErr != nil {
				return nil
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}

		logger.Info("worker connected to rabbitmq and confirmed topology")

		proc := outbox.NewProcessor(cfg, dbPool, pub, logger)
		err = proc.Run(ctx)
		if ch != nil {
			_ = ch.Close()
		}
		if conn != nil {
			_ = conn.Close()
		}

		if errors.Is(ctx.Err(), context.Canceled) {
			logger.Info("worker shutdown complete")
			return nil
		}

		if err != nil {
			logger.Warn("processor session stopped, reconnecting...", slog.String("error", err.Error()), slog.Duration("backoff", backoff))
			if waitErr := waitFn(ctx, backoff); waitErr != nil {
				return nil
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
		} else {
			backoff = initialBackoff
		}
	}
}

func defaultSetupRabbit(rabbitURL string) (*amqp.Connection, *amqp.Channel, messaging.Publisher, error) {
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("open rabbitmq channel: %w", err)
	}

	err = ch.ExchangeDeclare(
		"commerce.events", // name
		"topic",           // type
		true,              // durable
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("declare exchange commerce.events: %w", err)
	}

	pub, err := messaging.NewRabbitPublisher(ch)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("new rabbit publisher: %w", err)
	}

	return conn, ch, pub, nil
}
