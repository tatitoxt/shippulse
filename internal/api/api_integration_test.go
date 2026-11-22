//go:build integration

package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/testutil"
)

const webhookSecret = "integration-webhook-secret"

type client struct {
	t     *testing.T
	base  string
	token string
}

func (c client) do(method, path string, body any, headers map[string]string) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &out)
	} else if len(raw) > 0 {
		out["_list"] = string(raw)
	}
	return resp.StatusCode, out
}

func newServer(t *testing.T) (string, testutil.Target, testutil.Target) {
	t.Helper()
	s := testutil.DB(t, "api")
	iss, err := auth.NewIssuer("integration-jwt-secret-0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: s, Auth: iss, Runtime: provider.NewMemory(), WebhookSecret: webhookSecret,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts.URL, testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging"),
		testutil.Lookup(t, s, "globex-demo", "invoice-worker", "staging")
}

func login(t *testing.T, base, email string) client {
	t.Helper()
	c := client{t: t, base: base}
	code, out := c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": testutil.Password}, nil)
	if code != http.StatusOK {
		t.Fatalf("login %s: %d %v", email, code, out)
	}
	c.token = out["token"].(string)
	return c
}

func deployBody(tg testutil.Target, version string) map[string]string {
	return map[string]string{"service_id": tg.Service, "environment_id": tg.Env, "version": version, "failure_profile": "healthy"}
}

func TestMeReturnsOrganizationAndRole(t *testing.T) {
	base, _, _ := newServer(t)
	c := login(t, base, "viewer@acme.example")
	code, me := c.do("GET", "/api/v1/me", nil, nil)
	if code != http.StatusOK || me["organization"] != "Acme Demo" || me["role"] != "VIEWER" || me["email"] != "viewer@acme.example" {
		t.Fatalf("me: %d %v", code, me)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	base, _, _ := newServer(t)
	c := client{t: t, base: base}
	code, out := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "operator@acme.example", "password": "nope"}, nil)
	if code != http.StatusUnauthorized || out["error"] == nil {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestManualDeployIsIdempotent(t *testing.T) {
	base, acme, _ := newServer(t)
	c := login(t, base, "operator@acme.example")
	h := map[string]string{"Idempotency-Key": uuid.NewString()}
	code, first := c.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), h)
	if code != http.StatusAccepted || first["status"] != "queued" {
		t.Fatalf("create: %d %v", code, first)
	}
	code, again := c.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), h)
	if code != http.StatusOK || again["id"] != first["id"] {
		t.Fatalf("replay: %d %v", code, again)
	}
	code, _ = c.do("POST", "/api/v1/deployments", deployBody(acme, "v2.0.0"), h)
	if code != http.StatusConflict {
		t.Fatalf("key reuse with another body: %d", code)
	}
	code, _ = c.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("missing Idempotency-Key: %d", code)
	}
	code, _ = c.do("POST", "/api/v1/deployments", deployBody(acme, "not a version!"), map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid version: %d", code)
	}
}

