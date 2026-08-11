package models

import (
	"time"
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OrganizationMembership struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Role           string    `json:"role"` // OWNER, ADMIN, ENGINEER, VIEWER
	CreatedAt      time.Time `json:"created_at"`
}

type Project struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Environment struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Color          string    `json:"color"`
	CreatedAt      time.Time `json:"created_at"`
}

type Service struct {
	ID                 string    `json:"id"`
	OrganizationID     string    `json:"organization_id"`
	ProjectID          string    `json:"project_id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	Description        string    `json:"description,omitempty"`
	RepositoryURL      string    `json:"repository_url,omitempty"`
	Runtime            string    `json:"runtime"`
	OwnerTeam          string    `json:"owner_team,omitempty"`
	HealthEndpoint     string    `json:"health_endpoint"`
	CurrentVersion     string    `json:"current_version"`
	DeploymentStrategy string    `json:"deployment_strategy"`
	Tags               []string  `json:"tags"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ServiceDependency struct {
	ID                 string    `json:"id"`
	OrganizationID     string    `json:"organization_id"`
	ServiceID          string    `json:"service_id"`
	DependsOnServiceID string    `json:"depends_on_service_id"`
	DependencyType     string    `json:"dependency_type"`
	CreatedAt          time.Time `json:"created_at"`
}

type Deployment struct {
	ID                         string     `json:"id"`
	OrganizationID             string     `json:"organization_id"`
	ProjectID                  string     `json:"project_id"`
	EnvironmentID              string     `json:"environment_id"`
	ServiceID                  string     `json:"service_id"`
	Version                    string     `json:"version"`
	GitSHA                     string     `json:"git_sha,omitempty"`
	GitBranch                  string     `json:"git_branch,omitempty"`
	CommitMessage              string     `json:"commit_message,omitempty"`
	Author                     string     `json:"author,omitempty"`
	Source                     string     `json:"source"`
	Status                     string     `json:"status"` // QUEUED, RUNNING, SUCCEEDED, FAILED, CANCELLED, ROLLED_BACK
	DeploymentStrategy         string     `json:"deployment_strategy"`
	ImageTag                   string     `json:"image_tag,omitempty"`
	RollbackTargetDeploymentID *string    `json:"rollback_target_deployment_id,omitempty"`
	StartedAt                  *time.Time `json:"started_at,omitempty"`
	FinishedAt                 *time.Time `json:"finished_at,omitempty"`
	ErrorMessage               string     `json:"error_message,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
}

type DeploymentStep struct {
	ID           string     `json:"id"`
	DeploymentID string     `json:"deployment_id"`
	StepOrder    int        `json:"step_order"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	LogOutput    string     `json:"log_output,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type Incident struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	ProjectID      string     `json:"project_id"`
	EnvironmentID  *string    `json:"environment_id,omitempty"`
	ServiceID      *string    `json:"service_id,omitempty"`
	DeploymentID   *string    `json:"deployment_id,omitempty"`
	Title          string     `json:"title"`
	Summary        string     `json:"summary,omitempty"`
	Severity       string     `json:"severity"` // SEV1, SEV2, SEV3, SEV4
	Status         string     `json:"status"`   // OPEN, INVESTIGATING, MONITORING, RESOLVED
	OwnerUserID    *string    `json:"owner_user_id,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	DetectedAt     time.Time  `json:"detected_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type IncidentEvent struct {
	ID         string                 `json:"id"`
	IncidentID string                 `json:"incident_id"`
	EventType  string                 `json:"event_type"`
	Message    string                 `json:"message"`
	Actor      string                 `json:"actor"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

type Monitor struct {
	ID                           string     `json:"id"`
	OrganizationID               string     `json:"organization_id"`
	ServiceID                    string     `json:"service_id"`
	EnvironmentID                string     `json:"environment_id"`
	Name                         string     `json:"name"`
	Type                         string     `json:"type"` // HTTP, TCP
	EndpointURL                  string     `json:"endpoint_url"`
	IntervalSeconds              int        `json:"interval_seconds"`
	TimeoutSeconds               int        `json:"timeout_seconds"`
	ExpectedStatus               int        `json:"expected_status"`
	ConsecutiveFailureThreshold int        `json:"consecutive_failure_threshold"`
	ConsecutiveRecoveryThreshold int        `json:"consecutive_recovery_threshold"`
	CurrentState                 string     `json:"current_state"` // UNKNOWN, HEALTHY, DEGRADED, UNHEALTHY
	LastCheckAt                  *time.Time `json:"last_check_at,omitempty"`
	ConsecutiveFailures          int        `json:"consecutive_failures"`
	ConsecutiveSuccesses         int        `json:"consecutive_successes"`
	CreatedAt                    time.Time  `json:"created_at"`
}

type AlertRule struct {
	ID                       string    `json:"id"`
	OrganizationID           string    `json:"organization_id"`
	ServiceID                *string   `json:"service_id,omitempty"`
	Name                     string    `json:"name"`
	MetricName               string    `json:"metric_name"`
	ConditionOperator        string    `json:"condition_operator"`
	ThresholdValue           float64   `json:"threshold_value"`
	EvaluationWindowMinutes  int       `json:"evaluation_window_minutes"`
	Enabled                  bool      `json:"enabled"`
	WebhookURL               string    `json:"webhook_url,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
}

type AlertEvent struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	AlertRuleID    string     `json:"alert_rule_id"`
	ServiceID      *string    `json:"service_id,omitempty"`
	TriggeredValue float64    `json:"triggered_value"`
	Status         string     `json:"status"`
	Message        string     `json:"message"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

type SLO struct {
	ID                    string    `json:"id"`
	OrganizationID        string    `json:"organization_id"`
	ServiceID             string    `json:"service_id"`
	Name                  string    `json:"name"`
	TargetPercentage      float64   `json:"target_percentage"`
	TimeWindowDays        int       `json:"time_window_days"`
	SLOType               string    `json:"slo_type"`
	LatencyThresholdMS    int       `json:"latency_threshold_ms,omitempty"`
	CurrentSLI            float64   `json:"current_sli"`
	ErrorBudgetRemaining float64   `json:"error_budget_remaining"`
	UpdatedAt             time.Time `json:"updated_at"`
	CreatedAt             time.Time `json:"created_at"`
}

type APIKey struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	KeyPrefix      string     `json:"key_prefix"`
	Scopes         []string   `json:"scopes"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Agent struct {
	ID              string     `json:"id"`
	OrganizationID  string     `json:"organization_id"`
	Name            string     `json:"name"`
	Hostname        string     `json:"hostname,omitempty"`
	IPAddress       string     `json:"ip_address,omitempty"`
	Version         string     `json:"version,omitempty"`
	RuntimeType     string     `json:"runtime_type"`
	Capabilities    []string   `json:"capabilities"`
	Status          string     `json:"status"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type AuditEvent struct {
	ID             string                 `json:"id"`
	OrganizationID string                 `json:"organization_id"`
	ActorID        *string                `json:"actor_id,omitempty"`
	ActorEmail     string                 `json:"actor_email,omitempty"`
	Action         string                 `json:"action"`
	ResourceType   string                 `json:"resource_type"`
	ResourceID     string                 `json:"resource_id,omitempty"`
	IPAddress      string                 `json:"ip_address,omitempty"`
	Details        map[string]interface{} `json:"details,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}
