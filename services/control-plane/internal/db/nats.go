package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/tatitoxt/shippulse/packages/contracts"
)

type NatsClient struct {
	Conn *nats.Conn
	JS   nats.JetStreamContext
}

func NewNatsClient(natsURL string) (*NatsClient, error) {
	nc, err := nats.Connect(natsURL, nats.Timeout(10*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Create JetStream Stream if not exists
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "SHIPPULSE_EVENTS",
		Subjects: []string{"shippulse.events.>"},
		Storage:  nats.MemoryStorage,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		// Ignore if stream already exists
	}

	return &NatsClient{
		Conn: nc,
		JS:   js,
	}, nil
}

func (n *NatsClient) PublishEvent(subject string, evt contracts.Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = n.JS.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish NATS event: %w", err)
	}

	return nil
}

func (n *NatsClient) Close() {
	if n.Conn != nil {
		n.Conn.Close()
	}
}
