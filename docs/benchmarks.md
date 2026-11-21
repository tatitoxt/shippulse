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
