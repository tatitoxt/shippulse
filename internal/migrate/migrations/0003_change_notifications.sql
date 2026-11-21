-- Org-scoped change notifications for the SSE stream in the API.
-- Payloads stay tiny ({entity, deployment/service, status}); clients re-fetch
-- the resource, so notification bodies never carry full rows.

CREATE FUNCTION shippulse_notify_change() RETURNS trigger AS $$
DECLARE
    org_id    UUID;
    payload   TEXT;
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

    PERFORM pg_notify('shippulse_changes', jsonb_build_object('org', org_id, 'change', payload::jsonb)::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER deployments_notify
    AFTER INSERT OR UPDATE OF status ON deployments
    FOR EACH ROW EXECUTE FUNCTION shippulse_notify_change();

CREATE TRIGGER deployment_steps_notify
    AFTER INSERT OR UPDATE OF status ON deployment_steps
    FOR EACH ROW EXECUTE FUNCTION shippulse_notify_change();

CREATE TRIGGER service_environments_notify
    AFTER UPDATE OF current_version ON service_environments
    FOR EACH ROW EXECUTE FUNCTION shippulse_notify_change();
