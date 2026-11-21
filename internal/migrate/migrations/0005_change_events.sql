-- Persisted change events so SSE clients can replay what they missed with
-- Last-Event-ID. The notify payload now carries the row id; rows are pruned
-- after an hour (events carry only tiny change descriptors).
CREATE TABLE change_events (
    id          bigserial PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    change      jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX change_events_org_id_idx ON change_events (organization_id, id);

CREATE OR REPLACE FUNCTION shippulse_notify_change() RETURNS trigger AS $$
DECLARE
    org_id    UUID;
    payload   TEXT;
    row_id    BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'deployments' THEN
        payload := json_build_object(
            'entity', 'deployment',
            'id', NEW.id,
            'status', NEW.status
        )::text;
        org_id := NEW.organization_id;
    ELSIF TG_TABLE_NAME = 'deployment_steps' THEN
        SELECT d.organization_id INTO org_id
        FROM deployments d
        WHERE d.id = NEW.deployment_id;
        payload := json_build_object(
            'entity', 'step',
            'deployment_id', NEW.deployment_id,
            'step', NEW.name,
            'status', NEW.status
        )::text;
    ELSE -- service_environments
        payload := json_build_object(
            'entity', 'service_environment',
            'service_id', NEW.service_id,
            'environment_id', NEW.environment_id,
            'version', NEW.current_version
        )::text;
        org_id := NEW.organization_id;
    END IF;

    INSERT INTO change_events (organization_id, change) VALUES (org_id, payload::jsonb)
        RETURNING id INTO row_id;
    PERFORM pg_notify('shippulse_changes', jsonb_build_object('org', org_id, 'change', payload::jsonb, 'seq', row_id)::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
