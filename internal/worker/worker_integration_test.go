//go:build integration

package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tatitoxt/shippulse/internal/bus"
	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/outbox"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/testutil"
	"github.com/tatitoxt/shippulse/internal/worker"
)

type harness struct {
	s    *store.Store
	tg   testutil.Target
	prov *provider.Memory
	exec *worker.Executor
	bus  *bus.Bus
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func cfg(id string) worker.Config {
	c := worker.DefaultConfig(id)
	c.ApplyBackoff = 5 * time.Millisecond
	c.Probe = provider.ProbeConfig{Attempts: 2, Timeout: 50 * time.Millisecond, Interval: 5 * time.Millisecond}
	return c
}

// start wires the real pipeline: PostgreSQL outbox -> relay -> JetStream -> worker.
func start(t *testing.T) *harness {
	t.Helper()
	s := testutil.DB(t, "worker")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ns := "t" + uuid.NewString()[:8]
	b, err := bus.ConnectNamed(ctx, testutil.NATSURL(t), "IT_"+ns, ns+".jobs.", "it-"+ns)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.JS.DeleteStream(context.Background(), "IT_"+ns); b.Close() })
	h := &harness{s: s, tg: testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging"), prov: provider.NewMemory(), bus: b}
	h.exec = &worker.Executor{Store: s, Provider: h.prov, Cfg: cfg("w1"), Log: quiet}
	relay := &outbox.Relay{Store: s, Publish: b.Publish, Interval: 20 * time.Millisecond, Batch: 10,
		Base: 20 * time.Millisecond, Max: 200 * time.Millisecond, Log: quiet}
	go relay.Run(ctx)
	cc, err := b.Consume(ctx, quiet, h.exec.Handle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.Stop)
	return h
}

