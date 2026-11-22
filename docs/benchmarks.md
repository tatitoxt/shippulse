# Benchmarks

Measured end-to-end through the real pipeline — `CreateDeployment` → Postgres
transactional outbox → relay → JetStream → worker → promoted — not a mocked
unit. The benchmark lives in
`internal/worker/bench_integration_test.go`.

**How to reproduce**

```sh
TEST_DATABASE_URL=postgres://shippulse:shippulse@host:5432/shippulse \
TEST_NATS_URL=nats://host:4222 \
go test -tags integration -run '^$' \
  -bench DeploymentEndToEnd -benchtime=30x -count=1 ./internal/worker/
```

**Results (2026-11-21, 30 sequential deployments, local Postgres 17 + NATS)**

| Metric | Value |
|---|---:|
| Create → promoted latency | **22.5 ms** per deployment (ns/op) |
| Sustained throughput | ~44 deployments/sec, sequential |

**Reading the numbers**

- Each iteration covers the full round trip: row insert + outbox write, relay
  poll, JetStream publish/consume, worker steps (apply + health probe), and
  status write-back.
- The bench uses the in-memory provider with a healthy profile, so this is
  the control-plane floor: real cloud providers add their own apply and probe
  time on top.
- Latency is dominated by the relay's 20 ms poll interval; a bus-triggered
  relay could cut it further.

## Multi-worker throughput (2026-11-22)

`internal/worker/load_integration_test.go` runs the real pipeline with
several workers sharing the durable JetStream queue-group consumer — the same
topology as separate worker processes. For each pool size it verifies every
deployment reaches `succeeded` and that every step ran exactly one attempt
(a second claimant loses the lease, so duplicate execution cannot hide).

**How to reproduce**

```sh
TEST_DATABASE_URL=postgres://shippulse:shippulse@host:5432/shippulse \
TEST_NATS_URL=nats://host:4222 \
go test -tags integration -run TestLoadMultiWorker -v ./internal/worker/
```

**Results (local Postgres 17 + NATS, in-memory provider)**

| Workers | Deployments | Wall time | Throughput | Duplicates |
|--------:|------------:|----------:|-----------:|-----------:|
| 4 | 60 | 7.2 s | ~9.6 deployments/sec | 0 |
| 8 | 120 | 5.6 s | ~21.6 deployments/sec | 0 |

**Reading the numbers**

- Throughput scales with the worker count (~2.2× from 4 to 8): per-deployment
  control-plane latency (~0.4 s: relay poll + lease claim + 4 step
  transactions + health probe) is the bound, not contention — workers never
  fight over the same job.
- "Duplicates: 0" is the important invariant: `attempts = 1` on every step of
  every deployment, so leases guarantee exactly-once processing under
  horizontal scaling.
