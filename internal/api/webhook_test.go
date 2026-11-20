package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"a":1}`)
	if !VerifySignature("s3cret", body, sign("s3cret", string(body))) {
		t.Fatal("valid signature rejected")
	}
	for _, h := range []string{"", "sha1=abc", "sha256=zz", sign("other", string(body))} {
		if VerifySignature("s3cret", body, h) {
			t.Errorf("signature %q accepted", h)
		}
	}
	if VerifySignature("", body, sign("", string(body))) {
		t.Error("empty secret must never verify")
	}
}

// These cases are rejected or ignored before the store is touched.
func TestWebhookRejectsBeforePersistence(t *testing.T) {
	s := &Server{WebhookSecret: "s3cret", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.Router()
	body := `{"zen":"hi"}`
	cases := []struct {
		name, event, sig, delivery string
		want                       int
	}{
		{"bad signature", "ping", "sha256=00", "d1", http.StatusUnauthorized},
		{"missing delivery", "ping", sign("s3cret", body), "", http.StatusBadRequest},
		{"ping", "ping", sign("s3cret", body), "d2", http.StatusOK},
		{"push is ignored", "push", sign("s3cret", body), "d3", http.StatusAccepted},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body))
		req.Header.Set("X-GitHub-Event", c.event)
		req.Header.Set("X-Hub-Signature-256", c.sig)
		if c.delivery != "" {
			req.Header.Set("X-GitHub-Delivery", c.delivery)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
		}
	}
	disabled := (&Server{Log: s.Log}).Router()
	rec := httptest.NewRecorder()
	disabled.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("webhook without secret: got %d", rec.Code)
	}
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/deployments", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rec.Code)
	}
	if rec.Header().Get("X-Correlation-ID") == "" || !strings.Contains(rec.Body.String(), "correlation_id") {
		t.Fatal("error responses must carry a correlation id")
	}
}
