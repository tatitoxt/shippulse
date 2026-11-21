//go:build integration

package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tatitoxt/shippulse/internal/api"
	"github.com/tatitoxt/shippulse/internal/auth"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/testutil"
)

func newEventServer(t *testing.T) (string, *store.Store) {
	t.Helper()
	s := testutil.DB(t, "events")
	iss, err := auth.NewIssuer("integration-jwt-secret-0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: s, Auth: iss, Runtime: provider.NewMemory(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts.URL, s
}

// openStream connects to the SSE endpoint and returns a channel of `data:` payloads.
func openStream(t *testing.T, base, token string) chan string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), "GET", base+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				lines <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	return lines
}

func setDeploymentStatus(t *testing.T, s *store.Store, id, status string) {
	t.Helper()
	if _, err := s.Pool.Exec(context.Background(), `UPDATE deployments SET status = $2 WHERE id = $1`, id, status); err != nil {
		t.Fatalf("update status: %v", err)
	}
}

func createDeployment(t *testing.T, c client, tg testutil.Target, version string) string {
	t.Helper()
	code, out := c.do("POST", "/api/v1/deployments", deployBody(tg, version), map[string]string{"Idempotency-Key": "sse-" + version})
	if code != http.StatusAccepted && code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create deployment: %d %v", code, out)
	}
	d, _ := out["deployment"].(map[string]any)
	if d == nil {
		d = out
	}
	id, _ := d["id"].(string)
	if id == "" {
		t.Fatalf("no deployment id in %v", out)
	}
	return id
}

func TestEventStreamPushesDeploymentChanges(t *testing.T) {
	base, s := newEventServer(t)
	acme := testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging")
	c := login(t, base, "operator@acme.example")

	stream := openStream(t, base, c.token)
	id := createDeployment(t, c, acme, "9.9.9")
	setDeploymentStatus(t, s, id, "running")

	deadline := time.After(10 * time.Second)
	for {
		select {
		case line := <-stream:
			var ev map[string]any
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad event %q: %v", line, err)
			}
			if ev["entity"] == "deployment" && ev["id"] == id && ev["status"] == "running" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for deployment event")
		}
	}
}

func TestEventStreamDoesNotLeakAcrossOrgs(t *testing.T) {
	base, s := newEventServer(t)
	acme := testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging")
	globex := testutil.Lookup(t, s, "globex-demo", "invoice-worker", "staging")
	acmeClient := login(t, base, "operator@acme.example")
	globexClient := login(t, base, "operator@globex.example")

	stream := openStream(t, base, acmeClient.token)
	globexID := createDeployment(t, globexClient, globex, "9.9.9")
	setDeploymentStatus(t, s, globexID, "running")
	acmeID := createDeployment(t, acmeClient, acme, "9.9.9")

	// Every deployment event on acme's stream must belong to acme; the globex
	// deployment must never appear.
	deadline := time.After(10 * time.Second)
	seen := map[string]bool{}
	for !seen[acmeID] {
		select {
		case line := <-stream:
			var ev map[string]any
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad event %q: %v", line, err)
			}
			if ev["entity"] != "deployment" {
				continue
			}
			id, _ := ev["id"].(string)
			if id == globexID {
				t.Fatalf("cross-tenant event leaked: %s", line)
			}
			seen[id] = true
		case <-deadline:
			t.Fatal("timed out waiting for acme event")
		}
	}
}

func TestEventStreamRequiresAuth(t *testing.T) {
	base, _ := newEventServer(t)
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/events", base))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
