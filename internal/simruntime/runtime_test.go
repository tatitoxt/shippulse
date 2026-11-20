package simruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
)

func TestApplyIsIdempotent(t *testing.T) {
	rt := New()
	if changed, err := rt.Apply("t1", "api", "staging", "v1", domain.ProfileHealthy); err != nil || !changed {
		t.Fatalf("first apply: changed=%v err=%v", changed, err)
	}
	if changed, _ := rt.Apply("t1", "api", "staging", "v1", domain.ProfileHealthy); changed {
		t.Fatal("re-applying the same version must be a no-op")
	}
	st, _ := rt.Status("t1", "api", "staging")
	if st.Applies != 1 || len(st.History) != 1 {
		t.Fatalf("want 1 apply, got %d (history %d)", st.Applies, len(st.History))
	}
}

func TestProviderUnavailableLeavesRuntimeUnchanged(t *testing.T) {
	rt := New()
	_, _ = rt.Apply("t1", "api", "staging", "v1", domain.ProfileHealthy)
	if _, err := rt.Apply("t1", "api", "staging", "v2", domain.ProfileProviderUnavailable); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if st, _ := rt.Status("t1", "api", "staging"); st.Version != "v1" {
		t.Fatalf("runtime should still run v1, got %s", st.Version)
	}
}

func TestHealthProfiles(t *testing.T) {
	rt := New()
	rt.HangLimit = time.Second
	ctx := context.Background()
	if err := rt.Health(ctx, "t1", "api", "prod"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("want ErrNoRuntime, got %v", err)
	}
	_, _ = rt.Apply("t1", "api", "prod", "v1", domain.ProfileHealthy)
	if err := rt.Health(ctx, "t1", "api", "prod"); err != nil {
		t.Fatalf("healthy: %v", err)
	}
	_, _ = rt.Apply("t1", "api", "prod", "v2", domain.ProfileUnhealthy)
	if err := rt.Health(ctx, "t1", "api", "prod"); !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("unhealthy: %v", err)
	}
	_, _ = rt.Apply("t1", "api", "prod", "v3", domain.ProfileTimeout)
	tctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := rt.Health(tctx, "t1", "api", "prod"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(Handler(New()))
	defer srv.Close()
	put := func(body string) int {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/runtimes/t1/api/staging", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := put(`{"version":"v1","profile":"healthy"}`); code != 200 {
		t.Fatalf("apply: %d", code)
	}
	if code := put(`{"version":"v2","profile":"provider_unavailable"}`); code != 503 {
		t.Fatalf("unavailable: %d", code)
	}
	if code := put(`{"version":"bad version","profile":"healthy"}`); code != 400 {
		t.Fatalf("invalid version: %d", code)
	}
	resp, err := http.Get(srv.URL + "/v1/runtimes/t1/api/staging")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st Status
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.Version != "v1" {
		t.Fatalf("status version: %s", st.Version)
	}
	hr, _ := http.Get(srv.URL + "/v1/runtimes/t1/api/staging/health")
	hr.Body.Close()
	if hr.StatusCode != 200 {
		t.Fatalf("health: %d", hr.StatusCode)
	}
}

func TestRuntimesAreIsolatedPerTenant(t *testing.T) {
	rt := New()
	_, _ = rt.Apply("org-a", "api", "staging", "v1", domain.ProfileHealthy)
	_, _ = rt.Apply("org-b", "api", "staging", "v9", domain.ProfileUnhealthy)
	if st, _ := rt.Status("org-a", "api", "staging"); st.Version != "v1" || st.Applies != 1 {
		t.Fatalf("tenant a runtime changed by tenant b: %+v", st)
	}
	if err := rt.Health(context.Background(), "org-a", "api", "staging"); err != nil {
		t.Fatalf("tenant a health affected by tenant b: %v", err)
	}
	if _, err := rt.Status("org-c", "api", "staging"); !errors.Is(err, ErrNoRuntime) {
		t.Fatalf("unknown tenant must have no runtime, got %v", err)
	}
}
