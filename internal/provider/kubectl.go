package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/simruntime"
)

// Kubectl is a real provider backed by the `kubectl` CLI. It maps a Target to
// one Kubernetes Deployment named "<service>-<environment>" in a namespace:
//
//   - Apply sets the Deployment image to <service>=<version>.
//   - Probe is healthy when the Deployment reports at least one ready replica.
//   - SetTraffic patches the NGINX Ingress canary annotation on the Ingress
//     named like the Deployment (only when CanaryIngress is enabled).
//
// Convention-driven and opt-in via PROVIDER=kubectl; the simulated runtime
// stays the default so local development and tests need no cluster.
type Kubectl struct {
	Namespace     string
	Binary        string
	CanaryIngress bool
	// Runner executes kubectl and returns stdout; injectable for tests.
	Runner func(ctx context.Context, binary string, args ...string) ([]byte, error)
}

func NewKubectl(namespace, binary string, canaryIngress bool) *Kubectl {
	if namespace == "" {
		namespace = "default"
	}
	if binary == "" {
		binary = "kubectl"
	}
	return &Kubectl{
		Namespace:     namespace,
		Binary:        binary,
		CanaryIngress: canaryIngress,
		Runner:        runKubectl,
	}
}

func runKubectl(ctx context.Context, binary string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: kubectl %s: %v: %s", ErrUnavailable,
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (k *Kubectl) deployment(t Target) string {
	return t.Service + "-" + t.Environment
}

func (k *Kubectl) Apply(ctx context.Context, t Target, version string, _ domain.FailureProfile) error {
	dep := k.deployment(t)
	_, err := k.Runner(ctx, k.Binary, "-n", k.Namespace, "set", "image",
		"deployment/"+dep, t.Service+"="+version)
	return err
}

func (k *Kubectl) Probe(ctx context.Context, t Target) error {
	out, err := k.Runner(ctx, k.Binary, "-n", k.Namespace, "get",
		"deployment", k.deployment(t), "-o", "jsonpath={.status.readyReplicas}")
	if err != nil {
		return err
	}
	ready, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if ready < 1 {
		return ErrUnhealthy
	}
	return nil
}

func (k *Kubectl) SetTraffic(ctx context.Context, t Target, percent int) error {
	if !k.CanaryIngress {
		return nil
	}
	dep := k.deployment(t)
	_, err := k.Runner(ctx, k.Binary, "-n", k.Namespace, "annotate", "ingress", dep,
		fmt.Sprintf("nginx.ingress.kubernetes.io/canary-weight=%d", percent), "--overwrite")
	return err
}

func (k *Kubectl) Status(ctx context.Context, t Target) (simruntime.Status, error) {
	out, err := k.Runner(ctx, k.Binary, "-n", k.Namespace, "get",
		"deployment", k.deployment(t), "-o", "json")
	if err != nil {
		return simruntime.Status{}, err
	}
	var dep struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &dep); err != nil {
		return simruntime.Status{}, fmt.Errorf("parsing deployment json: %w", err)
	}
	status := simruntime.Status{
		Tenant:      t.Tenant,
		Service:     t.Service,
		Environment: t.Environment,
	}
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		status.Version = dep.Spec.Template.Spec.Containers[0].Image
	}
	return status, nil
}

var _ Provider = (*Kubectl)(nil)

// FromEnv selects the provider implementation: the simulated runtime over HTTP
// is the default; PROVIDER=kubectl switches to the real Kubernetes backend,
// PROVIDER=nomad to the Nomad HTTP API.
func FromEnv(runtimeURL string) Provider {
	switch strings.ToLower(os.Getenv("PROVIDER")) {
	case "kubectl":
		return NewKubectl(os.Getenv("KUBECTL_NAMESPACE"), os.Getenv("KUBECTL_BIN"),
			strings.EqualFold(os.Getenv("CANARY_INGRESS"), "true"))
	case "nomad":
		return NewNomad(os.Getenv("NOMAD_ADDR"), os.Getenv("NOMAD_TOKEN"),
			os.Getenv("NOMAD_IMAGE_PREFIX"))
	}
	return NewHTTP(runtimeURL)
}
