// Package worker executes deployment and rollback jobs against a provider.
// Jobs may be delivered more than once; the lease-based claim in the store and
// per-step bookkeeping make re-execution safe.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tatitoxt/shippulse/internal/bus"
	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/ghstatus"
	"github.com/tatitoxt/shippulse/internal/metrics"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	WorkerID      string
	Lease         time.Duration
	ApplyAttempts int
	ApplyBackoff  time.Duration
	ApplyTimeout  time.Duration
	Probe         provider.ProbeConfig
}

func DefaultConfig(id string) Config {
	return Config{
		WorkerID: id, Lease: 60 * time.Second,
		ApplyAttempts: 3, ApplyBackoff: 200 * time.Millisecond, ApplyTimeout: 5 * time.Second,
		Probe: provider.ProbeConfig{Attempts: 3, Timeout: time.Second, Interval: 500 * time.Millisecond},
	}
}

type Executor struct {
	Store    *store.Store
	Provider provider.Provider
	// CanaryProbe, when set, evaluates the real error signal after each traffic
	// shift — an unhealthy reading rolls the deployment back like a failed step.
	CanaryProbe metrics.Probe
	Cfg         Config
	Log         *slog.Logger
	Statuses *ghstatus.Reporter // best-effort GitHub commit statuses; nil/disabled = no-op
}

// reportStatus posts a commit status for the deployment when it has a
// repository + git sha. Best-effort: errors are logged inside the reporter.
func (e *Executor) reportStatus(ctx context.Context, d store.Deployment, state, description string) {
	if e.Statuses == nil || d.GitSHA == nil || d.Repository == nil {
		return
	}
	e.Statuses.ReportLogged(ctx, *d.Repository, *d.GitSHA, d.EnvironmentSlug, d.ID, state, description)
}

// Handle dispatches a job event from the bus.
func (e *Executor) Handle(ctx context.Context, ev domain.JobEvent) error {
	ctx, span := telemetry.Tracer().Start(
		telemetry.ExtractTraceParent(ctx, ev.TraceParent), "worker."+ev.Type,
		trace.WithAttributes(
			attribute.String("shippulse.aggregate_id", ev.AggregateID),
			attribute.String("shippulse.correlation_id", ev.CorrelationID),
		))
	defer span.End()
	switch ev.Type {
	case domain.EventDeploymentRequested:
		return e.RunDeployment(ctx, ev.AggregateID)
	case domain.EventRollbackRequested:
		return e.RunRollback(ctx, ev.AggregateID)
	default:
		e.Log.Warn("ignoring unknown job type", "type", ev.Type)
		return nil
	}
}

func target(d store.Deployment) provider.Target {
	return provider.Target{Tenant: d.OrganizationID, Service: d.ServiceSlug, Environment: d.EnvironmentSlug}
}

// RunDeployment executes validate -> apply_version -> health_probe -> promote,
// resuming at the first step that has not succeeded.
func (e *Executor) RunDeployment(ctx context.Context, id string) error {
	owner := e.Cfg.WorkerID
	d, res, err := e.Store.ClaimDeployment(ctx, id, owner, e.Cfg.Lease)
	if err != nil {
		return err
	}
	switch res {
	case store.ClaimDone:
		e.Log.Info("deployment already finished; dropping duplicate", "deployment_id", id)
		return nil
	case store.ClaimBusy:
		return bus.ErrRetryLater
	}
	log := e.Log.With("deployment_id", d.ID, "correlation_id", d.CorrelationID, "version", d.Version)
	log.Info("deployment claimed", "attempt", d.Attempts)
	e.reportStatus(ctx, d, "pending", fmt.Sprintf("deploying %s to %s (attempt %d)", d.Version, d.EnvironmentSlug, d.Attempts))

	done := map[string]bool{}
	for _, st := range d.Steps {
		done[st.Name] = st.Status == string(domain.StepSucceeded)
	}
	err = e.runSteps(ctx, d, done, log)
	if errors.Is(err, store.ErrLeaseLost) {
		log.Warn("lease lost; another worker owns this deployment")
		return nil
	}
	return err
}

