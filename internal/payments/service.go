package payments

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pkgEvents "github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

type Service struct {
	repo        Repository
	outboxStore outbox.Store
}

func NewService(repo Repository) Service {
	return Service{
		repo:        repo,
		outboxStore: outbox.NewStore(),
	}
}

func (s Service) InitializePayment(ctx context.Context, params InitializePaymentParams) (*Payment, error) {
	if strings.TrimSpace(params.OrderID) == "" {
		return nil, fmt.Errorf("%w: order_id is required", ErrInvalidInput)
	}
	if params.AmountMinor < 0 {
		return nil, fmt.Errorf("%w: amount_minor must be non-negative", ErrInvalidInput)
	}
	if strings.TrimSpace(params.Currency) == "" {
		return nil, fmt.Errorf("%w: currency is required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.PaymentMethod) == "" {
		return nil, fmt.Errorf("%w: payment_method is required", ErrInvalidInput)
	}

	now := time.Now().UTC()
	paymentID := uuid.NewString()
	initialStatus := StatusCreated

	payment := Payment{
		ID:            paymentID,
		OrderID:       strings.TrimSpace(params.OrderID),
		AmountMinor:   params.AmountMinor,
		Currency:      strings.ToUpper(strings.TrimSpace(params.Currency)),
		PaymentMethod: strings.TrimSpace(params.PaymentMethod),
		Status:        initialStatus,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	var attempt *PaymentAttempt
	if strings.TrimSpace(params.Provider) != "" {
		attempt = &PaymentAttempt{
			ID:                uuid.NewString(),
			PaymentID:         paymentID,
			Provider:          strings.TrimSpace(params.Provider),
			ProviderReference: strings.TrimSpace(params.ProviderReference),
			Status:            string(initialStatus),
			CreatedAt:         now,
			UpdatedAt:         now,
		}
	}

	err := s.repo.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.CreatePaymentTx(ctx, tx, payment, attempt)
	})
	if err != nil {
		return nil, err
	}

	if attempt != nil {
		payment.Attempts = []PaymentAttempt{*attempt}
	}
	return &payment, nil
}

func (s Service) UpdatePaymentStatus(ctx context.Context, params UpdateStatusParams) (*Payment, error) {
	if strings.TrimSpace(params.PaymentID) == "" {
		return nil, fmt.Errorf("%w: payment_id is required", ErrInvalidInput)
	}
	if !params.NewStatus.Valid() {
		return nil, fmt.Errorf("%w: invalid target status '%s'", ErrInvalidStatus, params.NewStatus)
	}

	now := time.Now().UTC()

	var (
		updatedPayment *Payment
		attempt        *PaymentAttempt
	)

	if strings.TrimSpace(params.Provider) != "" {
		attempt = &PaymentAttempt{
			ID:                uuid.NewString(),
			PaymentID:         strings.TrimSpace(params.PaymentID),
			Provider:          strings.TrimSpace(params.Provider),
			ProviderReference: strings.TrimSpace(params.ProviderReference),
			Status:            string(params.NewStatus),
			ErrorMessage:      strings.TrimSpace(params.ErrorMessage),
			CreatedAt:         now,
			UpdatedAt:         now,
		}
	}

	err := s.repo.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		updatedPayment, _, err = s.repo.UpdatePaymentStatusTx(
			ctx, tx, params.PaymentID, params.NewStatus, attempt, now,
		)
		if err != nil {
			return err
		}

		if params.NewStatus == StatusCaptured {
			outboxPayload := pkgEvents.PaymentCapturedPayload{
				PaymentID:         updatedPayment.ID,
				OrderID:           updatedPayment.OrderID,
				AmountMinor:       updatedPayment.AmountMinor,
				Currency:          updatedPayment.Currency,
				PaymentMethod:     updatedPayment.PaymentMethod,
				Provider:          params.Provider,
				ProviderReference: params.ProviderReference,
				CapturedAt:        now,
			}
			eventEnv, err := pkgEvents.NewPaymentCapturedEvent(outboxPayload, params.CorrelationID, params.CausationID)
			if err != nil {
				return fmt.Errorf("build payment captured event: %w", err)
			}
			if err := s.outboxStore.Enqueue(ctx, tx, eventEnv); err != nil {
				return fmt.Errorf("enqueue outbox payment captured event: %w", err)
			}
		} else if params.NewStatus == StatusFailed {
			outboxPayload := pkgEvents.PaymentFailedPayload{
				PaymentID:     updatedPayment.ID,
				OrderID:       updatedPayment.OrderID,
				AmountMinor:   updatedPayment.AmountMinor,
				Currency:      updatedPayment.Currency,
				PaymentMethod: updatedPayment.PaymentMethod,
				Provider:      params.Provider,
				ErrorMessage:  params.ErrorMessage,
				FailedAt:      now,
			}
			eventEnv, err := pkgEvents.NewPaymentFailedEvent(outboxPayload, params.CorrelationID, params.CausationID)
			if err != nil {
				return fmt.Errorf("build payment failed event: %w", err)
			}
			if err := s.outboxStore.Enqueue(ctx, tx, eventEnv); err != nil {
				return fmt.Errorf("enqueue outbox payment failed event: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	fullPayment, err := s.repo.GetPaymentByID(ctx, nil, updatedPayment.ID)
	if err != nil {
		return updatedPayment, nil
	}
	return fullPayment, nil
}

func (s Service) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	return s.repo.GetPaymentByID(ctx, nil, paymentID)
}

func (s Service) GetPaymentByOrder(ctx context.Context, orderID string) (*Payment, error) {
	return s.repo.GetPaymentByOrderID(ctx, nil, orderID)
}

func (s Service) PersistWebhookInbox(ctx context.Context, params PersistWebhookInboxParams) (*WebhookInbox, bool, error) {
	if strings.TrimSpace(params.Provider) == "" || strings.TrimSpace(params.ProviderEventID) == "" {
		return nil, false, fmt.Errorf("%w: provider and provider_event_id are required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.EventType) == "" {
		return nil, false, fmt.Errorf("%w: event_type is required", ErrInvalidInput)
	}
	if len(params.PayloadJSON) == 0 {
		return nil, false, fmt.Errorf("%w: payload_json is required", ErrInvalidInput)
	}

	now := time.Now().UTC()
	inbox := WebhookInbox{
		ID:                uuid.NewString(),
		Provider:          strings.TrimSpace(params.Provider),
		ConnectionID:      strings.TrimSpace(params.ConnectionID),
		ProviderEventID:   strings.TrimSpace(params.ProviderEventID),
		EventType:         strings.TrimSpace(params.EventType),
		PayloadJSON:       params.PayloadJSON,
		SignatureVerified: params.SignatureVerified,
		Status:            "PENDING",
		AttemptCount:      0,
		ReceivedAt:        now,
	}

	return s.repo.PersistWebhookInbox(ctx, nil, inbox)
}
