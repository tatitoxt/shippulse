//go:build integration

package worker_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	jetstream "github.com/nats-io/nats.go/jetstream"

	"github.com/tatitoxt/shippulse/internal/bus"
	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/outbox"
	"github.com/tatitoxt/shippulse/internal/provider"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/testutil"
	"github.com/tatitoxt/shippulse/internal/worker"
)

// Multi-worker load test: several workers share one durable queue-group
// consumer, exactly as separate processes would. Asserts every deployment
// finishes terminal, no step executes more than once (lease enforcement
// makes a second claimant lose), and reports sustained throughput.
func TestLoadMultiWorker(t *testing.T) {
	const workers, deployments = 8, 120

	s := testutil.DB(t, "workerload")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ns := "t" + uuid.NewString()[:8]
	b, err := bus.ConnectNamed(ctx, testutil.NATSURL(t), "IT_"+ns, ns+".jobs.", "it-"+ns)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.JS.DeleteStream(context.Background(), "IT_"+ns); b.Close() }()

	tg := testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging")
	relay := &outbox.Relay{Store: s, Publish: b.Publish, Interval: 10 * time.Millisecond, Batch: 25,
		Base: 10 * time.Millisecond, Max: 100 * time.Millisecond, Log: quiet}
	go relay.Run(ctx)

	var ccs []jetstream.ConsumeContext
	for i := range workers {
		exec := &worker.Executor{Store: s, Provider: provider.NewMemory(), Cfg: cfg(fmt.Sprintf("w%d", i)), Log: quiet}
		cc, err := b.Consume(ctx, quiet, exec.Handle)
		if err != nil {
			t.Fatal(err)
		}
		ccs = append(ccs, cc)
	}
	defer func() {
		for _, cc := range ccs {
			cc.Stop()
		}
	}()

	ids := make([]string, 0, deployments)
	start := time.Now()
	for i := range deployments {
		d, _, err := s.CreateDeployment(context.Background(), store.CreateDeploymentInput{
			OrganizationID: tg.Org, ServiceID: tg.Service, EnvironmentID: tg.Env,
			Version: fmt.Sprintf("1.0.%d", i), FailureProfile: string(domain.ProfileHealthy),
			IdempotencyKey: uuid.NewString(), Source: "manual",
			Actor: "load", CorrelationID: uuid.NewString(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	created := time.Since(start)

	// One aggregate query per poll — a per-deployment GetDeployment loop here
	// would load Postgres enough to skew the measurement itself.
	deadline := time.Now().Add(90 * time.Second)
	finished := 0
	for finished < len(ids) && time.Now().Before(deadline) {
		if err := s.Pool.QueryRow(context.Background(),
			`select count(*) from deployments where id = any($1::uuid[]) and status in ('succeeded','failed','cancelled')`,
			ids).Scan(&finished); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	drain := time.Since(start)
	if finished != len(ids) {
		t.Fatalf("only %d/%d deployments finished", finished, len(ids))
	}

	// Exactly-once across the pool: a second worker claiming the same job loses
	// the lease, so every step of every deployment ran exactly one attempt.
	for _, id := range ids {
		d, err := s.GetDeployment(context.Background(), tg.Org, id)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status != string(domain.DeploymentSucceeded) {
			t.Fatalf("deployment %s ended %q, want succeeded", id, d.Status)
		}
		for _, st := range d.Steps {
			if st.Attempts != 1 {
				t.Fatalf("deployment %s step %s ran %d attempts (duplicate execution)", id, st.Name, st.Attempts)
			}
		}
	}

	rate := float64(deployments) / drain.Seconds()
	t.Logf("%d workers: %d deployments in %v (%.1f deploys/sec aggregate; create phase %v)",
		workers, deployments, drain.Round(time.Millisecond), rate, created.Round(time.Millisecond))
}
