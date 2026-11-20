//go:build integration

package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/migrate"
	"github.com/tatitoxt/shippulse/internal/store"
	"github.com/tatitoxt/shippulse/internal/testutil"
)

func input(tg testutil.Target, version, key string) store.CreateDeploymentInput {
	return store.CreateDeploymentInput{
		OrganizationID: tg.Org, ServiceID: tg.Service, EnvironmentID: tg.Env, Version: version,
		FailureProfile: "healthy", IdempotencyKey: key, Source: "manual", Actor: "operator@acme.example",
		CorrelationID: "00000000-0000-0000-0000-000000000001",
	}
}

func setup(t *testing.T) (*store.Store, testutil.Target) {
	s := testutil.DB(t, "store")
	return s, testutil.Lookup(t, s, "acme-demo", "checkout-api", "staging")
}

func TestMigrationsAreIdempotent(t *testing.T) {
	s, _ := setup(t)
	v, err := migrate.Up(context.Background(), s.Pool)
	if err != nil || v != 2 {
		t.Fatalf("re-running migrations: version=%d err=%v", v, err)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM schema_migrations`); n != 2 {
		t.Fatalf("schema_migrations rows: %d", n)
	}
}

func TestCreateDeploymentWritesEverythingInOneTransaction(t *testing.T) {
	s, tg := setup(t)
	d, created, err := s.CreateDeployment(context.Background(), input(tg, "v1.0.0", "k1"))
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	if d.Status != "queued" || len(d.Steps) != 4 || d.Steps[0].Name != domain.StepValidate {
		t.Fatalf("unexpected deployment: %+v", d)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = $2`, d.ID, domain.EventDeploymentRequested); n != 1 {
		t.Fatalf("outbox rows: %d", n)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM audit_events WHERE subject_id = $1 AND action = 'deployment.requested'`, d.ID); n != 1 {
		t.Fatalf("audit rows: %d", n)
	}
}

func TestCreateDeploymentRollsBackWhenOutboxInsertFails(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	_, err := s.Pool.Exec(ctx, `
		CREATE FUNCTION fail_outbox() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END; $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_outbox BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION fail_outbox();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateDeployment(ctx, input(tg, "v1.0.0", "k-fail")); err == nil {
		t.Fatal("expected error from injected outbox failure")
	}
	for _, table := range []string{"deployments", "deployment_steps", "audit_events", "outbox_events"} {
		if n := testutil.Count(t, s, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after a failed create; transaction was not atomic", table, n)
		}
	}
}

func TestIdempotencyKey(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	first, _, err := s.CreateDeployment(ctx, input(tg, "v1.0.0", "same-key"))
	if err != nil {
		t.Fatal(err)
	}
	again, created, err := s.CreateDeployment(ctx, input(tg, "v1.0.0", "same-key"))
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("replay: id=%s created=%v err=%v", again.ID, created, err)
	}
	if _, _, err := s.CreateDeployment(ctx, input(tg, "v9.9.9", "same-key")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("key reuse with different body: want ErrConflict, got %v", err)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM outbox_events`); n != 1 {
		t.Fatalf("replays must not enqueue jobs: %d outbox rows", n)
	}
}

func TestConcurrentCreatesWithSameKeyYieldOneDeployment(t *testing.T) {
	s, tg := setup(t)
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, _, err := s.CreateDeployment(context.Background(), input(tg, "v1.0.0", "race"))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- d.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 || testutil.Count(t, s, `SELECT count(*) FROM deployments`) != 1 {
		t.Fatalf("expected exactly one deployment, got ids %v", seen)
	}
}

func TestTenantIsolation(t *testing.T) {
	s, acme := setup(t)
	ctx := context.Background()
	globex := testutil.Lookup(t, s, "globex-demo", "invoice-worker", "staging")
	d, _, err := s.CreateDeployment(ctx, input(acme, "v1.0.0", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeployment(ctx, globex.Org, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other tenant read a deployment: %v", err)
	}
	cross := input(acme, "v1.0.0", "k2")
	cross.OrganizationID = globex.Org
	if _, _, err := s.CreateDeployment(ctx, cross); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other tenant deployed to a foreign service: %v", err)
	}
	if list, _ := s.ListDeployments(ctx, globex.Org, store.ListFilter{}); len(list) != 0 {
		t.Errorf("other tenant listed %d deployments", len(list))
	}
	if ev, _ := s.ListAudit(ctx, globex.Org, "", 0); len(ev) != 0 {
		t.Errorf("other tenant listed %d audit events", len(ev))
	}
	if _, _, err := s.RequestRollback(ctx, globex.Org, d.ID, "x", "rk", "r", "c"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other tenant requested a rollback: %v", err)
	}
}

func TestClaimIsExclusiveAndExpiredLeasesAreReclaimed(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	d, _, _ := s.CreateDeployment(ctx, input(tg, "v1.0.0", "k1"))

	results := make(chan store.ClaimResult, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, res, err := s.ClaimDeployment(ctx, d.ID, fmt.Sprintf("w%d", i), time.Minute)
			if err != nil {
				t.Error(err)
			}
			results <- res
		}(i)
	}
	wg.Wait()
	close(results)
	claimed := 0
	for r := range results {
		if r == store.Claimed {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("%d workers claimed the same deployment", claimed)
	}

	var owner string
	_ = s.Pool.QueryRow(ctx, `SELECT lease_owner FROM deployments WHERE id = $1`, d.ID).Scan(&owner)
	if _, err := s.Pool.Exec(ctx, `UPDATE deployments SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	_, res, err := s.ClaimDeployment(ctx, d.ID, "rescuer", time.Minute)
	if err != nil || res != store.Claimed {
		t.Fatalf("expired lease not reclaimed: %v %v", res, err)
	}
	if err := s.CompleteStep(ctx, d, owner, domain.StepValidate, "late"); !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("stale owner must be fenced off, got %v", err)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM audit_events WHERE subject_id = $1 AND action = 'deployment.reclaimed'`, d.ID); n != 1 {
		t.Fatalf("reclaim audit events: %d", n)
	}
}

func TestOnlyOneRunningDeploymentPerTarget(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	a, _, _ := s.CreateDeployment(ctx, input(tg, "v1.0.0", "a"))
	b, _, _ := s.CreateDeployment(ctx, input(tg, "v1.1.0", "b"))
	if _, res, _ := s.ClaimDeployment(ctx, a.ID, "w1", time.Minute); res != store.Claimed {
		t.Fatal("first claim failed")
	}
	if _, res, err := s.ClaimDeployment(ctx, b.ID, "w2", time.Minute); res != store.ClaimBusy || err != nil {
		t.Fatalf("second deployment to the same target must wait: %v %v", res, err)
	}
}

func TestOutboxRelayBacksOffAndRetries(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	d, _, _ := s.CreateDeployment(ctx, input(tg, "v1.0.0", "k1"))
	boom := func(context.Context, store.OutboxEvent) error { return errors.New("nats down") }
	pub, failed, err := s.RelayOutbox(ctx, 10, boom, time.Minute, time.Hour)
	if err != nil || pub != 0 || failed != 1 {
		t.Fatalf("failing publish: pub=%d failed=%d err=%v", pub, failed, err)
	}
	if n := testutil.Count(t, s, `SELECT count(*) FROM outbox_events WHERE attempts = 1 AND next_attempt_at > now() AND last_error = 'nats down'`); n != 1 {
		t.Fatal("failed publish must be scheduled for a later retry")
	}
	var got []store.OutboxEvent
	ok := func(_ context.Context, ev store.OutboxEvent) error { got = append(got, ev); return nil }
	if pub, _, _ := s.RelayOutbox(ctx, 10, ok, time.Minute, time.Hour); pub != 0 {
		t.Fatal("event must not be retried before its backoff expires")
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE outbox_events SET next_attempt_at = now()`)
	if pub, _, _ := s.RelayOutbox(ctx, 10, ok, time.Minute, time.Hour); pub != 1 || got[0].AggregateID != d.ID {
		t.Fatalf("retry did not publish: %+v", got)
	}
	if n, _ := s.PendingOutbox(ctx); n != 0 {
		t.Fatalf("pending after publish: %d", n)
	}
}

