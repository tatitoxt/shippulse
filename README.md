# ShipPulse 🚀

> **Developer-first deployment control plane, real-time microservice observability platform, incident detection engine, and infrastructure operations suite.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-16.0-000000.svg)](https://nextjs.org/)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-OTLP%20Native-f542a1.svg)](https://opentelemetry.io/)

ShipPulse bridges CI/CD pipelines (GitHub Actions, webhooks), customer runtime environments (Docker, Kubernetes), real-time observability (OpenTelemetry, ClickHouse), and developer operations into a single unified control plane.

---

## Architecture Overview

```
GitHub / CI Pipeline
       │
       │ deployment events (HMAC verified)
       ▼
┌─────────────────────────────────────────────────────────────────┐
│                    ShipPulse Control Plane                      │
│            Next.js 16 UI + Go REST/gRPC Control Plane           │
├─────────────────────────────────────────────────────────────────┤
│ Deployments │ Services Catalog │ Environments │ Health Probes │
│ Incident Engine │ OpenTelemetry Logs & Traces │ SLO Error Budgets│
└────────────────────────────────┬────────────────────────────────┘
                                 │
                   safe outbound │ TLS channel
                                 ▼
                    ┌─────────────────────────┐
                    │     ShipPulse Agent     │
                    └────────────┬────────────┘
                                 │
                   customer runtime adapter
                                 ▼
                     Docker / Kubernetes Cluster
                                 │
                     ┌───────────┴───────────┐
                     │     Microservices     │
                     └───────────────────────┘
```

---

## Key Features

- 🛰️ **Service Catalog & Dependency Graph**: Interactive SVG dependency topology, health state indicators, owner teams, and runtime mappings.
- 🚀 **Deployment Control Engine**: State machine tracking (`QUEUED`, `RUNNING`, `SUCCEEDED`, `FAILED`, `CANCELLED`, `ROLLED_BACK`), step-by-step logs, GitHub webhooks correlation, and one-click rollbacks.
- 🛡️ **Incident Command Center**: Automated operational timeline correlating failed deployments, health probe drops, and OpenTelemetry alerts.
- 📊 **OpenTelemetry & ClickHouse Telemetry**: High-throughput OTLP ingestion for logs, traces, and metrics with backpressure protection.
- 📈 **SLOs & Error Budgets**: Rolling 30-day availability and latency SLO tracking with error budget burn rate calculation.
- 🤖 **Secure Infrastructure Agent**: Outbound TLS agent daemon executing safe deployment operations without opening public inbound ports.
- 🔒 **Multi-Tenant & RBAC Security**: Server-side organization isolation, role-based authorization (`OWNER`, `ADMIN`, `ENGINEER`, `VIEWER`), and HMAC-SHA256 GitHub webhook verification.

---

## Monorepo Architecture

```
shippulse/
├── apps/
│   └── web/                 # Next.js 16, React 19, TypeScript, Tailwind CSS
├── services/
│   ├── control-plane/       # Go REST API, Auth, RBAC, Multi-Tenancy, NATS
│   ├── ingest/              # Go OTLP Telemetry Ingestion into ClickHouse
│   ├── worker/              # Go Background Worker (Health Probes, Alerts, SLOs)
│   └── realtime/            # Go WebSocket Gateway connected to NATS
├── agent/
│   └── shippulse-agent/     # Go Infrastructure Agent CLI & Daemon
├── packages/
│   ├── contracts/           # Shared Go event schemas and domain types
│   └── sdk/                 # Go SDK for microservice telemetry integration
├── infra/
│   ├── docker/              # Docker Compose (PostgreSQL, ClickHouse, Redis, NATS, OTel)
│   ├── kubernetes/          # K8s Deployment Manifests
│   └── otel/                # OpenTelemetry Collector Config
├── demo/                    # End-to-end microservice demo lab
└── docs/                    # Architecture, security, protocol, and API specs
```

---

## Quick Start (Local Development)

### Prerequisites
- Docker & Docker Compose
- Go 1.22+
- Node.js v20+

### 1. Clone & Start Infrastructure
```bash
git clone https://github.com/tatitoxt/shippulse.git
cd shippulse

# Start PostgreSQL, ClickHouse, Redis, NATS, and OTel Collector
docker compose up -d
```

### 2. Build Services & Seed Demo Data
```bash
# Build all Go binaries
make build

# Seed PostgreSQL with realistic demo data
make seed
```

### 3. Run Control Plane & Web UI
```bash
# Terminal 1: Run Control Plane API
cd services/control-plane && go run ./cmd/server

# Terminal 2: Run Next.js Web Dashboard
cd apps/web && npm run dev
```

Open the URL configured through `WEB_ORIGIN`. Seed credentials must be supplied
through `DEMO_EMAIL` and `DEMO_PASSWORD`; they are never stored in the repository.

---

## License

ShipPulse is open-source software licensed under the [MIT License](LICENSE).
