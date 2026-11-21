// Package provider abstracts the system that actually runs a service version.
// The only implementation shipped is the simulated runtime (see
// internal/simruntime); real providers are out of scope for this slice.
package provider

import (
	"context"
	"errors"
	"time"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/simruntime"
)

var (
	ErrUnavailable = errors.New("provider unavailable")
	ErrUnhealthy   = errors.New("health check failed")
)

// Target identifies one runtime. Tenant is the organization id.
type Target struct {
	Tenant      string
	Service     string
	Environment string
}

type Provider interface {
	// Apply makes version the running version of target. It must be idempotent.
	Apply(ctx context.Context, t Target, version string, profile domain.FailureProfile) error
	// Probe performs one health check against the running version.
	Probe(ctx context.Context, t Target) error
	// SetTraffic routes percent of traffic to the running version (canary).
	SetTraffic(ctx context.Context, t Target, percent int) error
	Status(ctx context.Context, t Target) (simruntime.Status, error)
}

// Memory runs the simulated runtime in-process. Used by unit tests.
type Memory struct {
	RT *simruntime.Runtime
	// FailApply, when set, is consulted before every Apply (test fault injection).
	FailApply func(t Target, version string) error
}

func NewMemory() *Memory { return &Memory{RT: simruntime.New()} }

func (m *Memory) Apply(_ context.Context, t Target, version string, profile domain.FailureProfile) error {
	if m.FailApply != nil {
		if err := m.FailApply(t, version); err != nil {
			return err
		}
	}
	_, err := m.RT.Apply(t.Tenant, t.Service, t.Environment, version, profile)
	if errors.Is(err, simruntime.ErrUnavailable) {
		return ErrUnavailable
	}
	return err
}

func (m *Memory) SetTraffic(_ context.Context, t Target, percent int) error {
	return m.RT.SetTraffic(t.Tenant, t.Service, t.Environment, percent)
}

func (m *Memory) Probe(ctx context.Context, t Target) error {
	err := m.RT.Health(ctx, t.Tenant, t.Service, t.Environment)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	default:
		return ErrUnhealthy
	}
}

func (m *Memory) Status(_ context.Context, t Target) (simruntime.Status, error) {
	return m.RT.Status(t.Tenant, t.Service, t.Environment)
}

// ProbeConfig controls the health probe loop.
type ProbeConfig struct {
	Attempts int
	Timeout  time.Duration // per attempt
	Interval time.Duration
}

// ProbeUntilHealthy retries a probe and returns nil on the first healthy
// answer, or the error code describing the last failure.
func ProbeUntilHealthy(ctx context.Context, p Provider, t Target, cfg ProbeConfig) (attempts int, code string, err error) {
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	for attempts = 1; attempts <= cfg.Attempts; attempts++ {
		actx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		err = p.Probe(actx, t)
		cancel()
		if err == nil {
			return attempts, "", nil
		}
		if ctx.Err() != nil {
			return attempts, domain.CodeHealthCheckTimeout, ctx.Err()
		}
		code = domain.CodeHealthCheckFailed
		if errors.Is(err, context.DeadlineExceeded) {
			code = domain.CodeHealthCheckTimeout
		}
		if attempts < cfg.Attempts {
			select {
			case <-ctx.Done():
				return attempts, code, ctx.Err()
			case <-time.After(cfg.Interval):
			}
		}
	}
	return cfg.Attempts, code, err
}
