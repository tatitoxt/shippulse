// Package simruntime is a deterministic in-memory stand-in for a deployment
// target. It never touches real infrastructure: "applying" a version stores it,
// and the health endpoint answers according to the version's failure profile.
package simruntime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
)

var (
	ErrUnavailable = errors.New("simulated provider unavailable")
	ErrUnhealthy   = errors.New("simulated runtime unhealthy")
	ErrNoRuntime   = errors.New("no version applied yet")
)

type Event struct {
	Version   string    `json:"version"`
	Profile   string    `json:"profile"`
	AppliedAt time.Time `json:"applied_at"`
}

type Status struct {
	Tenant      string    `json:"tenant"`
	Service     string    `json:"service"`
	Environment string    `json:"environment"`
	Version     string    `json:"version"`
	Profile     string    `json:"profile"`
	AppliedAt   time.Time `json:"applied_at"`
	Applies     int       `json:"applies"`
	History     []Event   `json:"history"`
}

type Runtime struct {
	mu    sync.Mutex
	state map[string]*Status
	now   func() time.Time
	// HangLimit bounds how long the timeout profile blocks a health check.
	HangLimit time.Duration
}

func New() *Runtime {
	return &Runtime{state: map[string]*Status{}, now: func() time.Time { return time.Now().UTC() }, HangLimit: 30 * time.Second}
}

// Runtimes are keyed by tenant as well, so two organizations that use the same
// service and environment slugs never share a runtime.
func key(tenant, service, env string) string { return tenant + "/" + service + "/" + env }

// Apply sets the running version. Re-applying the same version and profile is
// a no-op, which makes retries and duplicate jobs safe.
func (r *Runtime) Apply(tenant, service, env, version string, profile domain.FailureProfile) (changed bool, err error) {
	if profile == domain.ProfileProviderUnavailable {
		return false, ErrUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.state[key(tenant, service, env)]
	if !ok {
		st = &Status{Tenant: tenant, Service: service, Environment: env}
		r.state[key(tenant, service, env)] = st
	}
	if st.Version == version && st.Profile == string(profile) {
		return false, nil
	}
	now := r.now()
	st.Version, st.Profile, st.AppliedAt = version, string(profile), now
	st.Applies++
	st.History = append(st.History, Event{Version: version, Profile: string(profile), AppliedAt: now})
	return true, nil
}

func (r *Runtime) Status(tenant, service, env string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.state[key(tenant, service, env)]
	if !ok {
		return Status{}, ErrNoRuntime
	}
	cp := *st
	cp.History = append([]Event(nil), st.History...)
	return cp, nil
}

// Health answers a probe for the running version.
func (r *Runtime) Health(ctx context.Context, tenant, service, env string) error {
	st, err := r.Status(tenant, service, env)
	if err != nil {
		return err
	}
	switch domain.FailureProfile(st.Profile) {
	case domain.ProfileUnhealthy:
		return ErrUnhealthy
	case domain.ProfileTimeout:
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.HangLimit):
			return context.DeadlineExceeded
		}
	default:
		return nil
	}
}
