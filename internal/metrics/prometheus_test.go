package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func promServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" || r.URL.Query().Get("query") == "" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(body))
	}))
}

func vector(v string) string {
	return `{"status":"success","data":{"resultType":"vector","result":[{"value":[0,"` + v + `"]}]}}`
}

func TestPrometheusHealthy(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want string // "" = healthy
	}{
		"under threshold": {vector("0.02"), ""},
		"over threshold":  {vector("0.09"), "over threshold"},
		"empty result":    {`{"status":"success","data":{"resultType":"vector","result":[]}}`, "no data"},
		"non-numeric":     {vector("NaNed"), "unparseable"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := promServer(t, tc.body)
			t.Cleanup(srv.Close)
			p := &Prometheus{BaseURL: srv.URL, Query: "error_rate", Threshold: 0.05}
			err := p.Healthy(context.Background())
			if tc.want == "" && err != nil {
				t.Fatalf("healthy: %v", err)
			}
			if tc.want != "" && (err == nil || !contains(err.Error(), tc.want)) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	p := &Prometheus{BaseURL: srv.URL, Query: "x", Threshold: 1}
	if err := p.Healthy(context.Background()); err == nil {
		t.Fatal("404 should fail closed")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{
		"CANARY_METRICS_URL":       "http://prom:9090",
		"CANARY_METRICS_QUERY":     "rate(errors[1m])",
		"CANARY_METRICS_THRESHOLD": "0.05",
	}
	get := func(k string) string { return env[k] }
	if FromEnv(get) == nil {
		t.Fatal("expected a probe")
	}
	delete(env, "CANARY_METRICS_QUERY")
	if FromEnv(get) != nil {
		t.Fatal("missing query should disable the probe")
	}
}

func TestPrometheusMultiCheck(t *testing.T) {
	// Each query name returns a different value; the p99 histogram check breaches.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vals := map[string]string{"error_rate": "0.02", "p99_latency": "0.9"}
		w.Write([]byte(vector(vals[r.URL.Query().Get("query")])))
	}))
	t.Cleanup(srv.Close)
	p := &Prometheus{BaseURL: srv.URL, Checks: []Check{
		{Name: "error-rate", Query: "error_rate", Threshold: 0.05},
		{Name: "p99-latency", Query: "p99_latency", Threshold: 0.5},
	}}
	err := p.Healthy(context.Background())
	if err == nil || !contains(err.Error(), `"p99-latency"`) || !contains(err.Error(), "over threshold") {
		t.Fatalf("err = %v, want named p99-latency breach", err)
	}
}

func TestFromEnvChecks(t *testing.T) {
	env := map[string]string{
		"CANARY_METRICS_URL":    "http://prom:9090",
		"CANARY_METRICS_CHECKS": `[{"name":"error-rate","query":"error_rate","threshold":0.05},{"name":"p99","query":"histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))","threshold":0.5}]`,
	}
	pr := FromEnv(func(k string) string { return env[k] })
	p, ok := pr.(*Prometheus)
	if !ok || len(p.Checks) != 2 || p.Checks[1].Name != "p99" {
		t.Fatalf("FromEnv = %+v, want 2 checks", pr)
	}

	env["CANARY_METRICS_CHECKS"] = `{"not":"a list"}`
	if FromEnv(func(k string) string { return env[k] }) != nil {
		t.Fatal("malformed CANARY_METRICS_CHECKS must yield nil probe")
	}

	delete(env, "CANARY_METRICS_CHECKS")
	env["CANARY_METRICS_QUERY"] = "error_rate"
	env["CANARY_METRICS_THRESHOLD"] = "0.05"
	p2, ok := FromEnv(func(k string) string { return env[k] }).(*Prometheus)
	if !ok || p2.Query != "error_rate" {
		t.Fatalf("legacy single-check envs broke: %+v", p2)
	}
}