func (h *harness) deploy(t *testing.T, version string, profile domain.FailureProfile) store.Deployment {
	t.Helper()
	d, _, err := h.s.CreateDeployment(context.Background(), store.CreateDeploymentInput{
		OrganizationID: h.tg.Org, ServiceID: h.tg.Service, EnvironmentID: h.tg.Env, Version: version,
		FailureProfile: string(profile), IdempotencyKey: uuid.NewString(), Source: "manual",
		Actor: "operator@acme.example", CorrelationID: uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// wait polls until the deployment and any rollback it spawned are terminal.
func (h *harness) wait(t *testing.T, id string) store.Deployment {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		d, err := h.s.GetDeployment(context.Background(), h.tg.Org, id)
		if err != nil {
			t.Fatal(err)
		}
		rbDone := true
		for _, rb := range d.Rollbacks {
			rbDone = rbDone && domain.RollbackStatus(rb.Status).Terminal()
		}
		if domain.DeploymentStatus(d.Status).Terminal() && rbDone {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("deployment %s did not finish", id)
	return store.Deployment{}
}

func (h *harness) stable(t *testing.T) string {
	t.Helper()
	sv, err := h.s.GetService(context.Background(), h.tg.Org, h.tg.Service)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sv.Environments {
		if e.EnvironmentID == h.tg.Env && e.CurrentVersion != nil {
			return *e.CurrentVersion
		}
	}
	return ""
}

func (h *harness) running(t *testing.T) string {
	st, err := h.prov.RT.Status("checkout-api", "staging")
	if err != nil {
		return ""
	}
	return st.Version
}

func stepStatus(d store.Deployment) map[string]string {
	m := map[string]string{}
	for _, s := range d.Steps {
		m[s.Name] = s.Status
	}
	return m
}

func TestHealthyDeploymentIsPromoted(t *testing.T) {
	h := start(t)
	d := h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	if d.Status != "succeeded" || len(d.Rollbacks) != 0 {
		t.Fatalf("got %s with %d rollbacks", d.Status, len(d.Rollbacks))
	}
	for name, st := range stepStatus(d) {
		if st != "succeeded" {
			t.Errorf("step %s = %s", name, st)
		}
	}
	if h.stable(t) != "v1.0.0" || h.running(t) != "v1.0.0" {
		t.Fatalf("stable=%s running=%s", h.stable(t), h.running(t))
	}
}

func TestUnhealthyAndTimeoutDeploymentsRollBackAutomatically(t *testing.T) {
	for _, tc := range []struct {
		profile domain.FailureProfile
		code    string
	}{
		{domain.ProfileUnhealthy, domain.CodeHealthCheckFailed},
		{domain.ProfileTimeout, domain.CodeHealthCheckTimeout},
	} {
		t.Run(string(tc.profile), func(t *testing.T) {
			h := start(t)
			h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
			d := h.wait(t, h.deploy(t, "v2.0.0", tc.profile).ID)
			if d.Status != "failed" || d.ErrorCode == nil || *d.ErrorCode != tc.code {
				t.Fatalf("status=%s code=%v", d.Status, d.ErrorCode)
			}
			if st := stepStatus(d); st["health_probe"] != "failed" || st["promote"] != "skipped" {
				t.Fatalf("steps: %v", st)
			}
			if len(d.Rollbacks) != 1 {
				t.Fatalf("rollbacks: %d", len(d.Rollbacks))
			}
			rb := d.Rollbacks[0]
			if rb.Trigger != "automatic" || rb.Status != "succeeded" || rb.FromVersion != "v2.0.0" || rb.ToVersion != "v1.0.0" {
				t.Fatalf("rollback: %+v", rb)
			}
			if h.stable(t) != "v1.0.0" || h.running(t) != "v1.0.0" {
				t.Fatalf("after rollback stable=%s running=%s", h.stable(t), h.running(t))
			}
			actions := audit(t, h, d.ID)
			for _, want := range []string{"deployment.requested", "deployment.started", "deployment.failed", "rollback.requested", "rollback.started", "rollback.succeeded"} {
				if !actions[want] {
					t.Errorf("missing audit action %s", want)
				}
			}
		})
	}
}

func audit(t *testing.T, h *harness, subject string) map[string]bool {
	ev, err := h.s.ListAudit(context.Background(), h.tg.Org, subject, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, e := range ev {
		m[e.Action] = true
	}
	return m
}

func TestFirstDeploymentFailureHasNothingToRollBackTo(t *testing.T) {
	h := start(t)
	d := h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileUnhealthy).ID)
	if d.Status != "failed" || len(d.Rollbacks) != 0 || h.stable(t) != "" {
		t.Fatalf("status=%s rollbacks=%d stable=%q", d.Status, len(d.Rollbacks), h.stable(t))
	}
}

func TestProviderUnavailableFailsWithoutTouchingRuntime(t *testing.T) {
	h := start(t)
	h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	d := h.wait(t, h.deploy(t, "v2.0.0", domain.ProfileProviderUnavailable).ID)
	if d.Status != "failed" || *d.ErrorCode != domain.CodeProviderUnavailable || len(d.Rollbacks) != 0 {
		t.Fatalf("status=%s code=%v rollbacks=%d", d.Status, d.ErrorCode, len(d.Rollbacks))
	}
	if st := stepStatus(d); st["apply_version"] != "failed" || st["health_probe"] != "skipped" {
		t.Fatalf("steps: %v", st)
	}
	if d.Steps[1].Attempts != 1 || h.running(t) != "v1.0.0" {
		t.Fatalf("attempts=%d running=%s", d.Steps[1].Attempts, h.running(t))
	}
}

func TestDuplicateDeliveryDoesNotReapply(t *testing.T) {
	h := start(t)
	d := h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	before := testutil.Count(t, h.s, `SELECT count(*) FROM audit_events WHERE subject_id = $1`, d.ID)
	// Same job, new message id: JetStream will not dedup it, the worker must.
	payload, _ := json.Marshal(domain.JobEvent{EventID: uuid.NewString(), Type: domain.EventDeploymentRequested,
		PayloadVersion: 1, OrganizationID: h.tg.Org, AggregateID: d.ID, CorrelationID: d.CorrelationID})
	for i := 0; i < 3; i++ {
		if err := h.bus.Publish(context.Background(), store.OutboxEvent{ID: uuid.NewString(), EventType: domain.EventDeploymentRequested, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	// Direct re-execution is also a no-op.
	if err := h.exec.RunDeployment(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	st, _ := h.prov.RT.Status("checkout-api", "staging")
	if st.Applies != 1 {
		t.Fatalf("version applied %d times", st.Applies)
	}
	if after := testutil.Count(t, h.s, `SELECT count(*) FROM audit_events WHERE subject_id = $1`, d.ID); after != before {
		t.Fatalf("duplicate deliveries wrote %d audit events", after-before)
	}
}

func TestWorkerResumesAfterCrashWithoutRepeatingCompletedSteps(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	// Pause delivery so we control execution, then simulate a worker that
	// applied the version and crashed before probing.
	d := h.deploy(t, "v2.0.0", domain.ProfileHealthy)
	claimed, res, err := h.s.ClaimDeployment(ctx, d.ID, "crashed", time.Minute)
	if err != nil || res != store.Claimed {
		t.Fatalf("claim: %v %v", res, err)
	}
	claimed, _ = h.s.ValidateTarget(ctx, claimed, "crashed")
	_ = h.prov.Apply(ctx, provider.Target{Service: "checkout-api", Environment: "staging"}, "v2.0.0", domain.ProfileHealthy)
	if err := h.s.CompleteStep(ctx, claimed, "crashed", domain.StepApplyVersion, "applied"); err != nil {
		t.Fatal(err)
	}
	// While the lease is live, other workers back off.
	if err := h.exec.RunDeployment(ctx, d.ID); !errors.Is(err, bus.ErrRetryLater) {
		t.Fatalf("live lease: want ErrRetryLater, got %v", err)
	}
	_, _ = h.s.Pool.Exec(ctx, `UPDATE deployments SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, d.ID)
	final := h.wait(t, d.ID)
	if final.Status != "succeeded" || final.Attempts < 2 {
		t.Fatalf("status=%s attempts=%d", final.Status, final.Attempts)
	}
	if final.Steps[1].Attempts != 0 {
		t.Fatalf("apply_version re-ran after resume (attempts %d)", final.Steps[1].Attempts)
	}
	st, _ := h.prov.RT.Status("checkout-api", "staging")
	if st.Applies != 2 { // v1.0.0 and v2.0.0, once each
		t.Fatalf("applies: %d", st.Applies)
	}
}

func TestFailedRollbackIsNotReportedAsRolledBack(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	h.prov.FailApply = func(_ provider.Target, v string) error {
		if v == "v1.0.0" {
			return provider.ErrUnavailable
		}
		return nil
	}
	d := h.wait(t, h.deploy(t, "v2.0.0", domain.ProfileUnhealthy).ID)
	if d.Status != "failed" || len(d.Rollbacks) != 1 || d.Rollbacks[0].Status != "failed" ||
		*d.Rollbacks[0].ErrorCode != domain.CodeRollbackFailed {
		t.Fatalf("deployment %s rollbacks %+v", d.Status, d.Rollbacks)
	}
	if h.running(t) != "v2.0.0" {
		t.Fatalf("runtime should still run the bad version, got %s", h.running(t))
	}
	// Once the provider recovers, an operator can retry the rollback manually.
	h.prov.FailApply = nil
	rb, created, err := h.s.RequestRollback(ctx, h.tg.Org, d.ID, "operator@acme.example", uuid.NewString(), "retry", uuid.NewString())
	if err != nil || !created || rb.Trigger != "manual" {
		t.Fatalf("manual retry: %+v %v", rb, err)
	}
	d = h.wait(t, d.ID)
	if last := d.Rollbacks[len(d.Rollbacks)-1]; last.Status != "succeeded" || h.running(t) != "v1.0.0" {
		t.Fatalf("manual rollback: %+v running=%s", last, h.running(t))
	}
}

func TestManualRollbackOfCurrentRelease(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.wait(t, h.deploy(t, "v1.0.0", domain.ProfileHealthy).ID)
	d2 := h.wait(t, h.deploy(t, "v2.0.0", domain.ProfileHealthy).ID)
	key := uuid.NewString()
	if _, _, err := h.s.RequestRollback(ctx, h.tg.Org, d2.ID, "operator@acme.example", key, "bad metrics", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, created, err := h.s.RequestRollback(ctx, h.tg.Org, d2.ID, "operator@acme.example", key, "bad metrics", uuid.NewString()); err != nil || created {
		t.Fatalf("idempotent replay: created=%v err=%v", created, err)
	}
	d2 = h.wait(t, d2.ID)
	if len(d2.Rollbacks) != 1 || d2.Rollbacks[0].Status != "succeeded" || d2.Status != "succeeded" {
		t.Fatalf("rollbacks=%+v status=%s", d2.Rollbacks, d2.Status)
	}
	if h.stable(t) != "v1.0.0" || h.running(t) != "v1.0.0" {
		t.Fatalf("stable=%s running=%s", h.stable(t), h.running(t))
	}
}
