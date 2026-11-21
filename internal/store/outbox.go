package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tatitoxt/shippulse/internal/domain"
)

// PublishFunc delivers one outbox event to the bus.
type PublishFunc func(ctx context.Context, ev OutboxEvent) error

// RelayOutbox publishes up to limit due events. Rows are locked with SKIP
// LOCKED so several relays can run concurrently without double-publishing in
// the common case; JetStream's Nats-Msg-Id dedup covers the rest. Failed
// publishes are retried with capped exponential backoff.
func (s *Store) RelayOutbox(ctx context.Context, limit int, publish PublishFunc, base, max time.Duration) (published, failed int, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT id, organization_id, aggregate_id, event_type, payload, correlation_id, attempts
		FROM outbox_events WHERE published_at IS NULL AND next_attempt_at <= now()
		ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return 0, 0, err
	}
	events, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OutboxEvent, error) {
		var ev OutboxEvent
		err := r.Scan(&ev.ID, &ev.OrganizationID, &ev.AggregateID, &ev.EventType, &ev.Payload, &ev.CorrelationID, &ev.Attempts)
		return ev, err
	})
	if err != nil {
		return 0, 0, err
	}
	for _, ev := range events {
		if perr := publish(ctx, ev); perr != nil {
			delay := domain.Backoff(ev.Attempts+1, base, max)
			msg := perr.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			if _, err := tx.Exec(ctx, `UPDATE outbox_events SET attempts = attempts + 1, last_error = $2,
				next_attempt_at = now() + make_interval(secs => $3) WHERE id = $1`, ev.ID, msg, delay.Seconds()); err != nil {
				return published, failed, err
			}
			failed++
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox_events SET attempts = attempts + 1, published_at = now(), last_error = NULL
			WHERE id = $1`, ev.ID); err != nil {
			return published, failed, err
		}
		published++
	}
	return published, failed, tx.Commit(ctx)
}

// PendingOutbox counts unpublished events (used by tests and the demo).
func (s *Store) PendingOutbox(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&n)
	return n, err
}