func TestAuditEventsAreAppendOnly(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	_, _, _ = s.CreateDeployment(ctx, input(tg, "v1.0.0", "k1"))
	if _, err := s.Pool.Exec(ctx, `UPDATE audit_events SET actor = 'mallory'`); err == nil {
		t.Error("audit update allowed")
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Error("audit delete allowed")
	}
}

func TestCancelOnlyWhileQueued(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	d, _, _ := s.CreateDeployment(ctx, input(tg, "v1.0.0", "k1"))
	c, err := s.CancelDeployment(ctx, tg.Org, d.ID, "operator@acme.example", "corr")
	if err != nil || c.Status != "cancelled" || c.FinishedAt == nil {
		t.Fatalf("cancel: %+v %v", c, err)
	}
	if _, err := s.CancelDeployment(ctx, tg.Org, d.ID, "operator@acme.example", "corr"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second cancel: %v", err)
	}
	if _, res, _ := s.ClaimDeployment(ctx, d.ID, "w1", time.Minute); res != store.ClaimDone {
		t.Fatal("cancelled deployment must not be executed")
	}
}

func TestRollbackRequiresAPreviousStableVersion(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	d, _, _ := s.CreateDeployment(ctx, input(tg, "v1.0.0", "k1"))
	if _, _, err := s.RequestRollback(ctx, tg.Org, d.ID, "op", "rk", "why", "corr"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("rollback of a queued first deployment: %v", err)
	}
}

