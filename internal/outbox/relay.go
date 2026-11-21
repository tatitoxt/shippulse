// Package outbox moves committed outbox rows onto the bus.
package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/tatitoxt/shippulse/internal/store"
)

type Relay struct {
	Store    *store.Store
	Publish  store.PublishFunc
	Interval time.Duration
	Batch    int
	Base     time.Duration
	Max      time.Duration
	Log      *slog.Logger
}

// Run polls until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick drains due events until a batch comes back short.
func (r *Relay) Tick(ctx context.Context) {
	for {
		pub, failed, err := r.Store.RelayOutbox(ctx, r.Batch, r.Publish, r.Base, r.Max)
		if err != nil {
			if ctx.Err() == nil {
				r.Log.Error("outbox relay", "err", err)
			}
			return
		}
		if failed > 0 {
			r.Log.Warn("outbox publish failed; will retry with backoff", "failed", failed)
		}
		if pub+failed < r.Batch {
			return
		}
	}
}
