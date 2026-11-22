-- Approval gates: a deployment created with requires_approval stays in
-- awaiting_approval until an engineer approves it; only then is the worker
-- job enqueued.
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'awaiting_approval', 'running', 'succeeded', 'failed', 'cancelled'));
ALTER TABLE deployments
    ADD COLUMN requires_approval boolean NOT NULL DEFAULT false,
    ADD COLUMN approved_by       text,
    ADD COLUMN approved_at       timestamptz;