// start creates a deployment and drives it through claim and validation, the
// state a worker is in right before it touches the runtime.
func start(t *testing.T, s *store.Store, tg testutil.Target, version string) store.Deployment {
	t.Helper()
	ctx := context.Background()
	d, _, err := s.CreateDeployment(ctx, input(tg, version, uuid.NewString()))
	if err != nil {
		t.Fatal(err)
	}
	d, res, err := s.ClaimDeployment(ctx, d.ID, "w", time.Minute)
	if err != nil || res != store.Claimed {
		t.Fatalf("claim %s: %v %v", version, res, err)
	}
	d, err = s.ValidateTarget(ctx, d, "w")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func promote(t *testing.T, s *store.Store, tg testutil.Target, version string) store.Deployment {
	t.Helper()
	d := start(t, s, tg, version)
	if err := s.Promote(context.Background(), d, "w"); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestQueuedDeploymentWaitsForPendingRollback(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	promote(t, s, tg, "v1")
	bad := start(t, s, tg, "v2")
	rb, err := s.Fail(ctx, bad, "w", store.Failure{Step: domain.StepHealthProbe, Code: domain.CodeHealthCheckFailed, Message: "x", Rollback: true})
	if err != nil || rb == nil {
		t.Fatalf("fail: %v %v", rb, err)
	}
	next, _, _ := s.CreateDeployment(ctx, input(tg, "v3", "next"))
	if _, res, err := s.ClaimDeployment(ctx, next.ID, "w2", time.Minute); res != store.ClaimBusy || err != nil {
		t.Fatalf("a newer release must wait for the pending rollback: %v %v", res, err)
	}
	claimed, res, err := s.ClaimRollback(ctx, rb.ID, "w", time.Minute)
	if err != nil || res != store.Claimed {
		t.Fatalf("claim rollback: %v %v", res, err)
	}
	if err := s.FinishRollback(ctx, claimed, "w", true, "", "restored"); err != nil {
		t.Fatal(err)
	}
	if _, res, err := s.ClaimDeployment(ctx, next.ID, "w2", time.Minute); res != store.Claimed || err != nil {
		t.Fatalf("release after rollback: %v %v", res, err)
	}
}

func TestRollbackIsSupersededByANewerRelease(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	promote(t, s, tg, "v1")
	d2 := promote(t, s, tg, "v2")
	d3 := start(t, s, tg, "v3") // running when the rollback of v2 is requested
	rb, _, err := s.RequestRollback(ctx, tg.Org, d2.ID, "operator@acme.example", "rk", "bad metrics", "corr")
	if err != nil {
		t.Fatal(err)
	}
	if _, res, err := s.ClaimRollback(ctx, rb.ID, "w", time.Minute); res != store.ClaimBusy || err != nil {
		t.Fatalf("rollback must wait for the running deployment: %v %v", res, err)
	}
	if err := s.Promote(ctx, d3, "w"); err != nil {
		t.Fatal(err)
	}
	if _, res, err := s.ClaimRollback(ctx, rb.ID, "w", time.Minute); res != store.ClaimDone || err != nil {
		t.Fatalf("stale rollback must not run: %v %v", res, err)
	}
	got, _ := s.GetRollback(ctx, tg.Org, rb.ID)
	if got.Status != "failed" || got.ErrorCode == nil || *got.ErrorCode != domain.CodeRollbackSuperseded {
		t.Fatalf("rollback: %+v", got)
	}
	var current string
	_ = s.Pool.QueryRow(ctx, `SELECT current_version FROM service_environments WHERE service_id = $1 AND environment_id = $2`, tg.Service, tg.Env).Scan(&current)
	if current != "v3" {
		t.Fatalf("newer release was overwritten: stable=%s", current)
	}
}

func TestSupersededFailureIsNotRollbackEligible(t *testing.T) {
	s, tg := setup(t)
	ctx := context.Background()
	promote(t, s, tg, "v1")
	bad := start(t, s, tg, "v2")
	if _, err := s.Fail(ctx, bad, "w", store.Failure{Step: domain.StepApplyVersion, Code: domain.CodeProviderUnavailable, Message: "x"}); err != nil {
		t.Fatal(err)
	}
	promote(t, s, tg, "v3")
	if _, _, err := s.RequestRollback(ctx, tg.Org, bad.ID, "op", "rk", "why", "corr"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("rolling back a superseded failure would overwrite v3: %v", err)
	}
}
