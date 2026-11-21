// Package metrics evaluates real canary rollback signals (error rate, latency)
// from a metrics backend during progressive rollouts.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Probe is a rollback signal evaluated between canary steps: Healthy reports
// whether the deployment's error signal is within the safe threshold.
type Probe interface {
	// Healthy returns nil while the signal is safe; an error rolls the step back.
	Healthy(ctx context.Context) error
}

// Prometheus evaluates an instant query against a Prometheus-compatible API
// (Prometheus, VictoriaMetrics, Grafana Mimir). The scalar result must be at
// or below Threshold — e.g. an error-rate ratio under 0.05.
type Prometheus struct {
	BaseURL   string
	Query     string
	Threshold float64
	Client    *http.Client // optional; defaults to a 5s-timeout client
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value [2]any `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// Healthy fetches the instant query and compares the first scalar sample with
// the threshold. Empty results and non-2xx responses fail closed (unhealthy) —
// a blind canary step must not proceed.
func (p *Prometheus) Healthy(ctx context.Context) error {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return fmt.Errorf("metrics probe: bad base url: %w", err)
	}
	u = u.JoinPath("/api/v1/query")
	q := u.Query()
	q.Set("query", p.Query)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("metrics probe unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("metrics probe: HTTP %d", resp.StatusCode)
	}
	var pr promResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return fmt.Errorf("metrics probe: bad response: %w", err)
	}
	if len(pr.Data.Result) == 0 || len(pr.Data.Result[0].Value) < 2 {
		return fmt.Errorf("metrics probe: query returned no data")
	}
	s, ok := pr.Data.Result[0].Value[1].(string)
	if !ok {
		return fmt.Errorf("metrics probe: non-numeric sample")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("metrics probe: unparseable sample %q", s)
	}
	if v > p.Threshold {
		return fmt.Errorf("metrics probe: %.4f over threshold %.4f", v, p.Threshold)
	}
	return nil
}

// FromEnv builds a Probe from CANARY_METRICS_URL + CANARY_METRICS_QUERY +
// CANARY_METRICS_THRESHOLD; nil when unset or misconfigured.
func FromEnv(getenv func(string) string) Probe {
	base, query := getenv("CANARY_METRICS_URL"), getenv("CANARY_METRICS_QUERY")
	if base == "" || query == "" {
		return nil
	}
	threshold, err := strconv.ParseFloat(getenv("CANARY_METRICS_THRESHOLD"), 64)
	if err != nil {
		return nil
	}
	return &Prometheus{BaseURL: base, Query: query, Threshold: threshold}
}
