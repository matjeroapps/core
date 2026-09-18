package messaging_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"core/packages/events"
	"core/packages/messaging"
)

type mockConfirmWaiter struct {
	acked bool
	err   error
}

func (m mockConfirmWaiter) WaitContext(ctx context.Context) (bool, error) {
	return m.acked, m.err
}

func sampleEnvelope() events.EventEnvelope {
	return events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        "commerce.order.created.v1",
		SchemaVersion:    1,
		AggregateType:    "order",
		AggregateID:      "ord_123",
		AggregateVersion: 1,
		CorrelationID:    "corr_abc_999",
		OccurredAt:       time.Now().UTC(),
		Payload: map[string]any{
			"order_id": "ord_123",
		},
	}
}

func TestRabbitPublisherNilChannelFails(t *testing.T) {
	_, err := messaging.NewRabbitPublisher(nil)
	if err == nil {
		t.Fatal("expected error when channel is nil, got nil")
	}
}

func TestRabbitPublisherNACKHandling(t *testing.T) {
	pub := messaging.NewRabbitPublisherWithSeamsForTest(
		nil,
		func(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) (messaging.ConfirmationWaiter, error) {
			return mockConfirmWaiter{acked: false, err: nil}, nil
		},
		nil,
	)

	env := sampleEnvelope()
	err := pub.PublishEvent(context.Background(), "commerce.events", "order.created", env)
	if err == nil {
		t.Fatal("expected NACK error, got nil")
	}
	if messaging.IsPublisherUnavailable(err) {
		t.Errorf("expected NACK error NOT to be classified as publisher unavailable: %v", err)
	}
}

func TestRabbitPublisherConfirmTimeoutFails(t *testing.T) {
	pub := messaging.NewRabbitPublisherWithSeamsForTest(
		nil,
		func(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) (messaging.ConfirmationWaiter, error) {
			return mockConfirmWaiter{acked: false, err: context.DeadlineExceeded}, nil
		},
		nil,
	)

	env := sampleEnvelope()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := pub.PublishEvent(ctx, "commerce.events", "order.created", env)
	if err == nil {
		t.Fatal("expected confirm timeout error, got nil")
	}
	if messaging.IsPublisherUnavailable(err) {
		t.Errorf("expected confirm timeout error NOT to be classified as publisher unavailable: %v", err)
	}
}

func TestRabbitMQRealBrokerIntegration(t *testing.T) {
	rabbitURL := os.Getenv("TEST_RABBITMQ_URL")
	if rabbitURL == "" {
		t.Skip("TEST_RABBITMQ_URL not set; skipping real RabbitMQ broker integration test")
	}

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		t.Fatalf("RabbitMQ connection failed at %s: %v", rabbitURL, err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer ch.Close()

	exchange := "commerce.events"
	routingKey := "order.created"

	err = ch.ExchangeDeclare(
		exchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("declare exchange: %v", err)
	}

	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare test queue: %v", err)
	}

	err = ch.QueueBind(q.Name, routingKey, exchange, false, nil)
	if err != nil {
		t.Fatalf("bind test queue: %v", err)
	}

	pub, err := messaging.NewRabbitPublisher(ch)
	if err != nil {
		t.Fatalf("NewRabbitPublisher error: %v", err)
	}

	eventID := uuid.NewString()
	correlationID := "test_corr_id_999"
	env := events.EventEnvelope{
		EventID:          eventID,
		EventType:        "commerce.order.created.v1",
		SchemaVersion:    1,
		AggregateType:    "order",
		AggregateID:      "ord_test_real",
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		OccurredAt:       time.Now().UTC().Truncate(time.Microsecond),
		Payload: map[string]any{
			"order_id": "ord_test_real",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = pub.PublishEvent(ctx, exchange, routingKey, env)
	if err != nil {
		t.Fatalf("PublishEvent returned error: %v", err)
	}

	msgs, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume queue: %v", err)
	}

	select {
	case msg := <-msgs:
		if msg.MessageId != eventID {
			t.Errorf("expected MessageId = %s, got %s", eventID, msg.MessageId)
		}
		if msg.Type != "commerce.order.created.v1" {
			t.Errorf("expected Type = commerce.order.created.v1, got %s", msg.Type)
		}
		if msg.CorrelationId != correlationID {
			t.Errorf("expected CorrelationId = %s, got %s", correlationID, msg.CorrelationId)
		}

		var receivedEnv events.EventEnvelope
		if err := json.Unmarshal(msg.Body, &receivedEnv); err != nil {
			t.Fatalf("unmarshal received message body: %v", err)
		}
		if receivedEnv.EventID != eventID {
			t.Errorf("unmarshaled envelope event_id = %s, expected %s", receivedEnv.EventID, eventID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for published message in test queue")
	}
}
