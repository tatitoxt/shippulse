// Exporter serves GET /metrics in Prometheus text exposition format. All
// gauges and summaries are derived from the database at scrape time, so they
// cover work done by every api and worker process — no shared state needed.
// HTTP request counters are process-local and reported by whichever api
// instance is scraped.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tatitoxt/shippulse/internal/store"
)

// Exporter renders the Prometheus exposition body.
type Exporter struct {
	Store *store.Store

	mu       sync.Mutex
	requests map[reqKey]int64 // this process's HTTP request counts
}

type reqKey struct {
	method string
	code   int
}

// NewExporter returns an Exporter reading deployment state from the store.
func NewExporter(st *store.Store) *Exporter {
	return &Exporter{Store: st, requests: map[reqKey]int64{}}
}

// ObserveRequest records one handled HTTP request for the http_requests_total
// counter family. Call it from middleware after the handler ran.
func (e *Exporter) ObserveRequest(method string, statusCode int) {
	e.mu.Lock()
	e.requests[reqKey{method: method, code: statusCode}]++
	e.mu.Unlock()
}

// Handler returns an http.Handler for GET /metrics.
func (e *Exporter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		var b strings.Builder
		if err := e.render(ctx, &b); err != nil {
			http.Error(w, "metrics unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	})
}

func (e *Exporter) render(ctx context.Context, b *strings.Builder) error {
	if err := e.deployments(ctx, b); err != nil {
		return err
	}
	if err := e.steps(ctx, b); err != nil {
		return err
	}
	if err := e.rollbacks(ctx, b); err != nil {
		return err
	}
	if err := e.durations(ctx, b); err != nil {
		return err
	}
	if err := e.outbox(ctx, b); err != nil {
		return err
	}
	e.httpRequests(b)
	return nil
}

func (e *Exporter) deployments(ctx context.Context, b *strings.Builder) error {
	rows, err := e.Store.Pool.Query(ctx, `SELECT status, count(*) FROM deployments GROUP BY status`)
	if err != nil {
		return fmt.Errorf("deployments: %w", err)
	}
	defer rows.Close()

	b.WriteString("# HELP shippulse_deployments Deployments by status.\n# TYPE shippulse_deployments gauge\n")
	counts := map[string]int64{}
	if err := scanCounts(rows, func(status string, n int64) { counts[status] = n }); err != nil {
		return err
	}
	for _, status := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
		fmt.Fprintf(b, "shippulse_deployments{status=%q} %d\n", status, counts[status])
	}
	return nil
}

func (e *Exporter) steps(ctx context.Context, b *strings.Builder) error {
	rows, err := e.Store.Pool.Query(ctx,
		`SELECT name, status, count(*) FROM deployment_steps GROUP BY name, status ORDER BY name, status`)
	if err != nil {
		return fmt.Errorf("deployment_steps: %w", err)
	}
	defer rows.Close()

	b.WriteString("# HELP shippulse_deployment_steps Deployment step executions by step and outcome.\n# TYPE shippulse_deployment_steps counter\n")
	for rows.Next() {
		var name, status string
		var n int64
		if err := rows.Scan(&name, &status, &n); err != nil {
			return err
		}
		fmt.Fprintf(b, "shippulse_deployment_steps{name=%q,status=%q} %d\n", name, status, n)
	}
	return rows.Err()
}

func (e *Exporter) rollbacks(ctx context.Context, b *strings.Builder) error {
	rows, err := e.Store.Pool.Query(ctx, `SELECT status, count(*) FROM rollback_operations GROUP BY status`)
	if err != nil {
		return fmt.Errorf("rollbacks: %w", err)
	}
	defer rows.Close()

	b.WriteString("# HELP shippulse_rollback_operations Rollback operations by status.\n# TYPE shippulse_rollback_operations gauge\n")
	counts := map[string]int64{}
	if err := scanCounts(rows, func(status string, n int64) { counts[status] = n }); err != nil {
		return err
	}
	for _, status := range []string{"queued", "running", "succeeded", "failed"} {
		fmt.Fprintf(b, "shippulse_rollback_operations{status=%q} %d\n", status, counts[status])
	}
	return nil
}

func (e *Exporter) durations(ctx context.Context, b *strings.Builder) error {
	var p50, p95, sum, count float64
	err := e.Store.Pool.QueryRow(ctx, `
		SELECT COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY d), 0),
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY d), 0),
		       COALESCE(sum(d), 0), count(*)
		  FROM (SELECT extract(epoch FROM finished_at - started_at) AS d
		          FROM deployments
		         WHERE status = 'succeeded' AND started_at IS NOT NULL) t`).Scan(&p50, &p95, &sum, &count)
	if err != nil {
		return fmt.Errorf("durations: %w", err)
	}

	b.WriteString("# HELP shippulse_deployment_duration_seconds Time from dispatch to completion of succeeded deployments.\n" +
		"# TYPE shippulse_deployment_duration_seconds summary\n")
	fmt.Fprintf(b, "shippulse_deployment_duration_seconds{quantile=\"0.5\"} %s\n", strconv.FormatFloat(p50, 'f', -1, 64))
	fmt.Fprintf(b, "shippulse_deployment_duration_seconds{quantile=\"0.95\"} %s\n", strconv.FormatFloat(p95, 'f', -1, 64))
	fmt.Fprintf(b, "shippulse_deployment_duration_seconds_sum %s\n", strconv.FormatFloat(sum, 'f', -1, 64))
	fmt.Fprintf(b, "shippulse_deployment_duration_seconds_count %d\n", int64(count))
	return nil
}

func (e *Exporter) outbox(ctx context.Context, b *strings.Builder) error {
	var pending, attempts int64
	err := e.Store.Pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(sum(attempts), 0) FROM outbox_events WHERE published_at IS NULL`).Scan(&pending, &attempts)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	b.WriteString("# HELP shippulse_outbox_pending_events Outbox events awaiting publish.\n# TYPE shippulse_outbox_pending_events gauge\n")
	fmt.Fprintf(b, "shippulse_outbox_pending_events %d\n", pending)
	b.WriteString("# HELP shippulse_outbox_retry_attempts Publish attempts already spent on pending outbox events.\n# TYPE shippulse_outbox_retry_attempts gauge\n")
	fmt.Fprintf(b, "shippulse_outbox_retry_attempts %d\n", attempts)
	return nil
}

func (e *Exporter) httpRequests(b *strings.Builder) {
	b.WriteString("# HELP shippulse_http_requests_total HTTP requests handled by this api process.\n# TYPE shippulse_http_requests_total counter\n")
	e.mu.Lock()
	keys := make([]reqKey, 0, len(e.requests))
	for k := range e.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].code < keys[j].code
	})
	for _, k := range keys {
		fmt.Fprintf(b, "shippulse_http_requests_total{method=%q,code=%q} %d\n",
			k.method, strconv.Itoa(k.code), e.requests[k])
	}
	e.mu.Unlock()
}

func scanCounts(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}, emit func(status string, n int64)) error {
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return err
		}
		emit(status, n)
	}
	return rows.Err()
}