func TestApprovalGateReleasesDeployment(t *testing.T) {
	base, acme, _ := newServer(t)
	e := login(t, base, "operator@acme.example")

	body := map[string]any{"service_id": acme.Service, "environment_id": acme.Env, "version": "v2.0.0",
		"failure_profile": "healthy", "requires_approval": true}
	code, d := e.do("POST", "/api/v1/deployments", body, map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusAccepted || d["status"] != "awaiting_approval" || d["requires_approval"] != true {
		t.Fatalf("create: %d %v", code, d)
	}
	id := d["id"].(string)

	// Viewers cannot approve; engineers can.
	v := login(t, base, "viewer@acme.example")
	if code, _ := v.do("POST", "/api/v1/deployments/"+id+"/approve", nil, nil); code != http.StatusForbidden {
		t.Fatalf("viewer approve: %d", code)
	}
	code, a := e.do("POST", "/api/v1/deployments/"+id+"/approve", nil, nil)
	if code != http.StatusOK || a["status"] != "queued" || a["approved_by"] != "operator@acme.example" {
		t.Fatalf("approve: %d %v", code, a)
	}
	if code, _ := e.do("POST", "/api/v1/deployments/"+id+"/approve", nil, nil); code != http.StatusConflict {
		t.Fatalf("second approval: %d", code)
	}
}

func TestRetryClonesTerminalDeployment(t *testing.T) {
	base, acme, _ := newServer(t)
	e := login(t, base, "operator@acme.example")
	v := login(t, base, "viewer@acme.example")

	code, d := e.do("POST", "/api/v1/deployments", deployBody(acme, "v3.0.0"), map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusAccepted {
		t.Fatalf("create: %d", code)
	}
	id := d["id"].(string)
	// A live deployment cannot be retried; cancel it first.
	if code, _ := e.do("POST", "/api/v1/deployments/"+id+"/retry", nil, nil); code != http.StatusConflict {
		t.Fatalf("retry queued: %d", code)
	}
	if code, _ := e.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if code, _ := v.do("POST", "/api/v1/deployments/"+id+"/retry", nil, nil); code != http.StatusForbidden {
		t.Fatalf("viewer retry: %d", code)
	}
	code, r := e.do("POST", "/api/v1/deployments/"+id+"/retry", nil, nil)
	if code != http.StatusAccepted || r["status"] != "queued" || r["source"] != "retry" || r["version"] != "v3.0.0" || r["id"] == id {
		t.Fatalf("retry: %d %v", code, r)
	}
	// A second retry returns the same clone.
	code, again := e.do("POST", "/api/v1/deployments/"+id+"/retry", nil, nil)
	if code != http.StatusOK || again["id"] != r["id"] {
		t.Fatalf("second retry: %d %v", code, again)
	}
}

func TestViewerCannotDeploy(t *testing.T) {
	base, acme, _ := newServer(t)
	v := login(t, base, "viewer@acme.example")
	code, out := v.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusForbidden || out["error"].(map[string]any)["code"] != "forbidden" {
		t.Fatalf("viewer deploy: %d %v", code, out)
	}
	if code, _ := v.do("GET", "/api/v1/services", nil, nil); code != http.StatusOK {
		t.Fatalf("viewer read: %d", code)
	}
}

func TestTenantsCannotSeeEachOther(t *testing.T) {
	base, acme, globex := newServer(t)
	a := login(t, base, "operator@acme.example")
	g := login(t, base, "operator@globex.example")
	_, d := a.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), map[string]string{"Idempotency-Key": uuid.NewString()})
	id := d["id"].(string)
	if code, _ := g.do("GET", "/api/v1/deployments/"+id, nil, nil); code != http.StatusNotFound {
		t.Errorf("cross-tenant read: %d", code)
	}
	if code, _ := g.do("GET", "/api/v1/services/"+acme.Service, nil, nil); code != http.StatusNotFound {
		t.Errorf("cross-tenant service read: %d", code)
	}
	if code, _ := g.do("POST", "/api/v1/deployments", deployBody(acme, "v1.0.0"), map[string]string{"Idempotency-Key": uuid.NewString()}); code != http.StatusNotFound {
		t.Errorf("cross-tenant deploy: %d", code)
	}
	mixed := map[string]string{"service_id": globex.Service, "environment_id": acme.Env, "version": "v1.0.0"}
	if code, _ := g.do("POST", "/api/v1/deployments", mixed, map[string]string{"Idempotency-Key": uuid.NewString()}); code != http.StatusNotFound {
		t.Errorf("environment from another tenant: %d", code)
	}
	if code, _ := g.do("POST", "/api/v1/deployments/"+id+"/rollback", map[string]string{"reason": "x"}, map[string]string{"Idempotency-Key": uuid.NewString()}); code != http.StatusNotFound {
		t.Errorf("cross-tenant rollback: %d", code)
	}
	_, list := g.do("GET", "/api/v1/deployments", nil, nil)
	if items, _ := list["deployments"].([]any); len(items) != 0 {
		t.Errorf("globex sees deployments: %v", list)
	}
}

