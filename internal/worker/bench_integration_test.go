//go:build integration

package worker_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tatitoxt/shippulse/internal/domain"
	"github.com/tatitoxt/shippulse/internal/store"
)

// BenchmarkDeploymentEndToEnd measures the real pipeline: API-style
// CreateDeployment -> Postgres outbox -> relay -> JetStream -> worker ->
// promoted. ns/op is the create-to-promote wall-clock latency.
func BenchmarkDeploymentEndToEnd(b *testing.B) {
	h := start(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, _, err := h.s.CreateDeployment(context.Background(), store.CreateDeploymentInput{
			OrganizationID: h.tg.Org, ServiceID: h.tg.Service, EnvironmentID: h.tg.Env,
			Version: fmt.Sprintf("0.0.%d", i), FailureProfile: string(domain.ProfileHealthy),
			IdempotencyKey: uuid.NewString(), Source: "manual",
			Actor: "bench", CorrelationID: uuid.NewString(),
		})
		if err != nil {
			b.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			cur, err := h.s.GetDeployment(context.Background(), h.tg.Org, d.ID)
			if err != nil {
				b.Fatal(err)
			}
			if domain.DeploymentStatus(cur.Status).Terminal() {
				break
			}
			if time.Now().After(deadline) {
				b.Fatalf("deployment %s did not finish", d.ID)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
