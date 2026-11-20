# ADR 0001: Transactional outbox + NATS JetStream for deployment jobs

Date: 2026-11-20 · Status: accepted

## Context

The previous `CreateDeployment` handler inserted the deployment, then inserted
steps and published to NATS while ignoring both errors. A crash or a broker
outage could leave an "accepted" deployment that nobody would ever execute.

## Decision

- `POST /api/v1/deployments` writes the deployment, its steps, an `outbox_events`
  row and an audit event in **one PostgreSQL transaction**. Any error aborts the
  whole request.
- An outbox relay (running inside the worker process) polls pending rows with
  `FOR UPDATE SKIP LOCKED`, publishes them to JetStream with `Nats-Msg-Id = outbox id`
  and marks them published. Failures increment `attempts` and push
  `next_attempt_at` with capped exponential backoff.
- Delivery is at-least-once. Duplicates are expected and handled by the worker
  (ADR 0002).

## Consequences

- The API does not need NATS to accept work; pending rows are visible with
  `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`.
- Extra latency of up to one relay poll interval (default 500 ms).
- Several relays can run concurrently thanks to `SKIP LOCKED`.
