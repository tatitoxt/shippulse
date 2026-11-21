package ghstatus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tatitoxt/shippulse/internal/domain"
)

func TestStateFor(t *testing.T) {
	cases := map[domain.DeploymentStatus]string{
		domain.DeploymentQueued:    "pending",
		domain.DeploymentRunning:   "pending",
		domain.DeploymentSucceeded: "success",
		domain.DeploymentFailed:    "failure",
		domain.DeploymentCancelled: "error",
	}
	for status, want := range cases {
		if got := StateFor(status); got != want {
			t.Errorf("StateFor(%s) = %q, want %q", status, got, want)
		}
	}
}

func TestReportPostsCommitStatus(t *testing.T) {
	var gotURL, gotAuth string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	r := &Reporter{Token: "tok", BaseURL: srv.URL, TargetBase: "https://pulse.example"}
	err := r.Report(context.Background(), "acme/api", "abc123", "production", "dep-1", "pending", "deploying")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if gotURL != "/repos/acme/api/statuses/abc123" {
		t.Fatalf("posted to %q", gotURL)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth header %q", gotAuth)
	}
	if body["state"] != "pending" || body["context"] != "shippulse/production" ||
		body["target_url"] != "https://pulse.example/deployments/dep-1" || body["description"] != "deploying" {
		t.Fatalf("unexpected body %v", body)
	}
}

func TestReportNoOpWhenDisabledOrMissingFields(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	r := &Reporter{Token: "tok", BaseURL: srv.URL}
	for _, tc := range [][2]string{
		{"", "abc123"},   // no repo
		{"acme/api", ""}, // no sha
	} {
		if err := r.Report(context.Background(), tc[0], tc[1], "prod", "d", "pending", "x"); err != nil {
			t.Fatalf("Report(%v): %v", tc, err)
		}
	}
	disabled := &Reporter{BaseURL: srv.URL} // no token
	if err := disabled.Report(context.Background(), "acme/api", "sha", "prod", "d", "pending", "x"); err != nil {
		t.Fatalf("disabled Report: %v", err)
	}
	if called {
		t.Fatal("no request should have been made")
	}
}

func TestReportSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	defer srv.Close()

	r := &Reporter{Token: "bad", BaseURL: srv.URL}
	err := r.Report(context.Background(), "acme/api", "sha", "prod", "d", "success", "x")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected HTTP 403 error, got %v", err)
	}
}
