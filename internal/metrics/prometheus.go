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

// Check is one named rollback signal: an instant query whose first scalar
// sample must stay at or below Threshold — e.g. an error-rate ratio under
// 0.05, or a `histogram_quantile(0.99, ...)` p99 latency under 0.5s.
type Check struct {
	Name      string  `json:"name"`
	Query     string  `json:"query"`
	Threshold float64 `json:"threshold"`
}

// Prometheus evaluates instant queries against a Prometheus-compatible API
// (Prometheus, VictoriaMetrics, Grafana Mimir). With Checks set, every check
// must pass for the probe to be healthy — so a scalar error-rate check can be
// combined with histogram-quantile latency checks. Without Checks it falls
// back to the single Query/Threshold pair.
type Prometheus struct {
	BaseURL   string
	Query     string
	Threshold float64
	Checks    []Check
	Client    *http.Client // optional; defaults to a 5s-timeout client
}

// checks returns the effective check list: explicit Checks when present, else
// the legacy single Query/Threshold as one unnamed check.
func (p *Prometheus) checks() []Check {
	if len(p.Checks) > 0 {
		return p.Checks
	}
	return []Check{{Name: "scalar", Query: p.Query, Threshold: p.Threshold}}
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

// query fetches one instant query and returns its first scalar sample.
// Empty results and non-2xx responses fail closed — a blind canary step must
// not proceed.
func (p *Prometheus) query(ctx context.Context, c Check) (float64, error) {
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return 0, fmt.Errorf("bad base url: %w", err)
	}
	u = u.JoinPath("/api/v1/query")
	q := u.Query()
	q.Set("query", c.Query)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("metrics probe unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var pr promResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return 0, fmt.Errorf("bad response: %w", err)
	}
	if len(pr.Data.Result) == 0 || len(pr.Data.Result[0].Value) < 2 {
		return 0, fmt.Errorf("query returned no data")
	}
	s, ok := pr.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("non-numeric sample")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("unparseable sample %q", s)
	}
	return v, nil
}

// Healthy evaluates every check in order and reports the first breach,
// naming the check and its observed value.
func (p *Prometheus) Healthy(ctx context.Context) error {
	for _, c := range p.checks() {
		v, err := p.query(ctx, c)
		if err != nil {
			return fmt.Errorf("metrics probe %q: %w", c.Name, err)
		}
		if v > c.Threshold {
			return fmt.Errorf("metrics probe %q: %.4f over threshold %.4f", c.Name, v, c.Threshold)
		}
	}
	return nil
}

// FromEnv builds a Probe from CANARY_METRICS_URL plus either
// CANARY_METRICS_CHECKS (a JSON array of {name, query, threshold}) or the
// single-check pair CANARY_METRICS_QUERY + CANARY_METRICS_THRESHOLD;
// nil when unset or misconfigured.
func FromEnv(getenv func(string) string) Probe {
	base := getenv("CANARY_METRICS_URL")
	if base == "" {
		return nil
	}
	if raw := getenv("CANARY_METRICS_CHECKS"); raw != "" {
		var checks []Check
		if err := json.Unmarshal([]byte(raw), &checks); err != nil || len(checks) == 0 {
			return nil
		}
		for _, c := range checks {
			if c.Name == "" || c.Query == "" {
				return nil
			}
		}
		return &Prometheus{BaseURL: base, Checks: checks}
	}
	query := getenv("CANARY_METRICS_QUERY")
	if query == "" {
		return nil
	}
	threshold, err := strconv.ParseFloat(getenv("CANARY_METRICS_THRESHOLD"), 64)
	if err != nil {
		return nil
	}
	return &Prometheus{BaseURL: base, Query: query, Threshold: threshold}
}
