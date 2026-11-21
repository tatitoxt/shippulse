// Mirrors the JSON returned by the Go API (internal/store/models.go).

export interface Principal {
  user_id: string;
  email: string;
  full_name: string;
  organization_id: string;
  organization: string;
  role: "OWNER" | "ADMIN" | "ENGINEER" | "VIEWER";
}

export interface ServiceEnvironment {
  environment_id: string;
  environment_slug: string;
  environment_name: string;
  current_version: string | null;
  current_deployment_id: string | null;
  updated_at: string | null;
}

export interface Service {
  id: string;
  project_name: string;
  name: string;
  slug: string;
  repository: string | null;
  environments: ServiceEnvironment[];
}

export interface Step {
  ordinal: number;
  name: string;
  status: string;
  attempts: number;
  safe_output: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface Rollback {
  id: string;
  from_version: string;
  to_version: string;
  trigger: "automatic" | "manual";
  reason: string;
  status: string;
  error_code: string | null;
  safe_output: string;
  actor: string;
  created_at: string;
  finished_at: string | null;
}

export interface Deployment {
  id: string;
  service_id: string;
  service_slug: string;
  environment_id: string;
  environment_slug: string;
  version: string;
  failure_profile: string;
  source: "manual" | "github";
  actor: string;
  git_sha: string | null;
  status: string;
  previous_version: string | null;
  error_code: string | null;
  error_message: string | null;
  correlation_id: string;
  attempts: number;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  steps?: Step[];
  rollbacks?: Rollback[];
}

export interface AuditEvent {
  id: string;
  actor: string;
  action: string;
  subject_type: string;
  subject_id: string;
  correlation_id: string;
  metadata: Record<string, unknown>;
  occurred_at: string;
}

export interface RuntimeStatus {
  environment: string;
  stable_version: string | null;
  observed: { version: string; profile: string; applied_at: string; applies: number } | null;
  provider_error?: string;
}

export const TERMINAL = new Set(["succeeded", "failed", "cancelled"]);
export const FAILURE_PROFILES = ["healthy", "unhealthy", "timeout", "provider_unavailable"] as const;
