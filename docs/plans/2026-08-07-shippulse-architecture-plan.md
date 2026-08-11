# ShipPulse Architecture & Implementation Plan

> **Goal:** Complete engineering specification and milestone execution roadmap for ShipPulse.
> **Repository:** [https://github.com/tatitoxt/shippulse](https://github.com/tatitoxt/shippulse)

---

## 0. Discovery Summary
- **GitHub Target Remote:** `https://github.com/tatitoxt/shippulse` (Verified PUBLIC)
- **Portfolio Live Site:** `https://fausto-pastura-resume.vercel.app` (Updated with ShipPulse as project 10)

---

## 1. System Architecture
- **Control Plane API:** Go (net/http, Chi, pgx/v5, JWT/Sessions, RBAC, Multi-tenancy)
- **Telemetry Ingestion:** Go (OpenTelemetry OTLP into ClickHouse)
- **Background Worker:** Go (Active HTTP/TCP probes, state machine, alert evaluation)
- **Realtime Gateway:** Go (WebSocket server connected to NATS JetStream)
- **Infrastructure Agent:** Go daemon with Docker deployment provider
- **Web UI:** Next.js 16, React 19, TypeScript, Tailwind CSS, Recharts, Command Palette (`⌘K`)
- **Databases:** PostgreSQL 16 (Transactional metadata), ClickHouse (Telemetry columnar store), Redis 7 (Caching & rate limiting), NATS JetStream (Event bus).

---

## 2. Milestones Summary (M0 - M30)
- **M0 - M5:** Monorepo foundation, Docker Compose stack, database migrations, contracts & SDK packages.
- **M6 - M16:** Control plane API, auth, RBAC, multi-tenancy, service catalog, deployments engine, GitHub webhooks, incidents, SLOs, API keys, seed generator.
- **M17 - M20:** OpenTelemetry ClickHouse ingest, background worker, realtime WebSocket gateway, agent binary.
- **M21 - M28:** Next.js 16 Web UI, Service Catalog SVG topology graph, Deployments & Rollbacks UI, Incident Command Center, Log & Trace Explorers, Command Palette.
- **M29 - M30:** Demo microservice lab, GitHub repository release, portfolio website integration.
