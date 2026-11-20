// Package bus carries job events over NATS JetStream. It is a transport only:
// PostgreSQL stays the source of truth and every consumer is idempotent.
package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/store"
)

const (
	StreamName    = "SHIPPULSE_JOBS"
	SubjectPrefix = "shippulse.jobs."
	ConsumerName  = "shippulse-worker"
)

// ErrRetryLater asks the bus to redeliver the message after a short delay.
var ErrRetryLater = errors.New("retry later")

type Bus struct {
	NC       *nats.Conn
	JS       jetstream.JetStream
	Stream   jetstream.Stream
	name     string
	prefix   string
	consumer string
}

func Connect(ctx context.Context, url string) (*Bus, error) {
	return ConnectNamed(ctx, url, StreamName, SubjectPrefix, ConsumerName)
}

// ConnectNamed lets tests use an isolated stream, subject prefix and consumer.
func ConnectNamed(ctx context.Context, url, stream, prefix, consumer string) (*Bus, error) {
	nc, err := nats.Connect(url, nats.Name("shippulse"), nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	st, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       stream,
		Subjects:   []string{prefix + ">"},
		Storage:    jetstream.FileStorage,
		Duplicates: 2 * time.Minute,
		MaxAge:     24 * time.Hour,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	return &Bus{NC: nc, JS: js, Stream: st, name: stream, prefix: prefix, consumer: consumer}, nil
}

func (b *Bus) Close() { b.NC.Close() }

// Publish sends an outbox event using its id as Nats-Msg-Id, so a relay retry
// after a lost ack is dropped by JetStream's duplicate window.
func (b *Bus) Publish(ctx context.Context, ev store.OutboxEvent) error {
	_, err := b.JS.Publish(ctx, b.prefix+ev.EventType, ev.Payload, jetstream.WithMsgID(ev.ID))
	return err
}

// Handler processes one job. Returning nil acks the message.
type Handler func(ctx context.Context, ev domain.JobEvent) error

// Consume starts a durable pull consumer and dispatches messages to h.
func (b *Bus) Consume(ctx context.Context, log *slog.Logger, h Handler) (jetstream.ConsumeContext, error) {
	cons, err := b.JS.CreateOrUpdateConsumer(ctx, b.name, jetstream.ConsumerConfig{
		Durable:       b.consumer,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Minute,
		MaxDeliver:    100,
		FilterSubject: b.prefix + ">",
	})
	if err != nil {
		return nil, fmt.Errorf("ensure consumer: %w", err)
	}
	return cons.Consume(func(msg jetstream.Msg) {
		var ev domain.JobEvent
		if err := json.Unmarshal(msg.Data(), &ev); err != nil || ev.PayloadVersion != 1 {
			log.Error("dropping malformed job", "subject", msg.Subject(), "err", err)
			_ = msg.Term()
			return
		}
		err := h(ctx, ev)
		switch {
		case err == nil:
			_ = msg.Ack()
		case errors.Is(err, ErrRetryLater):
			_ = msg.NakWithDelay(time.Second)
		default:
			log.Error("job failed; will be redelivered", "type", ev.Type, "aggregate_id", ev.AggregateID,
				"correlation_id", ev.CorrelationID, "err", err)
			_ = msg.NakWithDelay(3 * time.Second)
		}
	})
}
