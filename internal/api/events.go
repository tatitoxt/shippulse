package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// changeNotification is the payload emitted by the shippulse_notify_change
// trigger: an org envelope around a small change descriptor.
type changeNotification struct {
	Org    string          `json:"org"`
	Change json.RawMessage `json:"change"`
}

// notifier fans database change notifications out to SSE subscribers over a
// single dedicated LISTEN connection. Pooling a connection per client would
// starve the API when several consoles are open, so the LISTEN conn is opened
// directly (never counted against MaxConns) and events are broadcast
// in-process. A client that vanishes costs only a goroutine until the handler
// notices, never a database connection.
type notifier struct {
	pool *pgxpool.Pool // config source only
	log  *slog.Logger

	mu   sync.Mutex
	subs map[chan changeNotification]struct{}
}

func newNotifier(pool *pgxpool.Pool, log *slog.Logger) *notifier {
	n := &notifier{pool: pool, log: log, subs: map[chan changeNotification]struct{}{}}
	go n.run()
	return n
}

func (n *notifier) subscribe() chan changeNotification {
	ch := make(chan changeNotification, 256)
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	return ch
}

func (n *notifier) unsubscribe(ch chan changeNotification) {
	n.mu.Lock()
	delete(n.subs, ch)
	n.mu.Unlock()
}

// run keeps the LISTEN connection alive for the process lifetime, reconnecting
// after drops. WaitForNotification fails on conn loss; stale events are not
// replayed (clients refresh anyway).
func (n *notifier) run() {
	for {
		if err := n.listen(context.Background()); err != nil {
			n.log.Warn("change listener ended; reconnecting", "err", err)
		}
		time.Sleep(2 * time.Second)
	}
}

func (n *notifier) listen(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, n.pool.Config().ConnConfig)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN shippulse_changes"); err != nil {
		return err
	}
	n.log.Info("listening for change notifications")
	for {
		msg, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var cn changeNotification
		if err := json.Unmarshal([]byte(msg.Payload), &cn); err != nil {
			n.log.Warn("bad notification payload", "err", err)
			continue
		}
		n.broadcast(cn)
	}
}

func (n *notifier) broadcast(cn changeNotification) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- cn:
		default:
			// Subscriber buffer full: the client is wedged or gone. Dropping
			// the event is safe — the poll backstop still refreshes it.
		}
	}
}

// streamEvents serves GET /api/v1/events as an org-scoped Server-Sent Events
// stream. Deployment, step and promoted-version changes arrive via Postgres
// LISTEN/NOTIFY, so the stream works across API/worker process boundaries.
// Clients re-fetch the affected resource; events carry only ids and statuses.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming_unsupported", "response writer cannot stream")
		return
	}

	n := s.events()
	ch := n.subscribe()
	defer n.unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	org := principal(r).OrganizationID
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case note := <-ch:
			if note.Org != org {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", note.Change); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			// Comment line so proxies and browsers do not consider the stream
			// stalled; also flushes dead connections.
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