// step wraps one workflow step in a span so a deployment trace shows
// validate/apply/probe/promote as children of worker.deployment.requested.
func step(ctx context.Context, name string, fn func(context.Context) error) error {
	sctx, sp := telemetry.Tracer().Start(ctx, "step."+name)
	defer sp.End()
	return fn(sctx)
}

func (e *Executor) runSteps(ctx context.Context, d store.Deployment, done map[string]bool, log *slog.Logger) error {
	owner := e.Cfg.WorkerID
	fail := func(f store.Failure) error {
		rb, err := e.Store.Fail(ctx, d, owner, f)
		if err != nil {
			return err
		}
		log.Warn("deployment failed", "step", f.Step, "error_code", f.Code, "rollback_queued", rb != nil)
		e.reportStatus(ctx, d, "failure", fmt.Sprintf("%s failed at step %s: %s", d.Version, f.Step, f.Code))
		return nil
	}

	if !done[domain.StepValidate] {
		err := step(ctx, domain.StepValidate, func(sctx context.Context) error {
			if err := e.Store.StartStep(sctx, d, owner, domain.StepValidate); err != nil {
				return err
			}
			vd, err := e.Store.ValidateTarget(sctx, d, owner)
			if errors.Is(err, domain.ErrNotFound) {
				return fail(store.Failure{Step: domain.StepValidate, Code: domain.CodeTargetNotFound, Message: err.Error()})
			}
			if err != nil {
				return err
			}
			d = vd
			return nil
		})
		if err != nil {
			return err
		}
	}

	if !done[domain.StepApplyVersion] {
		err := step(ctx, domain.StepApplyVersion, func(sctx context.Context) error {
			if err := e.Store.StartStep(sctx, d, owner, domain.StepApplyVersion); err != nil {
				return err
			}
			tries, err := e.apply(sctx, target(d), d.Version, domain.FailureProfile(d.FailureProfile))
			if err != nil {
				// The provider rejected the change, so the runtime still runs the previous version.
				return fail(store.Failure{Step: domain.StepApplyVersion, Code: domain.CodeProviderUnavailable,
					Message: fmt.Sprintf("provider unavailable after %d attempts", tries)})
			}
			return e.Store.CompleteStep(sctx, d, owner, domain.StepApplyVersion,
				fmt.Sprintf("applied %s to %s/%s (attempts: %d)", d.Version, d.ServiceSlug, d.EnvironmentSlug, tries))
		})
		if err != nil {
			return err
		}
	}

	if !done[domain.StepHealthProbe] {
		err := step(ctx, domain.StepHealthProbe, func(sctx context.Context) error {
			if err := e.Store.StartStep(sctx, d, owner, domain.StepHealthProbe); err != nil {
				return err
			}
			tries, code, _ := provider.ProbeUntilHealthy(sctx, e.Provider, target(d), e.Cfg.Probe)
			if code != "" {
				if sctx.Err() != nil {
					return sctx.Err()
				}
				return fail(store.Failure{Step: domain.StepHealthProbe, Code: code, Rollback: true,
					Message: fmt.Sprintf("%s after %d probe(s) of %s", code, tries, d.Version)})
			}
			return e.Store.CompleteStep(sctx, d, owner, domain.StepHealthProbe,
				fmt.Sprintf("healthy after %d probe(s)", tries))
		})
		if err != nil {
			return err
		}
	}

	// Progressive rollout: each canary_<pct> step shifts traffic then
	// re-probes before the next weight; failure rolls back like any step.
	for _, st := range d.Steps {
		w, ok := domain.ParseCanaryWeight(st.Name)
		if !ok || done[st.Name] {
			continue
		}
		err := step(ctx, st.Name, func(sctx context.Context) error {
			if err := e.Store.StartStep(sctx, d, owner, st.Name); err != nil {
				return err
			}
			if err := e.Provider.SetTraffic(sctx, target(d), w); err != nil {
				return fail(store.Failure{Step: st.Name, Code: domain.CodeProviderUnavailable,
					Message: fmt.Sprintf("traffic shift to %d%% rejected: %v", w, err)})
			}
			tries, code, _ := provider.ProbeUntilHealthy(sctx, e.Provider, target(d), e.Cfg.Probe)
			if code != "" {
				if sctx.Err() != nil {
					return sctx.Err()
				}
				return fail(store.Failure{Step: st.Name, Code: code, Rollback: true,
					Message: fmt.Sprintf("%s at %d%% traffic after %d probe(s)", code, w, tries)})
			}
			if e.CanaryProbe != nil {
				if err := e.CanaryProbe.Healthy(sctx); err != nil {
					if sctx.Err() != nil {
						return sctx.Err()
					}
					return fail(store.Failure{Step: st.Name, Code: domain.CodeHealthCheckFailed,
						Rollback: true, Message: fmt.Sprintf("canary metrics unhealthy at %d%% traffic: %v", w, err)})
				}
			}
			return e.Store.CompleteStep(sctx, d, owner, st.Name,
				fmt.Sprintf("%d%% traffic healthy after %d probe(s)", w, tries))
		})
		if err != nil {
			return err
		}
	}

	if err := step(ctx, domain.StepPromote, func(sctx context.Context) error {
		if err := e.Store.StartStep(sctx, d, owner, domain.StepPromote); err != nil {
			return err
		}
		return e.Store.Promote(sctx, d, owner)
	}); err != nil {
		return err
	}
	log.Info("deployment succeeded")
	e.reportStatus(ctx, d, "success", fmt.Sprintf("%s live on %s", d.Version, d.EnvironmentSlug))
	return nil
}