func signed(body []byte) string {
	m := hmac.New(sha256.New, []byte(webhookSecret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func TestGitHubDeploymentWebhook(t *testing.T) {
	base, _, _ := newServer(t)
	anon := client{t: t, base: base}
	hook := func(event, delivery string, payload any) (int, map[string]any) {
		raw, _ := json.Marshal(payload)
		return anon.do("POST", "/api/v1/webhooks/github", raw, map[string]string{
			"X-GitHub-Event": event, "X-GitHub-Delivery": delivery, "X-Hub-Signature-256": signed(raw)})
	}
	payload := map[string]any{
		"deployment": map[string]any{"ref": "v3.1.0", "sha": "0123456789abcdef", "environment": "staging"},
		"repository": map[string]any{"full_name": "acme-demo/checkout-api"},
		"sender":     map[string]any{"login": "octo-demo"},
	}
	code, out := hook("deployment", "delivery-1", payload)
	if code != http.StatusAccepted || out["deployment_id"] == "" || out["duplicate"] != false {
		t.Fatalf("deployment event: %d %v", code, out)
	}
	code, dup := hook("deployment", "delivery-1", payload)
	if code != http.StatusOK || dup["deployment_id"] != out["deployment_id"] || dup["duplicate"] != true {
		t.Fatalf("redelivery: %d %v", code, dup)
	}
	if code, _ := hook("push", "delivery-2", map[string]any{"ref": "refs/heads/main"}); code != http.StatusAccepted {
		t.Fatalf("push: %d", code)
	}
	payload["repository"] = map[string]any{"full_name": "someone/unknown"}
	if code, _ := hook("deployment", "delivery-3", payload); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown repository: %d", code)
	}

	c := login(t, base, "operator@acme.example")
	_, d := c.do("GET", "/api/v1/deployments/"+out["deployment_id"].(string), nil, nil)
	if d["source"] != "github" || d["version"] != "v3.1.0" || d["actor"] != "github:octo-demo" {
		t.Fatalf("webhook deployment: %v", d)
	}
	_, all := c.do("GET", "/api/v1/deployments", nil, nil)
	if list, _ := all["deployments"].([]any); len(list) != 1 {
		t.Fatalf("push/unknown events must not create deployments; have %v", all)
	}
}

func TestErrorsCarryCorrelationID(t *testing.T) {
	base, _, _ := newServer(t)
	c := login(t, base, "operator@acme.example")
	corr := uuid.NewString()
	code, out := c.do("GET", "/api/v1/deployments/"+uuid.NewString(), nil, map[string]string{"X-Correlation-ID": corr})
	if code != http.StatusNotFound || out["error"].(map[string]any)["correlation_id"] != corr {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestCanaryWeightsAreValidatedAndStored(t *testing.T) {
	base, acme, _ := newServer(t)
	c := login(t, base, "operator@acme.example")

	canaryBody := func(version string, w []int) map[string]any {
		b := map[string]any{"service_id": acme.Service, "environment_id": acme.Env,
			"version": version, "failure_profile": "healthy", "canary": w}
		return b
	}
	for _, w := range [][]int{{0}, {100}, {50, 50}, {90, 10}} {
		code, _ := c.do("POST", "/api/v1/deployments", canaryBody("v9.9.9", w),
			map[string]string{"Idempotency-Key": uuid.NewString()})
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("canary %v: got %d, want 422", w, code)
		}
	}

	body := canaryBody("v1.2.3", []int{10, 50})
	code, d := c.do("POST", "/api/v1/deployments", body, map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %v", code, d)
	}
	code, got := c.do("GET", "/api/v1/deployments/"+d["id"].(string), nil, nil)
	if code != http.StatusOK {
		t.Fatalf("get: %d", code)
	}
	var names []string
	for _, st := range got["steps"].([]any) {
		names = append(names, st.(map[string]any)["name"].(string))
	}
	want := "validate,apply_version,health_probe,canary_10,canary_50,promote"
	if strings.Join(names, ",") != want {
		t.Fatalf("steps = %v, want %v", names, want)
	}
}
