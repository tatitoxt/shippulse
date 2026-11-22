// Package api is the HTTP control plane consumed by the web console and GitHub.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/tatitoxt/shippulse/internal/auth"
	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/ghstatus"
	"github.com/tatitoxt/shippulse/internal/metrics"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/store"
)

type Server struct {
	Store         *store.Store
	Auth          *auth.Issuer
	Runtime       provider.Provider  // used read-only to show the observed runtime
	WebhookSecret string             // GitHub webhook HMAC secret; empty disables the endpoint
	Statuses      *ghstatus.Reporter // best-effort GitHub commit statuses; nil/disabled = no-op
	Metrics       *metrics.Exporter  // Prometheus exporter; nil disables /metrics
	Log           *slog.Logger

	notifier     *notifier
	notifierOnce sync.Once
	loginLimiter *loginLimiter
}

// events returns the shared change notifier, started lazily on the first SSE
// request so servers without streaming clients never open a LISTEN connection.
func (s *Server) events() *notifier {
	s.notifierOnce.Do(func() { s.notifier = newNotifier(s.Store.Pool, s.Log) })
	return s.notifier
}

type ctxKey int

const (
	keyCorrelation ctxKey = iota
	keyPrincipal
)

func (s *Server) Router() http.Handler {
	s.loginLimiter = &loginLimiter{fails: make(map[string][]time.Time)}
	r := chi.NewRouter()
	r.Use(middleware.RealIP, s.correlation, s.accessLog, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Get("/readyz", s.ready)
	if s.Metrics != nil {
		r.Get("/metrics", s.Metrics.Handler().ServeHTTP)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Post("/webhooks/github", s.githubWebhook)

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/me", s.me)
			r.Get("/services", s.listServices)
			r.Get("/services/{id}", s.getService)
			r.Get("/services/{id}/releases", s.listReleases)
			r.Get("/services/{id}/runtime", s.serviceRuntime)
			r.Get("/deployments", s.listDeployments)
			r.Get("/deployments/{id}", s.getDeployment)
			r.Get("/audit-events", s.listAudit)
			r.Get("/events", s.streamEvents)
			r.Group(func(r chi.Router) {
				r.Use(requireRole(domain.RoleEngineer))
				r.Post("/deployments", s.createDeployment)
				r.Post("/deployments/{id}/rollback", s.requestRollback)
				r.Post("/deployments/{id}/cancel", s.cancelDeployment)
				r.Post("/deployments/{id}/approve", s.approveDeployment)
				r.Post("/deployments/{id}/retry", s.retryDeployment)
			})
		})
	})
	return r
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Pool.Ping(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "database unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyCorrelation, id)))
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if s.Metrics != nil {
			s.Metrics.ObserveRequest(r.Method, ww.Status())
		}
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(), "correlation_id", correlationID(r))
	})
}

func correlationID(r *http.Request) string {
	id, _ := r.Context().Value(keyCorrelation).(string)
	return id
}

func principal(r *http.Request) store.Principal {
	p, _ := r.Context().Value(keyPrincipal).(store.Principal)
	return p
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
			return
		}
		claims, err := s.Auth.Verify(h[len(prefix):])
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "invalid or expired token")
			return
		}
		p, err := s.Store.Membership(r.Context(), claims.UserID, claims.OrgID)
		if errors.Is(err, domain.ErrForbidden) {
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "membership revoked")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, p)))
	})
}

func requireRole(min domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !domain.Role(principal(r).Role).Allows(min) {
				writeError(w, r, http.StatusForbidden, "forbidden", "role "+principal(r).Role+" cannot perform this action")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type errorBody struct {
	Error struct {
		Code          string `json:"code"`
		Message       string `json:"message"`
		CorrelationID string `json:"correlation_id"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	var b errorBody
	b.Error.Code, b.Error.Message, b.Error.CorrelationID = code, msg, correlationID(r)
	writeJSON(w, status, b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("internal error", "err", err, "path", r.URL.Path, "correlation_id", correlationID(r))
	writeError(w, r, http.StatusInternalServerError, "internal", "internal error")
}

// domainError maps store/domain errors to HTTP responses.
func (s *Server) domainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "forbidden")
	default:
		s.internal(w, r, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
		return false
	}
	return true
}
