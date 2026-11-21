package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/simruntime"
)

// Nomad is a real provider backed by the Nomad HTTP API. A Target maps to a
// job named "<service>-<environment>":
//
//   - Apply registers the job (POST /v1/jobs) with a docker task image of
//     "<ImagePrefix><service>:<version>" and meta.version for Status reads.
//   - Probe is healthy when GET /v1/job/<name> reports Status "running".
//   - SetTraffic promotes the latest deployment when percent reaches 100
//     (POST /v1/deployment/promote/<id>). Intermediate percents are a no-op:
//     Nomad canaries gate on task counts, not traffic weight, so steps only
//     add bake time between the canary and full promotion.
//   - Status reads the job's status and meta.version.
//
// Opt in with PROVIDER=nomad, NOMAD_ADDR (default http://127.0.0.1:4646),
// optional NOMAD_TOKEN and NOMAD_IMAGE_PREFIX (e.g. "registry.acme/").
type Nomad struct {
	Addr        string
	Token       string
	ImagePrefix string
	Doer        interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func NewNomad(addr, token, imagePrefix string) *Nomad {
	if addr == "" {
		addr = "http://127.0.0.1:4646"
	}
	return &Nomad{
		Addr:        strings.TrimSuffix(addr, "/"),
		Token:       token,
		ImagePrefix: imagePrefix,
		Doer:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (n *Nomad) jobName(t Target) string { return t.Service + "-" + t.Environment }

func (n *Nomad) call(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rdr *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(data)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, n.Addr+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.Token != "" {
		req.Header.Set("X-Nomad-Token", n.Token)
	}
	resp, err := n.Doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: nomad %s %s: %v", ErrUnavailable, method, path, err)
	}
	return resp, nil
}

func (n *Nomad) Apply(ctx context.Context, t Target, version string, _ domain.FailureProfile) error {
	image := n.ImagePrefix + t.Service + ":" + version
	name := n.jobName(t)
	job := map[string]any{
		"Job": map[string]any{
			"ID":   name,
			"Name": name,
			"Type": "service",
			"Meta": map[string]string{"version": version},
			"TaskGroups": []any{map[string]any{
				"Name":  "app",
				"Count": 1,
				"Tasks": []any{map[string]any{
					"Name":   t.Service,
					"Driver": "docker",
					"Config": map[string]string{"image": image},
				}},
			}},
		},
	}
	resp, err := n.call(ctx, http.MethodPost, "/v1/jobs", job)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: nomad register job: %s", ErrUnavailable, resp.Status)
	}
	return nil
}

func (n *Nomad) Probe(ctx context.Context, t Target) error {
	var job struct {
		Status string `json:"Status"`
	}
	if err := n.get(ctx, "/v1/job/"+url.PathEscape(n.jobName(t)), &job); err != nil {
		return err
	}
	if job.Status != "running" {
		return ErrUnhealthy
	}
	return nil
}

func (n *Nomad) SetTraffic(ctx context.Context, t Target, percent int) error {
	if percent < 100 {
		return nil // bake step: Nomad canaries have no traffic-weight dial
	}
	var deployments []struct {
		ID string `json:"ID"`
	}
	if err := n.get(ctx, "/v1/job/"+url.PathEscape(n.jobName(t))+"/deployments?latest=true", &deployments); err != nil {
		return err
	}
	if len(deployments) == 0 {
		return nil // no rolling deployment in flight: nothing to promote
	}
	resp, err := n.call(ctx, http.MethodPost, "/v1/deployment/promote/"+deployments[0].ID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: nomad promote deployment: %s", ErrUnavailable, resp.Status)
	}
	return nil
}

func (n *Nomad) Status(ctx context.Context, t Target) (simruntime.Status, error) {
	var job struct {
		Status string            `json:"Status"`
		Meta   map[string]string `json:"Meta"`
	}
	if err := n.get(ctx, "/v1/job/"+url.PathEscape(n.jobName(t)), &job); err != nil {
		return simruntime.Status{}, err
	}
	status := simruntime.Status{
		Tenant:      t.Tenant,
		Service:     t.Service,
		Environment: t.Environment,
		Version:     job.Meta["version"],
	}
	return status, nil
}

func (n *Nomad) get(ctx context.Context, path string, out any) error {
	resp, err := n.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrUnhealthy
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: nomad get %s: %s", ErrUnavailable, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var _ Provider = (*Nomad)(nil)
