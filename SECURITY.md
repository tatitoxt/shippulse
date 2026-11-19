# ShipPulse Security Model & Threat Boundary

## 1. Multi-Tenant Isolation
ShipPulse enforces strict server-side tenant isolation. Every database query, WebSocket channel, and background job is scoped explicitly by `organization_id`.

## 2. Agent Identity & Credential Security
- Agents communicate with the control plane via **outbound-only TLS** connections.
- Registration uses **one-time cryptographically random tokens** expiring in 1 hour.
- Long-lived agent identities use SHA-256 token hashing with token rotation.

## 3. Webhook Signature Verification
- Inbound GitHub webhooks require valid **HMAC-SHA256 signatures** matched against `GITHUB_WEBHOOK_SECRET`.
- Delivery deduplication via **Redis SETNX** prevents duplicate event processing.

## 4. Secret Masking & Audit Trails
- API keys are hashed using SHA-256; raw secrets are displayed only ONCE upon creation.
- Immutable `audit_events` records capture all operations without storing sensitive credentials or passwords.


## Threat model (for security scans)

Added 2026-11-19 with the maintainer's answers.

### Components

| Component | Repository | Code it covers |
|---|---|---|
| ShipPulse API | shippulse | `services/control-plane` (Go chi REST API: auth register/login, agents, GitHub webhooks, API keys, projects/services/deployments/incidents/monitors/SLOs) |
| ShipPulse Realtime & Ingest | shippulse | `services/realtime` (`/ws` WebSocket gateway), `services/ingest` (OTLP telemetry ingest), `services/worker` (health/alert worker), `agent/` (shippulse-agent) |
| ShipPulse Web | shippulse | `apps/web` (Next.js UI), `packages/` (contracts, sdk) |
| ShipPulse Data & Event Bus | shippulse | `docker-compose.yml`, `infra/` — PostgreSQL `postgres:16-alpine`, Redis 7, NATS `nats:2.10-alpine` (`-js -m 8222`, JetStream + monitoring, no MQTT config), ClickHouse 24.3, OTel collector 0.98 |

### Entry points and who can reach them

- API: public `POST /api/v1/auth/register`, `/auth/login`, `/agents/register` and `/webhooks/github`. All other routes sit behind JWT/`X-API-Key` middleware. **Anyone on the internet can get an account through public sign-up**, so an authenticated outside tenant is the main attacker.
- Realtime `/ws` and Ingest OTLP (4317/4318) are for agents and authenticated users.
- `docker-compose.yml` publishes PostgreSQL 5432, Redis 6379, NATS 4222/8222, ClickHouse 8123/9000 and OTel 4317/4318 on the host. Anyone who can reach the host can reach them.

### Assets

Tenant metadata in PostgreSQL (users, memberships, API keys, agents, audit events), telemetry in ClickHouse, `GITHUB_WEBHOOK_SECRET`, agent tokens and database passwords.

### Attackers that matter

- A self-registered tenant attempting cross-tenant access, or database-level escalation (for example, PostgreSQL CVE-2026-116331 needs CREATE on a schema).
- A network attacker reaching the published data-service ports.

### Out of scope / accepted risk

- Denial of service (crashes, resource exhaustion) is accepted risk.
- No infrastructure repository exists. Deployment is defined only by `docker-compose.yml` and `infra/`.
