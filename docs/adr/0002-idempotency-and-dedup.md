# ADR 0002: Idempotency keys and durable claims in PostgreSQL

Date: 2026-11-20 · Status: accepted

## Context

Clients retry, GitHub redelivers webhooks and JetStream redelivers unacked
messages. Redis `SETNX` was used for webhook dedup, but it is a second source of
truth and not part of the deployment transaction.

## Decision

- `deployments` has `UNIQUE (organization_id, idempotency_key)`; rollback
  operations have the same constraint. A replay returns the existing row.
- GitHub deliveries use `github:<X-GitHub-Delivery>` as idempotency key, so the
  dedup lives in the same transaction as the deployment.
- The worker *claims* a job with a conditional `UPDATE ... WHERE status='queued'
  OR (status='running' AND lease_expires_at < now())`. Terminal jobs are acked
  and skipped. Expired leases let another worker take over a crashed job.
- Completed steps are skipped on resume; `apply_version` is idempotent in the
  provider (applying the same version twice is a no-op).
- A partial unique index allows only one `running` deployment per
  (service, environment).

## Consequences

Redis is no longer needed on the critical path and was removed from the stack.
