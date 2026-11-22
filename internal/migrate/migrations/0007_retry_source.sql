-- Deployment retry: a terminal deployment can be cloned into a fresh run with
-- source 'retry'.
ALTER TABLE deployments DROP CONSTRAINT deployments_source_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_source_check
    CHECK (source IN ('manual', 'github', 'retry'));
