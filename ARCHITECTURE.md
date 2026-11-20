# Architecture

ShipPulse is one Go module (`github.com/tatitoxt/shippulse`) built into a single binary,
`shippulse`, plus a Next.js console in `apps/web`. The binary's subcommands are run as separate
containers by `docker-compose.yml`.

| Subcommand | Role |
|---|---|
| `migrate` | applies embedded SQL migrations (`internal/migrate/migrations`) under an advisory lock |
| `seed` | upserts the synthetic tenants, services and users (`internal/seed`) |
| `api` | HTTP API on `:8080` (`internal/api`) |
| `worker` | outbox relay + JetStream consumer + step executor (`internal/outbox`, `internal/bus`, `internal/worker`) |
| `simruntime` | the simulated deploy target on `:8090` (`internal/simruntime`) |
| `demo` | scripted end-to-end demo against the HTTP API (`internal/demo`) |
| `probe` | container health check helper |

## Request to rollback, step by step

```mermaid
sequenceDiagram
  participant C as Console / GitHub
  participant A as API
  participant P as PostgreSQL
  participant R as Outbox relay
  participant N as NATS JetStream
  participant W as Worker
  participant S as Simulated runtime
  C->>A: POST /deployments (Idempotency-Key) or signed deployment webhook
  A->>P: BEGIN; deployment + 4 steps + outbox + audit; COMMIT
  A-->>C: 202 queued (200 on idempotent replay)
  R->>P: SELECT ... FOR UPDATE SKIP LOCKED
  R->>N: publish deployment.requested (Nats-Msg-Id = outbox id)
  R->>P: mark published (or schedule retry with backoff)
  N->>W: deliver (explicit ack)
  W->>P: claim with lease (one running deploy per target)
  W->>S: apply version, then probe health
  alt healthy
    W->>P: promote: current version = new version
  else unhealthy / timeout
    W->>P: deployment failed + rollback queued + outbox (same tx)
    N->>W: rollback.requested
    W->>S: re-apply previous stable version and probe
    W->>P: rollback succeeded / failed
  end
```

## Correctness properties and where they are enforced

| Property | Mechanism | Test |
|---|---|---|
| A requested deploy is never lost | deployment and outbox row commit together; relay retries | `TestCreateDeploymentRollsBackWhenOutboxInsertFails`, `TestOutboxRelayBacksOffAndRetries` |
| Retried requests do not create duplicates | unique `(organization_id, idempotency_key)`; GitHub delivery id as key | `TestIdempotencyKey`, `TestConcurrentCreatesWithSameKeyYieldOneDeployment`, `TestGitHubDeploymentWebhook` |
| Duplicate deliveries do not re-run work | lease claim + terminal-state check + completed-step skip + idempotent provider | `TestDuplicateDeliveryDoesNotReapply` |
| A crashed worker's deploy is resumed, not repeated | lease expiry, reclaim, fencing by `lease_owner` | `TestClaimIsExclusiveAndExpiredLeasesAreReclaimed`, `TestWorkerResumesAfterCrashWithoutRepeatingCompletedSteps` |
| One deploy at a time per service/environment | partial unique index on running deployments | `TestOnlyOneRunningDeploymentPerTarget` |
| Failed health means the last stable version is restored | separate `rollback_operations` row, re-probe after restore | `TestUnhealthyAndTimeoutDeploymentsRollBackAutomatically`, E2E |
| A failed rollback is not reported as success | rollback status is independent of deployment status | `TestFailedRollbackIsNotReportedAsRolledBack` |
| Tenants are isolated | every query filtered by `organization_id`; composite foreign keys | `TestTenantIsolation`, `TestTenantsCannotSeeEachOther`, E2E |
| Audit cannot be rewritten | trigger rejects `UPDATE`/`DELETE` on `audit_events` | `TestAuditEventsAreAppendOnly` |

See [docs/spec/release-lifecycle.md](docs/spec/release-lifecycle.md) for states and API, and
[docs/adr](docs/adr) for the outbox, idempotency and scope decisions.

## What was removed

Earlier iterations of this repository contained an OTLP ingest service, a WebSocket gateway, an
agent, an SDK, Redis, ClickHouse and several UI pages backed by mock data. None of them were part
of a tested end-to-end flow, so they were removed ([ADR 0003](docs/adr/0003-scope-of-the-first-slice.md)).
They are listed in the roadmap only where they would add a real capability.
