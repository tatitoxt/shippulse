package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tatitoxt/shippulse/internal/domain"
)

type fakeRunner struct {
	calls   [][]string
	outputs map[string]string
	err     error
}

func (f *fakeRunner) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	joined := strings.Join(args, " ")
	for match, out := range f.outputs {
		if strings.Contains(joined, match) {
			return []byte(out), nil
		}
	}
	return []byte(""), nil
}

func kubectlWithRunner(t *testing.T, r *fakeRunner) *Kubectl {
	t.Helper()
	k := NewKubectl("prod", "kubectl", true)
	k.Runner = r.run
	return k
}

var target = Target{Tenant: "acme", Service: "payments", Environment: "staging"}

func TestKubectlApplySetsDeploymentImage(t *testing.T) {
	r := &fakeRunner{}
	k := kubectlWithRunner(t, r)

	err := k.Apply(context.Background(), target, "registry/payments:v2", domain.FailureProfile(""))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	want := "-n prod set image deployment/payments-staging payments=registry/payments:v2"
	if got := strings.Join(r.calls[0], " "); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKubectlProbeHealthyWhenReplicaReady(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{"readyReplicas": "1"}}
	k := kubectlWithRunner(t, r)
	if err := k.Probe(context.Background(), target); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
	if !strings.Contains(strings.Join(r.calls[0], " "), "get deployment payments-staging") {
		t.Fatalf("unexpected probe command: %v", r.calls[0])
	}
}

func TestKubectlProbeUnhealthyWithNoReplicas(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{}}
	k := kubectlWithRunner(t, r)
	if err := k.Probe(context.Background(), target); !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("expected ErrUnhealthy, got %v", err)
	}
}

func TestKubectlProbePropagatesRunnerError(t *testing.T) {
	r := &fakeRunner{err: fmt.Errorf("%w: boom", ErrUnavailable)}
	k := kubectlWithRunner(t, r)
	if err := k.Probe(context.Background(), target); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestKubectlSetTrafficAnnotatesCanaryIngress(t *testing.T) {
	r := &fakeRunner{}
	k := kubectlWithRunner(t, r)
	if err := k.SetTraffic(context.Background(), target, 25); err != nil {
		t.Fatalf("set traffic: %v", err)
	}
	joined := strings.Join(r.calls[0], " ")
	if !strings.Contains(joined, "annotate ingress payments-staging") ||
		!strings.Contains(joined, "canary-weight=25") {
		t.Fatalf("unexpected command: %v", r.calls[0])
	}
}

func TestKubectlSetTrafficNoopWithoutCanaryIngress(t *testing.T) {
	r := &fakeRunner{}
	k := NewKubectl("prod", "kubectl", false)
	k.Runner = r.run
	if err := k.SetTraffic(context.Background(), target, 25); err != nil {
		t.Fatalf("set traffic: %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no kubectl call, got %v", r.calls)
	}
}

func TestKubectlStatusMapsDeployment(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{
		"get deployment": `{"spec":{"template":{"spec":{"containers":[{"image":"registry/payments:v2"}]}}}}`,
	}}
	k := kubectlWithRunner(t, r)
	s, err := k.Status(context.Background(), target)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if s.Tenant != "acme" || s.Service != "payments" || s.Environment != "staging" || s.Version != "registry/payments:v2" {
		t.Fatalf("got %+v", s)
	}
}

func TestKubectlStatusPropagatesRunnerError(t *testing.T) {
	r := &fakeRunner{err: fmt.Errorf("%w: boom", ErrUnavailable)}
	k := kubectlWithRunner(t, r)
	if _, err := k.Status(context.Background(), target); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
