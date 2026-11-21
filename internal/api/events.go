package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// changeNotification is the payload emitted by the shippulse_notify_change
// trigger: an org envelope around a small change descriptor.
type changeNotification struct {
	Org    string          `json:"org"`
	Change json.RawMessage `json:"change"`
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

	conn, err := s.Store.Pool.Acquire(r.Context())
	if err != nil {
		s.internal(w, r, fmt.Errorf("acquire listen conn: %w", err))
		return
	}
	defer conn.Release()

	if _, err := conn.Exec(r.Context(), "LISTEN shippulse_changes"); err != nil {
		s.internal(w, r, fmt.Errorf("listen: %w", err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	org := principal(r).OrganizationID
	for {
		// Block up to the heartbeat interval; on timeout emit a comment line so
		// proxies and browsers do not consider the stream stalled.
		waitCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		n, err := conn.Conn().WaitForNotification(waitCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && r.Context().Err() == nil {
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
				continue
			}
			if r.Context().Err() == nil {
				s.Log.Warn("event stream ended", "err", err, "correlation_id", correlationID(r))
			}
			return
		}
		var note changeNotification
		if err := json.Unmarshal([]byte(n.Payload), &note); err != nil {
			s.Log.Warn("bad notification payload", "err", err, "correlation_id", correlationID(r))
			continue
		}
		if note.Org != org {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", note.Change); err != nil {
			return
		}
		flusher.Flush()
	}
}
