# Security

ShipPulse is a portfolio/demo project. It has not been security-audited and is not deployed
anywhere. Please report issues through GitHub issues on this repository (no real data is involved).

## Controls implemented

- **Tenant isolation**: every read and write is scoped by the `organization_id` from the caller's
  membership; projects, environments and services are tied together with composite foreign keys,
  so a service cannot be deployed to another tenant's environment. Cross-tenant ids return `404`.
- **Authentication**: bcrypt password hashes; HS256 JWTs (issuer `shippulse`, other algorithms
  rejected, `JWT_SECRET` must be at least 32 characters). Membership and role are re-read from
  PostgreSQL on every request, so removing a membership takes effect immediately.
- **Authorization**: roles `OWNER > ADMIN > ENGINEER > VIEWER`; deploy, rollback and cancel need
  `ENGINEER` or higher.
- **GitHub webhook**: HMAC-SHA256 over the raw body with constant-time comparison; 1 MiB body
  limit; `X-GitHub-Delivery` is required and used as the idempotency key. The endpoint is disabled
  when `GITHUB_WEBHOOK_SECRET` is empty.
- **Audit log**: append-only (enforced by a database trigger); metadata keys that look like
  secrets (`password`, `secret`, `token`, `authorization`, `signature`, `key`) are redacted.
- **Errors**: responses never include stack traces or SQL errors; each carries a correlation id
  that matches the server log line.
- **Containers**: the Go image is distroless and runs as non-root; demo ports are bound to
  `127.0.0.1`.

## Known gaps

No rate limiting or account lockout on login, no token revocation list, no TLS termination in
the Compose setup, and the demo credentials in `docker-compose.yml` are public defaults.

## Threat model (for security scans)

Updated with the reduced architecture; the maintainer's original answers about attackers and
accepted risk are kept.

### Components

| Component | Repository | Code it covers |
|---|---|---|
| ShipPulse API | shippulse | `internal/api` (login, services, deployments, rollback, cancel, audit, GitHub webhook), `internal/auth`, `internal/store` |
| ShipPulse Worker | shippulse | `internal/worker`, `internal/outbox`, `internal/bus`, `internal/provider`, `internal/simruntime` |
| ShipPulse Web | shippulse | `apps/web` (Next.js console and its `/api/v1` proxy to the API) |
| ShipPulse Data & Event Bus | shippulse | `docker-compose.yml` — PostgreSQL 16, NATS 2.11 with JetStream |

### Entry points and who can reach them

- API: public `POST /api/v1/auth/login` and `POST /api/v1/webhooks/github`; everything else
  requires a JWT. There is no public sign-up; users come from the seed.
- `simruntime` (`:8090`) has no authentication and is meant to be reachable only by the worker and
  API on the Compose network; it is published on `127.0.0.1` for local inspection.
- `docker-compose.yml` publishes PostgreSQL 5432, NATS 4222, API 8080, simruntime 8090 and web
  3000 on `127.0.0.1` only.

### Assets

Tenant metadata in PostgreSQL (users, memberships, deployments, audit events),
`GITHUB_WEBHOOK_SECRET`, `JWT_SECRET` and database passwords.

### Attackers that matter

- An authenticated user of one tenant attempting cross-tenant access, or database-level
  escalation (for example, PostgreSQL CVE-2026-116331 needs CREATE on a schema).
- A network attacker reaching published service ports.
- A forged or replayed GitHub webhook.

### Out of scope / accepted risk

- Denial of service (crashes, resource exhaustion) is accepted risk.
- No infrastructure repository exists. Deployment is defined only by `docker-compose.yml`.
