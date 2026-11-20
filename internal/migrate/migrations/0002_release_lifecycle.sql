-- Release lifecycle: deployments, steps, rollbacks, outbox and audit trail.

-- Stable version currently promoted for a service in an environment.
CREATE TABLE service_environments (
    service_id             UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    environment_id         UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    organization_id        UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    current_version        TEXT,
    current_deployment_id  UUID,
    row_version            BIGINT NOT NULL DEFAULT 1,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (service_id, environment_id)
);

CREATE TABLE deployments (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id              UUID NOT NULL,
    environment_id          UUID NOT NULL,
    version                 TEXT NOT NULL CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    failure_profile         TEXT NOT NULL DEFAULT 'healthy'
                            CHECK (failure_profile IN ('healthy', 'unhealthy', 'timeout', 'provider_unavailable')),
    idempotency_key         TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    source                  TEXT NOT NULL CHECK (source IN ('manual', 'github')),
    actor                   TEXT NOT NULL,
    git_sha                 TEXT,
    status                  TEXT NOT NULL DEFAULT 'queued'
                            CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    previous_version        TEXT,
    previous_deployment_id  UUID REFERENCES deployments(id) ON DELETE SET NULL,
    error_code              TEXT,
    error_message           TEXT,
    correlation_id          TEXT NOT NULL,
    lease_owner             TEXT,
    lease_expires_at        TIMESTAMPTZ,
    attempts                INT NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at              TIMESTAMPTZ,
    finished_at             TIMESTAMPTZ,
    UNIQUE (organization_id, idempotency_key),
    FOREIGN KEY (organization_id, service_id) REFERENCES services(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, environment_id) REFERENCES environments(organization_id, id) ON DELETE CASCADE,
    CHECK ((status IN ('succeeded', 'failed', 'cancelled')) = (finished_at IS NOT NULL))
);

-- Only one deployment may run per service environment at a time.
CREATE UNIQUE INDEX deployments_one_running_per_target
    ON deployments (service_id, environment_id) WHERE status = 'running';
CREATE INDEX deployments_org_created ON deployments (organization_id, created_at DESC);
CREATE INDEX deployments_service_env_created ON deployments (service_id, environment_id, created_at DESC);

CREATE TABLE deployment_steps (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id  UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    ordinal        INT NOT NULL CHECK (ordinal > 0),
    name           TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    attempts       INT NOT NULL DEFAULT 0,
    safe_output    TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    UNIQUE (deployment_id, ordinal),
    UNIQUE (deployment_id, name)
);

CREATE TABLE rollback_operations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    target_deployment_id  UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    restore_deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL,
    from_version          TEXT NOT NULL,
    to_version            TEXT NOT NULL,
    trigger               TEXT NOT NULL CHECK (trigger IN ('automatic', 'manual')),
    reason                TEXT NOT NULL,
    idempotency_key       TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    error_code            TEXT,
    safe_output           TEXT NOT NULL DEFAULT '',
    correlation_id        TEXT NOT NULL,
    actor                 TEXT NOT NULL,
    lease_owner           TEXT,
    lease_expires_at      TIMESTAMPTZ,
    attempts              INT NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    UNIQUE (organization_id, idempotency_key)
);

CREATE UNIQUE INDEX rollback_one_active_per_target
    ON rollback_operations (target_deployment_id) WHERE status IN ('queued', 'running');

CREATE TABLE outbox_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    aggregate_id     UUID NOT NULL,
    event_type       TEXT NOT NULL,
    payload_version  INT NOT NULL DEFAULT 1,
    payload          JSONB NOT NULL,
    correlation_id   TEXT NOT NULL,
    attempts         INT NOT NULL DEFAULT 0,
    last_error       TEXT,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at     TIMESTAMPTZ
);

CREATE INDEX outbox_pending ON outbox_events (next_attempt_at) WHERE published_at IS NULL;

CREATE TABLE audit_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id    UUID NOT NULL,
    actor              TEXT NOT NULL,
    action             TEXT NOT NULL,
    subject_type       TEXT NOT NULL,
    subject_id         UUID NOT NULL,
    correlation_id     TEXT NOT NULL,
    metadata_redacted  JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_org_occurred ON audit_events (organization_id, occurred_at DESC);
CREATE INDEX audit_subject ON audit_events (subject_id, occurred_at);

-- Audit rows are append-only.
CREATE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_no_update
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
