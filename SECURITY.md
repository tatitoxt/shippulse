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
