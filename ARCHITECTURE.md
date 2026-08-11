# ShipPulse Architecture Specification

## Distributed System Components

```
┌────────────────────────────────────────────────────────────────────────┐
│                        ShipPulse Control Plane                         │
│             Next.js 16 UI + Go REST/gRPC API Control Plane             │
└────────────┬────────────────────────────▲──────────────────────────────┘
             │                            │
             ▼                            │
┌──────────────────────────┐   ┌──────────┴───────────────┐
│ Control Plane API (Go)   │   │ Realtime Gateway (Go)    │
└────────────┬─────────────┘   └──────────▲───────────────┘
             │                            │
             └────────────┬───────────────┘
                          │ NATS JetStream Event Bus
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                     NATS JetStream Event Bus                    │
└────────────┬────────────────────────────┬───────────────────────┘
             │                            │
┌────────────▼─────────────┐   ┌──────────▼───────────────┐
│ Health & Alert Worker    │   │ OTLP Telemetry Ingest    │
└────────────┬─────────────┘   └──────────┬───────────────┘
             │                            │
┌────────────▼─────────────┐   ┌──────────▼───────────────┐
│ PostgreSQL 16            │   │ ClickHouse Columnar Store │
│ (Transactional Metadata) │   │ (Logs, Metrics, Traces)   │
└──────────────────────────┘   └──────────────────────────┘
```

## Data Layer Segmentation

1. **PostgreSQL 16**: Transactional tenant metadata (`users`, `organizations`, `memberships`, `projects`, `environments`, `services`, `deployments`, `incidents`, `monitors`, `slos`, `api_keys`, `agents`, `audit_events`).
2. **ClickHouse Columnar Database**: Time-series telemetry storage (`telemetry_logs`, `telemetry_metrics`, `telemetry_traces`). Partitioned by month with 14-day and 30-day retention policies.
3. **Redis 7**: Session caching, API rate limiting, and webhook delivery idempotency deduplication.
4. **NATS JetStream**: High-performance event bus streaming operational events (`shippulse.events.>`).
