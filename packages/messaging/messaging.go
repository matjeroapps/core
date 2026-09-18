package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"core/packages/events"
)

var ErrPublisherUnavailable = errors.New("publisher unavailable")

func IsPublisherUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrPublisherUnavailable) || errors.Is(err, amqp.ErrClosed)
}

type Publisher interface {
	PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error
	PublishMessage(ctx context.Context, exchange, routingKey string, message events.MessageEnvelope) error
}

type ConfirmationWaiter interface {
	WaitContext(ctx context.Context) (bool, error)
}

type RabbitPublisher struct {
	channel     *amqp.Channel
	publishFunc func(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) (ConfirmationWaiter, error)
	waitConfirm func(cw ConfirmationWaiter, ctx context.Context) (bool, error)
}

type rabbitPublication struct {
	exchange      string
	routingKey    string
	messageID     string
	messageType   string
	correlationID string
	payload       any
}

func NewRabbitPublisher(channel *amqp.Channel) (*RabbitPublisher, error) {
	if channel == nil || channel.IsClosed() {
		return nil, fmt.Errorf("%w: rabbitmq channel is required", ErrPublisherUnavailable)
	}
	if err := channel.Confirm(false); err != nil {
		return nil, fmt.Errorf("%w: enable channel confirm mode: %v", ErrPublisherUnavailable, err)
	}
	return &RabbitPublisher{channel: channel}, nil
}

func NewRabbitPublisherWithSeamsForTest(
	channel *amqp.Channel,
	publishFunc func(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) (ConfirmationWaiter, error),
	waitConfirm func(cw ConfirmationWaiter, ctx context.Context) (bool, error),
) *RabbitPublisher {
	return &RabbitPublisher{
		channel:     channel,
		publishFunc: publishFunc,
		waitConfirm: waitConfirm,
	}
}

func (p *RabbitPublisher) PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error {
	if err := event.Validate(); err != nil {
		return err
	}
	return p.publish(ctx, rabbitPublication{
		exchange:      exchange,
		routingKey:    routingKey,
		messageID:     event.EventID,
		messageType:   event.EventType,
		correlationID: event.CorrelationID,
		payload:       event,
	})
}

func (p *RabbitPublisher) PublishMessage(ctx context.Context, exchange, routingKey string, message events.MessageEnvelope) error {
	if err := message.Validate(); err != nil {
		return err
	}
	return p.publish(ctx, rabbitPublication{
		exchange:      exchange,
		routingKey:    routingKey,
		messageID:     message.MessageID,
		messageType:   message.MessageType,
		correlationID: message.CorrelationID,
		payload:       message,
	})
}

func (p *RabbitPublisher) publish(ctx context.Context, publication rabbitPublication) error {
	if p.publishFunc == nil && (p.channel == nil || p.channel.IsClosed()) {
		return fmt.Errorf("%w: rabbitmq channel is closed", ErrPublisherUnavailable)
	}

	body, err := json.Marshal(publication.payload)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	msg := amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     publication.messageID,
		Type:          publication.messageType,
		CorrelationId: publication.correlationID,
		Body:          body,
	}

	var confirmation ConfirmationWaiter
	if p.publishFunc != nil {
		c, err := p.publishFunc(ctx, publication.exchange, publication.routingKey, msg)
		if err != nil {
			return err
		}
		confirmation = c
	} else {
		c, err := p.channel.PublishWithDeferredConfirmWithContext(
			ctx,
			publication.exchange,
			publication.routingKey,
			false,
			false,
			msg,
		)
		if err != nil {
			if errors.Is(err, amqp.ErrClosed) || p.channel.IsClosed() {
				return fmt.Errorf("%w: publish message: %v", ErrPublisherUnavailable, err)
			}
			return fmt.Errorf("publish message: %w", err)
		}
		if c == nil {
			return fmt.Errorf("%w: channel is not in confirm mode", ErrPublisherUnavailable)
		}
		confirmation = c
	}

	waitFn := p.waitConfirm
	if waitFn == nil {
		waitFn = func(cw ConfirmationWaiter, ctx context.Context) (bool, error) {
			return cw.WaitContext(ctx)
		}
	}

	acked, err := waitFn(confirmation, ctx)
	if err != nil {
		if errors.Is(err, amqp.ErrClosed) || (p.channel != nil && p.channel.IsClosed()) {
			return fmt.Errorf("%w: wait publish confirm: %v", ErrPublisherUnavailable, err)
		}
		return fmt.Errorf("wait publish confirm: %w", err)
	}
	if !acked {
		return fmt.Errorf("rabbitmq publish nacked by broker for message_id %s", publication.messageID)
	}

	return nil
}
