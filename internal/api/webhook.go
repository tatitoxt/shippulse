package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// VerifySignature checks GitHub's X-Hub-Signature-256 header in constant time.
func VerifySignature(secret string, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || secret == "" {
		return false
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

type githubDeploymentEvent struct {
	Deployment struct {
		ID          int64  `json:"id"`
		SHA         string `json:"sha"`
		Ref         string `json:"ref"`
		Environment string `json:"environment"`
		Payload     struct {
			FailureProfile string `json:"failure_profile"`
		} `json:"payload"`
	} `json:"deployment"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

// githubWebhook accepts signed GitHub "deployment" events. "ping" is
// acknowledged; every other event type (including "push") is ignored, so a
// push never starts a deployment by itself.
func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.WebhookSecret == "" {
		writeError(w, r, http.StatusServiceUnavailable, "webhook_disabled", "GITHUB_WEBHOOK_SECRET is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "payload too large")
		return
	}
	if !VerifySignature(s.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, r, http.StatusUnauthorized, "invalid_signature", "signature verification failed")
		return
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery == "" || len(delivery) > 100 {
		writeError(w, r, http.StatusBadRequest, "missing_delivery", "X-GitHub-Delivery header is required")
		return
	}
	switch event := r.Header.Get("X-GitHub-Event"); event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"status": "pong"})
		return
	case "deployment":
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "ignored", "event": event})
		return
	}

	var ev githubDeploymentEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.Repository.FullName == "" || ev.Deployment.Environment == "" {
		writeError(w, r, http.StatusBadRequest, "invalid_payload", "deployment event is missing repository or environment")
		return
	}
	version := ev.Deployment.Ref
	if domain.ValidateVersion(version) != nil {
		version = ev.Deployment.SHA
		if len(version) > 12 {
			version = version[:12]
		}
	}
	if err := domain.ValidateVersion(version); err != nil {
		s.domainError(w, r, err)
		return
	}
	profile, err := domain.ParseFailureProfile(ev.Deployment.Payload.FailureProfile)
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	org, serviceID, envID, err := s.Store.ResolveRepositoryTarget(r.Context(), ev.Repository.FullName, ev.Deployment.Environment)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, r, http.StatusUnprocessableEntity, "unknown_target", "repository/environment is not registered")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	actor := "github"
	if ev.Sender.Login != "" {
		actor = "github:" + ev.Sender.Login
	}
	ctx, span := telemetry.Tracer().Start(r.Context(), "api.github.deployment",
		trace.WithAttributes(attribute.String("github.delivery", delivery),
			attribute.String("github.sha", ev.Deployment.SHA)))
	defer span.End()
	d, created, err := s.Store.CreateDeployment(ctx, store.CreateDeploymentInput{
		OrganizationID: org, ServiceID: serviceID, EnvironmentID: envID, Version: version,
		FailureProfile: string(profile), IdempotencyKey: "github:" + delivery, Source: "github",
		Actor: actor, GitSHA: ev.Deployment.SHA, CorrelationID: correlationID(r),
	})
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"status": "queued", "deployment_id": d.ID, "duplicate": !created})
}
