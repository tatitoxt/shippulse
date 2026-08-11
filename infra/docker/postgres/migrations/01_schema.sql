-- ShipPulse PostgreSQL Schema Definition v1.0
-- Enforces tenant isolation via organization_id on all tenant resources

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    full_name VARCHAR(255) NOT NULL,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Organizations
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Organization Memberships (RBAC)
CREATE TYPE user_role AS ENUM ('OWNER', 'ADMIN', 'ENGINEER', 'VIEWER');

CREATE TABLE organization_memberships (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role user_role NOT NULL DEFAULT 'ENGINEER',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(organization_id, user_id)
);

-- 4. Projects
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(organization_id, slug)
);

-- 5. Environments
CREATE TABLE environments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL, -- e.g. Production, Staging, Development
    slug VARCHAR(100) NOT NULL,
    color VARCHAR(50) DEFAULT '#10B981',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, slug)
);

-- 6. Services
CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    description TEXT,
    repository_url TEXT,
    runtime VARCHAR(100) NOT NULL DEFAULT 'docker', -- docker, kubernetes, serverless
    owner_team VARCHAR(255),
    health_endpoint VARCHAR(255) DEFAULT '/health',
    current_version VARCHAR(100) DEFAULT 'v0.1.0',
    deployment_strategy VARCHAR(50) DEFAULT 'rolling', -- rolling, blue-green, canary
    tags TEXT[] DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, slug)
);

-- 7. Service Dependencies
CREATE TABLE service_dependencies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    depends_on_service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    dependency_type VARCHAR(50) DEFAULT 'HTTP', -- HTTP, gRPC, DB, Queue
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(service_id, depends_on_service_id)
);

-- 8. Deployments
CREATE TYPE deployment_status AS ENUM ('QUEUED', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'ROLLED_BACK');

CREATE TABLE deployments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    version VARCHAR(100) NOT NULL,
    git_sha VARCHAR(100),
    git_branch VARCHAR(100) DEFAULT 'main',
    commit_message TEXT,
    author VARCHAR(255),
    source VARCHAR(100) DEFAULT 'github_actions', -- github_webhooks, manual, agent
    status deployment_status NOT NULL DEFAULT 'QUEUED',
    deployment_strategy VARCHAR(50) DEFAULT 'rolling',
    image_tag VARCHAR(255),
    rollback_target_deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 9. Deployment Steps
CREATE TABLE deployment_steps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    deployment_id UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    step_order INT NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING', -- PENDING, IN_PROGRESS, SUCCESS, FAILED
    log_output TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

-- 10. Incidents
CREATE TYPE incident_status AS ENUM ('OPEN', 'INVESTIGATING', 'MONITORING', 'RESOLVED');
CREATE TYPE incident_severity AS ENUM ('SEV1', 'SEV2', 'SEV3', 'SEV4');

CREATE TABLE incidents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id UUID REFERENCES environments(id) ON DELETE SET NULL,
    service_id UUID REFERENCES services(id) ON DELETE SET NULL,
    deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL,
    summary TEXT,
    severity incident_severity NOT NULL DEFAULT 'SEV2',
    status incident_status NOT NULL DEFAULT 'OPEN',
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 11. Incident Events Timeline
CREATE TABLE incident_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL, -- deployment_started, alert_triggered, status_changed, note_added, rollback_initiated
    message TEXT NOT NULL,
    actor VARCHAR(255) DEFAULT 'System',
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 12. Monitors & Health Checks
CREATE TYPE monitor_type AS ENUM ('HTTP', 'TCP');
CREATE TYPE health_state AS ENUM ('UNKNOWN', 'HEALTHY', 'DEGRADED', 'UNHEALTHY');

CREATE TABLE monitors (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type monitor_type NOT NULL DEFAULT 'HTTP',
    endpoint_url TEXT NOT NULL,
    interval_seconds INT NOT NULL DEFAULT 15,
    timeout_seconds INT NOT NULL DEFAULT 5,
    expected_status INT DEFAULT 200,
    consecutive_failure_threshold INT DEFAULT 3,
    consecutive_recovery_threshold INT DEFAULT 2,
    current_state health_state NOT NULL DEFAULT 'UNKNOWN',
    last_check_at TIMESTAMPTZ,
    consecutive_failures INT DEFAULT 0,
    consecutive_successes INT DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE health_check_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    monitor_id UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    status_code INT,
    latency_ms INT,
    is_healthy BOOLEAN NOT NULL,
    error_message TEXT,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 13. Alert Rules & Events
CREATE TABLE alert_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id UUID REFERENCES services(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    metric_name VARCHAR(100) NOT NULL, -- error_rate, p95_latency, health_failure, agent_offline
    condition_operator VARCHAR(10) NOT NULL DEFAULT '>', -- >, <, >=, <=, ==
    threshold_value DOUBLE PRECISION NOT NULL,
    evaluation_window_minutes INT NOT NULL DEFAULT 5,
    enabled BOOLEAN DEFAULT true,
    webhook_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE alert_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    alert_rule_id UUID NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    service_id UUID REFERENCES services(id) ON DELETE CASCADE,
    triggered_value DOUBLE PRECISION NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'TRIGGERED', -- TRIGGERED, RESOLVED
    message TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

-- 14. Service Level Objectives (SLOs)
CREATE TABLE slos (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    target_percentage DOUBLE PRECISION NOT NULL DEFAULT 99.9, -- e.g. 99.9%
    time_window_days INT NOT NULL DEFAULT 30,
    slo_type VARCHAR(50) NOT NULL DEFAULT 'AVAILABILITY', -- AVAILABILITY, LATENCY
    latency_threshold_ms INT DEFAULT 500,
    current_sli DOUBLE PRECISION DEFAULT 100.0,
    error_budget_remaining DOUBLE PRECISION DEFAULT 100.0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 15. API Keys
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_prefix VARCHAR(20) NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{"deployments:read", "deployments:write", "services:read"}',
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 16. Agents & Tokens
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    hostname VARCHAR(255),
    ip_address VARCHAR(100),
    version VARCHAR(50),
    runtime_type VARCHAR(50) DEFAULT 'docker', -- docker, kubernetes
    capabilities TEXT[] DEFAULT '{"docker:deploy", "docker:rollback", "health:check"}',
    status VARCHAR(50) NOT NULL DEFAULT 'OFFLINE', -- ONLINE, OFFLINE, DEGRADED
    last_heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE agent_registration_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    is_used BOOLEAN DEFAULT false,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 17. Audit Events
CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_email VARCHAR(255),
    action VARCHAR(100) NOT NULL, -- e.g. deployment.created, agent.registered, key.revoked
    resource_type VARCHAR(100) NOT NULL,
    resource_id VARCHAR(255),
    ip_address VARCHAR(100),
    details JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Performance Indexes
CREATE INDEX idx_memberships_org_user ON organization_memberships(organization_id, user_id);
CREATE INDEX idx_projects_org ON projects(organization_id);
CREATE INDEX idx_services_org_project ON services(organization_id, project_id);
CREATE INDEX idx_deployments_org_service ON deployments(organization_id, service_id);
CREATE INDEX idx_deployments_status ON deployments(status);
CREATE INDEX idx_incidents_org_status ON incidents(organization_id, status);
CREATE INDEX idx_monitors_org_service ON monitors(organization_id, service_id);
CREATE INDEX idx_audit_org_created ON audit_events(organization_id, created_at DESC);
