export interface Service {
  id: string;
  name: string;
  slug: string;
  description: string;
  runtime: string;
  health_endpoint: string;
  current_version: string;
  deployment_strategy: string;
  owner_team: string;
  tags: string[];
}

export interface Deployment {
  id: string;
  service_id: string;
  version: string;
  git_sha: string;
  author: string;
  source: string;
  status: 'QUEUED' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELLED' | 'ROLLED_BACK';
  commit_message: string;
  created_at: string;
}

export interface Incident {
  id: string;
  title: string;
  summary: string;
  severity: 'SEV1' | 'SEV2' | 'SEV3' | 'SEV4';
  status: 'OPEN' | 'INVESTIGATING' | 'MONITORING' | 'RESOLVED';
  created_at: string;
}

export interface SLO {
  id: string;
  name: string;
  service_id: string;
  target_percentage: number;
  current_sli: number;
  error_budget_remaining: number;
}

// Fallback Mock Data for UI Standalone Mode
export const MOCK_SERVICES: Service[] = [
  {
    id: "srv-01",
    name: "Web Control Plane",
    slug: "web-dashboard",
    description: "Main developer control plane and operational UI",
    runtime: "docker",
    health_endpoint: "/api/health",
    current_version: "v2.4.1",
    deployment_strategy: "rolling",
    owner_team: "Frontend Ops",
    tags: ["nextjs", "frontend", "production"]
  },
  {
    id: "srv-02",
    name: "API Gateway & Router",
    slug: "api-gateway",
    description: "Central gRPC / REST API router and auth proxy",
    runtime: "kubernetes",
    health_endpoint: "/healthz",
    current_version: "v1.9.3",
    deployment_strategy: "canary",
    owner_team: "Core Backend",
    tags: ["go", "grpc", "gateway"]
  },
  {
    id: "srv-03",
    name: "Payments & Billing Engine",
    slug: "payments-service",
    description: "Stripe and subscription reconciliation service",
    runtime: "docker",
    health_endpoint: "/health",
    current_version: "v1.2.0",
    deployment_strategy: "blue-green",
    owner_team: "FinTech Ops",
    tags: ["billing", "payments", "critical"]
  },
  {
    id: "srv-04",
    name: "Async Background Worker",
    slug: "async-worker",
    description: "Distributed job queue worker for alerts and notifications",
    runtime: "docker",
    health_endpoint: "/health",
    current_version: "v3.0.1",
    deployment_strategy: "rolling",
    owner_team: "Platform Infra",
    tags: ["worker", "nats", "redis"]
  }
];

export const MOCK_DEPLOYMENTS: Deployment[] = [
  {
    id: "dep-104",
    service_id: "srv-02",
    version: "v1.9.3",
    git_sha: "a7f92bc",
    author: "Fausto Pastura",
    source: "github_actions",
    status: "SUCCEEDED",
    commit_message: "feat(auth): add OAuth2 token rotation & rate limiting",
    created_at: new Date(Date.now() - 15 * 60000).toISOString()
  },
  {
    id: "dep-103",
    service_id: "srv-03",
    version: "v1.2.1-rc1",
    git_sha: "f41a880",
    author: "CI Bot",
    source: "github_webhook",
    status: "FAILED",
    commit_message: "fix(stripe): update webhook signature verification logic",
    created_at: new Date(Date.now() - 45 * 60000).toISOString()
  },
  {
    id: "dep-102",
    service_id: "srv-01",
    version: "v2.4.1",
    git_sha: "9310cbe",
    author: "Fausto Pastura",
    source: "manual",
    status: "SUCCEEDED",
    commit_message: "feat(ui): add command palette ⌘K & trace waterfall view",
    created_at: new Date(Date.now() - 120 * 60000).toISOString()
  }
];

export const MOCK_INCIDENTS: Incident[] = [
  {
    id: "inc-42",
    title: "Intermittent Gateway 504 Timeout Spikes",
    summary: "Elevated HTTP 504 gateway timeouts observed in billing service upstream dependencies",
    severity: "SEV2",
    status: "INVESTIGATING",
    created_at: new Date(Date.now() - 10 * 60000).toISOString()
  }
];

export const MOCK_SLOS: SLO[] = [
  {
    id: "slo-01",
    name: "API Gateway Availability",
    service_id: "srv-02",
    target_percentage: 99.9,
    current_sli: 99.94,
    error_budget_remaining: 88.5
  },
  {
    id: "slo-02",
    name: "Payments p95 Latency < 500ms",
    service_id: "srv-03",
    target_percentage: 99.5,
    current_sli: 98.8,
    error_budget_remaining: 42.1
  }
];
