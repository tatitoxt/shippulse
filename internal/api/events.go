package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
	Seq    int64           `json:"seq"`
}

// notifier fans database change notifications out to SSE subscribers over a
// single dedicated LISTEN connection. Pooling a connection per client would
// starve the API when several consoles are open, so the LISTEN conn is opened
// directly (never counted against MaxConns) and events are broadcast
// in-process. A client that vanishes costs only a goroutine until the handler
// notices, never a database connection.
type notifier struct {
	pool *pgxpool.Pool // also used for replay + prune queries
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
// after drops; reconnecting clients replay missed changes via Last-Event-ID.
func (n *notifier) run() {
	ctx := context.Background()
	for {
		listenCtx, stop := context.WithCancel(ctx)
		if err := n.listen(listenCtx); err != nil {
			n.log.Warn("change listener ended; reconnecting", "err", err)
		}
		stop()
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
	go n.prune(ctx)
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

// prune drops change_events older than an hour so the replay log stays
// bounded; SSE clients older than that fall back to the 30 s poll.
func (n *notifier) prune(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if _, err := n.pool.Exec(ctx,
			`DELETE FROM change_events WHERE created_at < now() - interval '1 hour'`); err != nil {
			n.log.Warn("change_events prune failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// replayEvents re-sends persisted change rows newer than seq for one org,
// returning the largest id written so live delivery can skip duplicates.
func (n *notifier) replayEvents(ctx context.Context, org string, after int64, send func(seq int64, change json.RawMessage) error) (int64, error) {
	rows, err := n.pool.Query(ctx,
		`SELECT id, change FROM change_events WHERE organization_id = $1 AND id > $2 ORDER BY id`,
		org, after)
	if err != nil {
		return after, err
	}
	defer rows.Close()
	last := after
	for rows.Next() {
		var id int64
		var change json.RawMessage
		if err := rows.Scan(&id, &change); err != nil {
			return last, err
		}
		if err := send(id, change); err != nil {
			return last, err
		}
		last = id
	}
	return last, rows.Err()
}

// streamEvents serves GET /api/v1/events as an org-scoped Server-Sent Events
// stream. Deployment, step and promoted-version changes arrive via Postgres
// LISTEN/NOTIFY, so the stream works across API/worker process boundaries.
// Clients re-fetch the affected resource; events carry only ids and statuses.
// Reconnecting clients pass Last-Event-ID and missed changes are replayed
// from the persisted change_events log before live delivery resumes.
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
	lastID, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	hadLastID := r.Header.Get("Last-Event-ID") != ""
	send := func(seq int64, change json.RawMessage) error {
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", seq, change); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	// Replay anything missed while disconnected before live delivery resumes
	// (only when the client passes Last-Event-ID — a fresh connect gets none).
	var err error
	if hadLastID {
		if lastID, err = n.replayEvents(r.Context(), org, lastID, send); err != nil {
			return
		}
	}
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case note := <-ch:
			if note.Org != org || (note.Seq != 0 && note.Seq <= lastID) {
				continue
			}
			if err := send(note.Seq, note.Change); err != nil {
				return
			}
			lastID = note.Seq
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
