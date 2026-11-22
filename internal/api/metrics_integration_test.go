//go:build integration

package api_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tatitoxt/shippulse/internal/api"
	"github.com/tatitoxt/shippulse/internal/auth"
	"github.com/tatitoxt/shippulse/internal/metrics"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/testutil"
)

func TestMetricsEndpoint(t *testing.T) {
	s := testutil.DB(t, "api")
	iss, err := auth.NewIssuer("integration-jwt-secret-0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: s, Auth: iss, Runtime: provider.NewMemory(),
		Metrics: metrics.NewExporter(s), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	acme := testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging")
	c := login(t, ts.URL, "operator@acme.example")
	code, out := c.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"),
		map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %v", code, out)
	}

	if _, err := http.Get(ts.URL + "/healthz"); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type: %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		`shippulse_deployments{status="queued"} 1`,
		`shippulse_deployments{status="running"} 0`,
		`shippulse_deployment_steps{name="validate",status="pending"} 1`,
		"shippulse_rollback_operations{status=",
		"shippulse_deployment_duration_seconds_count",
		"shippulse_outbox_pending_events",
		`shippulse_http_requests_total{method="POST",code="202"}`,
		`shippulse_http_requests_total{method="GET",code="204"}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, text)
		}
	}
}

func TestMetricsAbsentWithoutExporter(t *testing.T) {
	base, _, _ := newServer(t)
	resp, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 without exporter, got %d", resp.StatusCode)
	}
}
