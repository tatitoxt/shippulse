package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tatitoxt/shippulse/internal/domain"
)

func nomadServer(t *testing.T, handler http.HandlerFunc) (*Nomad, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	n := NewNomad(srv.URL, "tok", "registry.acme/")
	return n, srv
}

var tgt = Target{Tenant: "org1", Service: "web", Environment: "prod"}

func TestNomadApplyRegistersJob(t *testing.T) {
	var gotPath, gotMethod, gotToken string
	var got map[string]any
	n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotToken = r.URL.Path, r.Method, r.Header.Get("X-Nomad-Token")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	})
	if err := n.Apply(context.Background(), tgt, "1.2.3", domain.ProfileHealthy); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/jobs" {
		t.Fatalf("request: %s %s", gotMethod, gotPath)
	}
	if gotToken != "tok" {
		t.Fatalf("token: %q", gotToken)
	}
	job := got["Job"].(map[string]any)
	if job["ID"] != "web-prod" {
		t.Fatalf("job id: %v", job["ID"])
	}
	if job["Meta"].(map[string]any)["version"] != "1.2.3" {
		t.Fatalf("meta: %v", job["Meta"])
	}
	task := job["TaskGroups"].([]any)[0].(map[string]any)["Tasks"].([]any)[0].(map[string]any)
	if task["Config"].(map[string]any)["image"] != "registry.acme/web:1.2.3" {
		t.Fatalf("image: %v", task["Config"])
	}
}

func TestNomadProbe(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   error
	}{
		{"running", nil},
		{"pending", ErrUnhealthy},
		{"dead", ErrUnhealthy},
	} {
		n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"Status": tc.status})
		})
		if err := n.Probe(context.Background(), tgt); err != tc.want {
			t.Fatalf("status %s: got %v want %v", tc.status, err, tc.want)
		}
	}
}

func TestNomadProbeMissingJob(t *testing.T) {
	n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if err := n.Probe(context.Background(), tgt); err != ErrUnhealthy {
		t.Fatalf("got %v want ErrUnhealthy", err)
	}
}

func TestNomadSetTrafficPromotesAt100(t *testing.T) {
	var promoted string
	n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			json.NewEncoder(w).Encode([]map[string]string{{"ID": "dep-9"}})
		case strings.HasPrefix(r.URL.Path, "/v1/deployment/promote/"):
			promoted = strings.TrimPrefix(r.URL.Path, "/v1/deployment/promote/")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if err := n.SetTraffic(context.Background(), tgt, 50); err != nil || promoted != "" {
		t.Fatalf("bake step should not promote: err=%v promoted=%q", err, promoted)
	}
	if err := n.SetTraffic(context.Background(), tgt, 100); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if promoted != "dep-9" {
		t.Fatalf("promoted %q want dep-9", promoted)
	}
}

func TestNomadSetTrafficNoDeployment(t *testing.T) {
	n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	})
	if err := n.SetTraffic(context.Background(), tgt, 100); err != nil {
		t.Fatalf("no deployment in flight should be fine: %v", err)
	}
}

func TestNomadStatus(t *testing.T) {
	n, _ := nomadServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"Status": "running", "Meta": map[string]string{"version": "1.2.3"}})
	})
	st, err := n.Status(context.Background(), tgt)
	if err != nil || st.Version != "1.2.3" {
		t.Fatalf("status: %+v err=%v", st, err)
	}
}

func TestNomadUnreachable(t *testing.T) {
	n := NewNomad("http://127.0.0.1:1", "", "")
	if err := n.Probe(context.Background(), tgt); err == nil {
		t.Fatal("expected error")
	}
}
