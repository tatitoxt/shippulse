package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tatitoxt/shippulse/internal/auth"
	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/simruntime"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// dummyHash keeps login timing similar for unknown emails.
var dummyHash, _ = auth.HashPassword("not-a-real-password")

// loginLimiter throttles failed sign-ins per client IP + email so the demo is
// not a free credential-stuffing oracle. In-memory like the rest of the demo
// state; a fleet deployment would move this to Redis.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const (
	loginMaxFailures = 5
	loginWindow      = 5 * time.Minute
)

// blocked reports whether key already hit the failure cap inside the window.
func (l *loginLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-loginWindow)
	kept := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.fails[key] = kept
	return len(kept) >= loginMaxFailures
}

func (l *loginLimiter) record(key string, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ok {
		delete(l.fails, key)
		return
	}
	l.fails[key] = append(l.fails[key], time.Now())
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	key := r.RemoteAddr + "|" + email
	if s.loginLimiter.blocked(key) {
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many failed sign-ins; try again later")
		return
	}
	p, hash, err := s.Store.UserCredentials(r.Context(), email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		s.internal(w, r, err)
		return
	}
	if err != nil {
		hash = dummyHash
	}
	if !auth.CheckPassword(hash, body.Password) || errors.Is(err, domain.ErrNotFound) {
		s.loginLimiter.record(key, false)
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	s.loginLimiter.record(key, true)
	token, err := s.Auth.Issue(p.UserID, p.OrganizationID, p.Email)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": p})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, principal(r))
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListServices(r.Context(), principal(r).OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": list})
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	sv, err := s.Store.GetService(r.Context(), principal(r).OrganizationID, chi.URLParam(r, "id"))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sv)
}

func (s *Server) listReleases(w http.ResponseWriter, r *http.Request) {
	org := principal(r).OrganizationID
	sv, err := s.Store.GetService(r.Context(), org, chi.URLParam(r, "id"))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	list, err := s.Store.ListDeployments(r.Context(), org, store.ListFilter{ServiceID: sv.ID, Limit: 100})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": sv, "releases": list})
}

// serviceRuntime reports what the simulated provider is actually running,
// which can differ from the promoted version while a deployment is in flight.
func (s *Server) serviceRuntime(w http.ResponseWriter, r *http.Request) {
	sv, err := s.Store.GetService(r.Context(), principal(r).OrganizationID, chi.URLParam(r, "id"))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	type envRuntime struct {
		Environment   string             `json:"environment"`
		StableVersion *string            `json:"stable_version"`
		Observed      *simruntime.Status `json:"observed"`
		ProviderError string             `json:"provider_error,omitempty"`
	}
	out := []envRuntime{}
	for _, e := range sv.Environments {
		er := envRuntime{Environment: e.EnvironmentSlug, StableVersion: e.CurrentVersion}
		st, err := s.Runtime.Status(r.Context(), provider.Target{Tenant: sv.OrganizationID, Service: sv.Slug, Environment: e.EnvironmentSlug})
		switch {
		case err == nil:
			er.Observed = &st
		case errors.Is(err, simruntime.ErrNoRuntime):
		default:
			er.ProviderError = "provider unavailable"
		}
		out = append(out, er)
	}
	writeJSON(w, http.StatusOK, map[string]any{"service_id": sv.ID, "runtimes": out})
}

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.Store.ListDeployments(r.Context(), principal(r).OrganizationID,
		store.ListFilter{ServiceID: r.URL.Query().Get("service_id"), Limit: limit})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": list})
}

func (s *Server) getDeployment(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.GetDeployment(r.Context(), principal(r).OrganizationID, chi.URLParam(r, "id"))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServiceID      string `json:"service_id"`
		EnvironmentID  string `json:"environment_id"`
		Version        string `json:"version"`
		FailureProfile string `json:"failure_profile"`
		Canary         []int  `json:"canary"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if err := domain.ValidateIdempotencyKey(key); err != nil {
		s.domainError(w, r, err)
		return
	}
	if err := domain.ValidateVersion(body.Version); err != nil {
		s.domainError(w, r, err)
		return
	}
	profile, err := domain.ParseFailureProfile(body.FailureProfile)
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	if len(body.Canary) > 0 {
		if err := domain.ValidateCanaryWeights(body.Canary); err != nil {
			s.domainError(w, r, fmt.Errorf("%w: %s", domain.ErrValidation, err.Error()))
			return
		}
	}
	p := principal(r)
	ctx, span := telemetry.Tracer().Start(r.Context(), "api.deployment.create",
		trace.WithAttributes(
			attribute.String("shippulse.service_id", body.ServiceID),
			attribute.String("shippulse.environment_id", body.EnvironmentID),
			attribute.String("shippulse.version", body.Version)))
	defer span.End()
	d, created, err := s.Store.CreateDeployment(ctx, store.CreateDeploymentInput{
		OrganizationID: p.OrganizationID, ServiceID: body.ServiceID, EnvironmentID: body.EnvironmentID,
		Version: body.Version, FailureProfile: string(profile), IdempotencyKey: key,
		Source: "manual", Actor: p.Email, Canary: body.Canary, CorrelationID: correlationID(r),
	})
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, d)
}

func (s *Server) requestRollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if err := domain.ValidateIdempotencyKey(key); err != nil {
		s.domainError(w, r, err)
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "manual rollback"
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	p := principal(r)
	rb, created, err := s.Store.RequestRollback(r.Context(), p.OrganizationID, chi.URLParam(r, "id"), p.Email, key, reason, correlationID(r))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, rb)
}

func (s *Server) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	d, err := s.Store.CancelDeployment(r.Context(), p.OrganizationID, chi.URLParam(r, "id"), p.Email, correlationID(r))
	if err != nil {
		s.domainError(w, r, err)
		return
	}
	if s.Statuses != nil && d.GitSHA != nil && d.Repository != nil {
		s.Statuses.ReportLogged(r.Context(), *d.Repository, *d.GitSHA, d.EnvironmentSlug, d.ID,
			"error", fmt.Sprintf("deploy of %s cancelled by %s", d.Version, p.Email))
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.Store.ListAudit(r.Context(), principal(r).OrganizationID, r.URL.Query().Get("subject_id"), limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit_events": list})
}
