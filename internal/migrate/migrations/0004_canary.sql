-- Progressive (canary) deploys: per-deployment traffic weights. An empty
-- array is a direct deploy; otherwise the worker inserts canary_<pct>
-- steps before promote and shifts simulated traffic through each weight.
ALTER TABLE deployments ADD COLUMN canary_weights integer[] NOT NULL DEFAULT '{}';
