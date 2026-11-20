// Package demo drives the scripted end-to-end demo through the public HTTP API,
// exactly like an operator (or GitHub) would.
package demo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tatitoxt/shippulse/internal/store"
)

type Config struct {
	APIURL, Email, Password string
	WebhookSecret           string // when set, the second deploy arrives as a signed GitHub event
	Timeout                 time.Duration
}

type client struct {
	cfg   Config
	token string
	http  *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body any, headers map[string]string, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.APIURL+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

// Run deploys three versions of the synthetic checkout-api to staging:
// v1.0.0 (healthy), v1.1.0 (healthy) and v1.2.0 (unhealthy, which must be
// rolled back automatically to v1.1.0). It returns an error if any
// expectation is not met.
func Run(ctx context.Context, w io.Writer, cfg Config) error {
	if cfg.Password == "" {
		return fmt.Errorf("DEMO_PASSWORD is required")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	c := &client{cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}}
	var login struct {
		Token string          `json:"token"`
		User  store.Principal `json:"user"`
	}
	if _, err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": cfg.Email, "password": cfg.Password}, nil, &login); err != nil {
		return err
	}
	c.token = login.Token
	fmt.Fprintf(w, "Logged in as %s (%s, role %s)\n", login.User.Email, login.User.Organization, login.User.Role)

	var services struct {
		Services []store.Service `json:"services"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/services", nil, nil, &services); err != nil {
		return err
	}
	var svc store.Service
	var envID string
	for _, s := range services.Services {
		if s.Slug == "checkout-api" {
			svc = s
			for _, e := range s.Environments {
				if e.EnvironmentSlug == "staging" {
					envID = e.EnvironmentID
				}
			}
		}
	}
	if envID == "" {
		return fmt.Errorf("checkout-api/staging not found; did you run `shippulse seed`?")
	}
	run := time.Now().UTC().Format("20060102T150405")

	fmt.Fprintln(w, "\n[1/3] Manual deploy of v1.0.0 (healthy)")
	d1, err := c.deploy(ctx, svc.ID, envID, "v1.0.0", "healthy", "demo-"+run+"-1")
	if err != nil {
		return err
	}
	if err := c.expect(ctx, w, d1, "succeeded", ""); err != nil {
		return err
	}

	var d2 string
	if cfg.WebhookSecret != "" {
		fmt.Fprintln(w, "\n[2/3] GitHub 'deployment' webhook for v1.1.0 (healthy, signed with HMAC-SHA256)")
		d2, err = c.webhook(ctx, *svc.Repository, "staging", "v1.1.0", "demo-"+run+"-2")
	} else {
		fmt.Fprintln(w, "\n[2/3] Manual deploy of v1.1.0 (healthy; set GITHUB_WEBHOOK_SECRET to send it as a webhook)")
		d2, err = c.deploy(ctx, svc.ID, envID, "v1.1.0", "healthy", "demo-"+run+"-2")
	}
	if err != nil {
		return err
	}
	if err := c.expect(ctx, w, d2, "succeeded", ""); err != nil {
		return err
	}

	fmt.Fprintln(w, "\n[3/3] Manual deploy of v1.2.0 with failure_profile=unhealthy")
	d3, err := c.deploy(ctx, svc.ID, envID, "v1.2.0", "unhealthy", "demo-"+run+"-3")
	if err != nil {
		return err
	}
	if err := c.expect(ctx, w, d3, "failed", "succeeded"); err != nil {
		return err
	}

	var runtime struct {
		Runtimes []struct {
			Environment   string  `json:"environment"`
			StableVersion *string `json:"stable_version"`
			Observed      *struct {
				Version string `json:"version"`
			} `json:"observed"`
		} `json:"runtimes"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/services/"+svc.ID+"/runtime", nil, nil, &runtime); err != nil {
		return err
	}
	for _, r := range runtime.Runtimes {
		if r.Environment != "staging" {
			continue
		}
		if r.StableVersion == nil || *r.StableVersion != "v1.1.0" || r.Observed == nil || r.Observed.Version != "v1.1.0" {
			return fmt.Errorf("expected stable and running version v1.1.0 after rollback, got %+v", r)
		}
		fmt.Fprintf(w, "\nRuntime check: stable version %s, simulated runtime is running %s\n", *r.StableVersion, r.Observed.Version)
	}

	var audit struct {
		Events []store.AuditEvent `json:"audit_events"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/audit-events?subject_id="+d3, nil, nil, &audit); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nAudit trail for the failed deployment %s:\n", d3)
	for i := len(audit.Events) - 1; i >= 0; i-- {
		e := audit.Events[i]
		fmt.Fprintf(w, "  %s  %-26s %s\n", e.OccurredAt.Format(time.RFC3339), e.Action, e.Actor)
	}
	fmt.Fprintf(w, "\nDemo OK. Open the console at http://localhost:3000/deployments/%s\n", d3)
	return nil
}

func (c *client) deploy(ctx context.Context, serviceID, envID, version, profile, key string) (string, error) {
	var d store.Deployment
	_, err := c.do(ctx, http.MethodPost, "/api/v1/deployments", map[string]string{
		"service_id": serviceID, "environment_id": envID, "version": version, "failure_profile": profile,
	}, map[string]string{"Idempotency-Key": key}, &d)
	return d.ID, err
}

func (c *client) webhook(ctx context.Context, repo, env, ref, delivery string) (string, error) {
	payload := map[string]any{
		"deployment": map[string]any{"id": 1, "sha": "0000000000000000000000000000000000000000", "ref": ref, "environment": env},
		"repository": map[string]any{"full_name": repo},
		"sender":     map[string]any{"login": "demo-bot"},
	}
	body, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(c.cfg.WebhookSecret))
	mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/api/v1/webhooks/github", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-GitHub-Event", "deployment")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		DeploymentID string `json:"deployment_id"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("webhook: %d %s", resp.StatusCode, raw)
	}
	err = json.Unmarshal(raw, &out)
	return out.DeploymentID, err
}

// expect polls until the deployment (and its rollback, if one is expected) is terminal.
func (c *client) expect(ctx context.Context, w io.Writer, id, status, rollback string) error {
	for {
		var d store.Deployment
		if _, err := c.do(ctx, http.MethodGet, "/api/v1/deployments/"+id, nil, nil, &d); err != nil {
			return err
		}
		rbDone := rollback == ""
		var rb *store.Rollback
		if len(d.Rollbacks) > 0 {
			rb = &d.Rollbacks[len(d.Rollbacks)-1]
			rbDone = rbDone || rb.Status == "succeeded" || rb.Status == "failed"
		}
		if (d.Status == "succeeded" || d.Status == "failed" || d.Status == "cancelled") && rbDone {
			fmt.Fprintf(w, "  deployment %s  %s -> %s\n", d.ID, d.Version, d.Status)
			for _, st := range d.Steps {
				fmt.Fprintf(w, "    %-13s %-9s %s\n", st.Name, st.Status, st.SafeOutput)
			}
			if d.Status != status {
				return fmt.Errorf("deployment %s: expected %s, got %s", d.ID, status, d.Status)
			}
			if rollback != "" {
				if rb == nil || rb.Status != rollback {
					return fmt.Errorf("deployment %s: expected rollback %s, got %+v", d.ID, rollback, rb)
				}
				fmt.Fprintf(w, "  automatic rollback %s: %s -> %s %s (%s)\n", rb.ID, rb.FromVersion, rb.ToVersion, rb.Status, rb.SafeOutput)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for deployment %s (status %s)", id, d.Status)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
