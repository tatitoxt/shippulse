# ShipPulse — Release Safety Console

[![CI](https://github.com/tatitoxt/shippulse/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/tatitoxt/shippulse/actions/workflows/ci.yml)

ShipPulse is a portfolio project that models the control plane of a safe deploy pipeline:
a deploy request (manual or GitHub `deployment` webhook) is validated, persisted with a
transactional outbox, delivered through NATS JetStream to an idempotent worker, applied to a
**simulated** runtime, health-probed, and automatically rolled back to the last stable version
when probes fail. Every step is recorded in an append-only audit log and shown in a web console
that reads the real API.

> Scope: the runtime is a deterministic simulator (`internal/simruntime`). ShipPulse does not
> deploy to Kubernetes, VMs or any real infrastructure, and it has no production users.

## The problem

Release tooling has to stay correct when things go wrong: a request is retried, a message is
delivered twice, a worker dies mid-deploy, the new version fails health checks. The interesting
part is not the happy path but proving that the system never runs two deploys on the same
target, never loses a requested deploy, and always restores the last known-good version — and
that an operator can see the evidence afterwards.

## What it does today

- **Deploy requests**: `POST /api/v1/deployments` (JWT, `Idempotency-Key` header) or a signed
  GitHub `deployment` webhook (HMAC-SHA256, delivery id used for deduplication; `push` events are ignored).
- **Validation**: service and environment must belong to the caller's organization and project;
  versions are validated; viewers get `403`.
- **Transactional outbox**: deployment, its four ordered steps, the outbox event and the audit
  event are written in one PostgreSQL transaction. A relay publishes to NATS JetStream with
  `Nats-Msg-Id` deduplication and exponential backoff on failure.
- **Idempotent worker**: claims deployments with a lease (`lease_owner`, `lease_expires_at`),
  skips completed steps when resuming after a crash, fences off workers that lost their lease,
  and ignores duplicate deliveries. A partial unique index allows only one running deployment
  per service/environment.
- **Lifecycle**: `validate → apply_version → health_probe → [canary_<pct>…] → promote`.
  A request may carry `"canary": [10, 50]` — strictly increasing weights, 1-99 — and the
  worker inserts one `canary_<pct>` step per weight before `promote`, calling the provider's
  `SetTraffic` and re-probing health between shifts; a failed canary step rolls back like
  any other. With `CANARY_METRICS_URL` + `CANARY_METRICS_QUERY` + `CANARY_METRICS_THRESHOLD`
  set, each weight is also gated on a real error signal: the worker evaluates a Prometheus
  instant query (any compatible API — Prometheus, VictoriaMetrics, Mimir) and rolls back
  when the scalar result exceeds the threshold or the query fails (fail-closed).
  `CANARY_METRICS_CHECKS` widens the gate to several named checks — a JSON array of
  `{"name", "query", "threshold"}` so a scalar error-rate check can run alongside
  `histogram_quantile` latency checks (e.g.
  `[{"name":"p99","query":"histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))","threshold":0.5}]`); every check must pass, and rollback names the breached check. The simulated runtime
  supports the failure profiles `healthy`, `unhealthy`, `timeout` and `provider_unavailable`.
- **Real provider (opt-in)**: `PROVIDER=kubectl` swaps the simulated runtime for a real
  Kubernetes backend driven by the `kubectl` CLI. It maps each target to a Deployment
  named `<service>-<environment>` in `KUBECTL_NAMESPACE` (default `default`), applies via
  `kubectl set image`, probes by ready replicas, and — with `CANARY_INGRESS=true` —
  drives canary weights through the NGINX Ingress `canary-weight` annotation on an
  Ingress named like the Deployment. `PROVIDER=nomad` targets the Nomad HTTP API
  (`NOMAD_ADDR`, optional `NOMAD_TOKEN`/`NOMAD_IMAGE_PREFIX`): `Apply` registers a
  `<service>-<environment>` docker job, `Probe` requires job status `running`,
  and `SetTraffic` promotes the latest deployment at 100% (intermediate canary
  steps only add bake time — Nomad canaries gate on task counts, not traffic
  weight).
- **Automatic rollback**: a failed health probe marks the deployment `failed` and queues a
  separate rollback operation that re-applies and re-probes the previous stable version.
  Rollbacks that fail are reported as failed (never as "rolled back") and can be retried manually.
- **Audit log**: append-only table (updates/deletes are rejected by a trigger); sensitive
  metadata keys are redacted.
- **Live updates over SSE**: `GET /api/v1/events` streams org-scoped change events emitted by
  Postgres `LISTEN/NOTIFY` triggers on `deployments`, `deployment_steps` and
  `service_environments`. A single dedicated `LISTEN` connection fans out to all subscribers
  in-process, so any number of open consoles costs exactly one database connection. The console
  refreshes instantly on each event and keeps a 30 s poll as a backstop.
  Events are persisted to `change_events` (retained for one hour) and every
  `data:` frame carries an `id:` — a reconnecting client that sends
  `Last-Event-ID` replays everything it missed before live delivery resumes.
- **GitHub commit statuses**: when `GITHUB_TOKEN` is set, the worker posts `pending` →
  `success`/`failure` statuses (context `shippulse/<environment>`) back to the deployment's
  `git_sha`, and the API posts `error` when a queued deploy is cancelled. Reporting is
  best-effort — a GitHub failure is logged, never fails the deploy. `GITHUB_API_URL`
  overrides the API base (e.g. GHES) and `STATUS_TARGET_BASE` links statuses to a dashboard.
- **Prometheus metrics**: `GET /metrics` exposes deployments/steps/rollbacks by status,
  deployment duration p50/p95 (computed at scrape time, so it covers every api and
  worker process), outbox backlog, and per-process HTTP request counters.
- **Web console** (Next.js): services with promoted vs. actually-running version, deploy form,
  deployment list, per-deployment steps, rollback and runtime evidence, audit trail.
- **Multi-tenant**: two synthetic organizations (`Acme Demo`, `Globex Demo`); every query is
  scoped by organization, and cross-tenant access returns `404`.

## Quickstart

Requirements: Docker with Compose v2, `make`. (Go 1.26 and Node 22 only for running tests locally.)

```bash
git clone https://github.com/tatitoxt/shippulse.git
cd shippulse
make demo          # builds and starts the stack, then runs the scripted demo
```

<p align="center">
  <img src="docs/demo.svg" width="880" alt="two healthy deploys, failed health probe and automatic rollback with audit trail">
</p>

`make demo` deploys `checkout-api` to `staging` three times through the real HTTP API:

1. `v1.0.0` manual deploy, healthy → **succeeded**
2. `v1.1.0` via a signed GitHub `deployment` webhook, healthy → **succeeded**
3. `v1.2.0` manual deploy with `failure_profile=unhealthy` → **failed**, then the automatic
   rollback restores **v1.1.0**; the demo checks that both the promoted version and the
   simulated runtime report `v1.1.0` and prints the audit trail.

Then open http://localhost:3000 and sign in as `operator@acme.example` (engineer),
`viewer@acme.example` (read-only) or `operator@globex.example` (second tenant). The password is
`DEMO_PASSWORD`, which defaults to `shippulse-demo` in `docker-compose.yml`.

All credentials in `docker-compose.yml` are throwaway demo defaults; override `POSTGRES_PASSWORD`,
`JWT_SECRET`, `GITHUB_WEBHOOK_SECRET` and `DEMO_PASSWORD` via environment variables for anything else.
Ports are published on `127.0.0.1` only.

```bash
make down          # stop the stack
make demo-reset    # stop and delete all demo data
```

Manual API example (after `make up`):

```bash
TOKEN=$(curl -s localhost:8080/api/v1/auth/login \
  -d '{"email":"operator@acme.example","password":"shippulse-demo"}' | jq -r .token)
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/services | jq '.services[].slug'
```

## Architecture

```mermaid
flowchart LR
  UI[Next.js console] -->|/api/v1 proxy| API
  GH[GitHub deployment webhook] -->|HMAC-SHA256| API
  subgraph Go binary: shippulse
    API[api] 
    Relay[worker: outbox relay]
    Worker[worker: executor]
    Sim[simruntime]
  end
  API -->|one transaction: deployment + steps + outbox + audit| PG[(PostgreSQL)]
  Relay -->|SELECT ... FOR UPDATE SKIP LOCKED| PG
  Relay -->|Nats-Msg-Id = outbox id| NATS[[NATS JetStream]]
  NATS -->|explicit ack, redelivery| Worker
  Worker -->|lease, steps, audit, rollback| PG
  Worker -->|apply version / health probe| Sim
  API -->|observed runtime, read-only| Sim
```

One Go module, one binary with subcommands (`migrate`, `seed`, `api`, `worker`, `simruntime`,
`demo`). Details: [ARCHITECTURE.md](ARCHITECTURE.md), the
[lifecycle spec](docs/spec/release-lifecycle.md) and the ADRs in [docs/adr](docs/adr).

| Path | Responsibility |
|---|---|
| `internal/domain` | state machines, validation, roles, backoff |
| `internal/store` | PostgreSQL persistence: transactional create, leases, outbox, audit |
| `internal/migrate` | embedded, versioned SQL migrations |
| `internal/outbox`, `internal/bus` | outbox relay and NATS JetStream publish/consume |
| `internal/worker` | step executor and rollback |
| `internal/provider`, `internal/simruntime` | provider interface, HTTP client and the simulated runtime |
| `internal/api` | HTTP API, JWT auth, RBAC, GitHub webhook |
| `apps/web` | Next.js console and Playwright E2E |

## Tests

| Suite | Command | What it covers |
|---|---|---|
| Unit | `make test` | state transitions, validation, backoff, JWT, webhook signatures, simulated runtime and provider |
| Integration | `make deps-up && make test-integration` | real PostgreSQL + NATS: atomic create (with injected failure), idempotency (incl. concurrent requests), leases and crash resume, duplicate delivery, outbox retry, timeout, provider unavailable, automatic and manual rollback, failed rollback, tenant isolation, viewer denial, webhook handling |
| E2E | `make e2e` | Playwright drives the browser against the full Compose stack: healthy deploy, failed deploy auto-rolled-back, evidence after reload, viewer denial (UI and API), tenant isolation |
| Lint / types | `make lint`, `make web-check` | gofmt, go vet, ESLint, `tsc --noEmit`, production build |
| Audits | `make vuln` | govulncheck and `npm audit --audit-level=high` |

Measured in this repository (count them with `go test -list . ./...` and
`go test -tags=integration -list . ./...`): 56 Go test functions — 22 unit and 34 integration —
plus 3 Playwright E2E tests. The SSE endpoint is covered by live integration tests that drive a
deployment through real `pg_notify` triggers and assert org isolation on the stream. All of them run in [GitHub Actions](.github/workflows/ci.yml) on every
pull request and push to `main`, together with `make demo`.

## Limitations

- The runtime is simulated in memory: its state resets when the `simruntime` container restarts,
  and "health" is decided by the failure profile chosen at deploy time.
- Single worker process in the demo; multiple workers are supported by leases and are covered by
  integration tests, not by a load test. Measured numbers: docs/benchmarks.md (~22.5ms create→promote, ~44 deployments/sec).
- GitHub integration is limited to receiving signed `deployment` events plus opt-in
  commit statuses (`GITHUB_TOKEN`); ShipPulse does not create GitHub Deployments itself.
- Authentication is email/password with HS256 JWTs for seeded demo users, with a
  sliding-window rate limit on failed sign-ins (5 per 5 min per IP+email → 429).
  There is no sign-up, password reset, SSO or token revocation.
- SSE replay covers only the last hour (`change_events` retention); an event missed for longer
  is only caught by the 30 s poll backstop, and there is no fallback for clients that block
  `EventSource`.
- `GET /metrics` serves Prometheus exposition (deployments/steps/rollbacks by status,
  duration p50/p95, outbox backlog, per-process HTTP counters). It is unauthenticated,
  like `/healthz` — bind the api to a private interface in real deployments. No tracing.
- Not security-audited and not deployed anywhere; the Compose file is for local demos.

## License

[MIT](LICENSE) © Fausto Pastura

## Benchmarks

`go test -tags integration -bench DeploymentEndToEnd` drives the full pipeline (Postgres outbox -> relay -> JetStream -> worker -> promoted): **22.5 ms** create-to-promote, ~44 deployments/sec sequential. See [docs/benchmarks.md](docs/benchmarks.md).
