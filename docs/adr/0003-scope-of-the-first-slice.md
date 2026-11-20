# ADR 0003: Narrow the repository to the release-safety slice

Date: 2026-11-20 · Status: accepted

## Context

The repository contained scaffolding for telemetry ingest (ClickHouse), a
WebSocket gateway, a host agent, an SDK, incidents, SLOs and monitors. None of it
was wired end to end, and the UI rendered hard-coded mock data, which implied
features that did not exist.

## Decision

Remove the unwired components instead of keeping dead code: `services/ingest`,
`services/realtime`, `agent/`, `packages/sdk`, the ClickHouse and OTel collector
containers, Redis, and the incident/SLO/monitor/agent/API-key handlers and UI
pages. They remain available in git history (before this change) and are listed
in the README roadmap only as ideas.

Keep a single Go module with one binary (`shippulse`) and subcommands
(`api`, `worker`, `simruntime`, `migrate`, `seed`, `demo`).

## Consequences

Smaller surface to test, audit and explain. Observability is limited to
structured JSON logs with `correlation_id`, `deployment_id` and `step`;
OpenTelemetry tracing is on the roadmap, not implemented.