func (e *Executor) apply(ctx context.Context, t provider.Target, version string, profile domain.FailureProfile) (int, error) {
	var err error
	for attempt := 1; attempt <= e.Cfg.ApplyAttempts; attempt++ {
		actx, cancel := context.WithTimeout(ctx, e.Cfg.ApplyTimeout)
		err = e.Provider.Apply(actx, t, version, profile)
		cancel()
		if err == nil {
			return attempt, nil
		}
		if attempt < e.Cfg.ApplyAttempts {
			select {
			case <-ctx.Done():
				return attempt, ctx.Err()
			case <-time.After(domain.Backoff(attempt, e.Cfg.ApplyBackoff, 5*time.Second)):
			}
		}
	}
	return e.Cfg.ApplyAttempts, err
}

// RunRollback restores the previous stable version and verifies its health.
func (e *Executor) RunRollback(ctx context.Context, id string) error {
	owner := e.Cfg.WorkerID
	rb, res, err := e.Store.ClaimRollback(ctx, id, owner, e.Cfg.Lease)
	if err != nil {
		return err
	}
	switch res {
	case store.ClaimDone:
		return nil
	case store.ClaimBusy:
		return bus.ErrRetryLater
	}
	log := e.Log.With("rollback_id", rb.ID, "deployment_id", rb.TargetDeploymentID, "correlation_id", rb.CorrelationID)
	d, err := e.Store.GetDeployment(ctx, rb.OrganizationID, rb.TargetDeploymentID)
	if err != nil {
		return err
	}
	finish := func(ok bool, code, out string) error {
		err := e.Store.FinishRollback(ctx, rb, owner, ok, code, out)
		if errors.Is(err, store.ErrLeaseLost) {
			return nil
		}
		if err == nil {
			log.Info("rollback finished", "ok", ok, "to_version", rb.ToVersion, "error_code", code)
		}
		return err
	}
	// The stable version passed its health probe when it was promoted.
	tries, err := e.apply(ctx, target(d), rb.ToVersion, domain.ProfileHealthy)
	if err != nil {
		return finish(false, domain.CodeRollbackFailed, fmt.Sprintf("could not restore %s: provider unavailable after %d attempts", rb.ToVersion, tries))
	}
	probes, code, _ := provider.ProbeUntilHealthy(ctx, e.Provider, target(d), e.Cfg.Probe)
	if code != "" {
		return finish(false, domain.CodeRollbackFailed, fmt.Sprintf("restored %s but %s after %d probe(s)", rb.ToVersion, code, probes))
	}
	return finish(true, "", fmt.Sprintf("restored %s on %s/%s; healthy after %d probe(s)", rb.ToVersion, d.ServiceSlug, d.EnvironmentSlug, probes))
}
