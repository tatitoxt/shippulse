# Benchmarks

All numbers below were measured on 2026-11-22 in a Devin VM (8 vCPU Intel Xeon, 31 GiB RAM)
against the Docker Compose `postgres:16.10-alpine` and `nats:2.11.9-alpine` containers. Both
benchmarks run the real pipeline (`CreateDeployment` → PostgreSQL transactional outbox → relay →
JetStream → worker → promoted) with the in-memory provider and the `healthy` profile, so they
measure control-plane overhead only. A real provider adds its own apply and probe time on top.
They are local measurements on one machine, not production numbers.

Start the dependencies first:

```sh
make deps-up
export TEST_DATABASE_URL=postgres://shippulse:shippulse@localhost:5432/shippulse?sslmode=disable
export TEST_NATS_URL=nats://localhost:4222
```

## Sequential latency

`internal/worker/bench_integration_test.go`

```sh
go test -tags integration -run '^$' -bench DeploymentEndToEnd -benchtime=30x -count=3 ./internal/worker/
```

| Run | Create → promoted |
|---|---:|
| 1 | 29.8 ms/op |
| 2 | 33.1 ms/op |
| 3 | 30.4 ms/op |

The outbox relay polls every 20 ms in this test, which accounts for a large share of the latency.

## Multi-worker load test

`internal/worker/load_integration_test.go` starts 8 workers on one durable JetStream consumer
(the same topology as 8 worker processes), creates 120 deployments, and asserts that every
deployment succeeds and every step has `attempts = 1` (no duplicate execution).

```sh
go test -tags integration -count=1 -run TestLoadMultiWorker -v ./internal/worker/
```

| Workers | Deployments | Wall time | Throughput | Steps run more than once |
|---:|---:|---:|---:|---:|
| 8 | 120 | 5.5 s | about 21.8 deployments/s | 0 |
