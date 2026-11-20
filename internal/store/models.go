package store

import (
	"encoding/json"
	"time"
)

type Principal struct {
	UserID         string `json:"user_id"`
	Email          string `json:"email"`
	FullName       string `json:"full_name"`
	OrganizationID string `json:"organization_id"`
	Organization   string `json:"organization"`
	Role           string `json:"role"`
}

type ServiceEnvironment struct {
	EnvironmentID       string     `json:"environment_id"`
	EnvironmentSlug     string     `json:"environment_slug"`
	EnvironmentName     string     `json:"environment_name"`
	CurrentVersion      *string    `json:"current_version"`
	CurrentDeploymentID *string    `json:"current_deployment_id"`
	UpdatedAt           *time.Time `json:"updated_at"`
}

type Service struct {
	ID             string               `json:"id"`
	OrganizationID string               `json:"organization_id"`
	ProjectID      string               `json:"project_id"`
	ProjectName    string               `json:"project_name"`
	Name           string               `json:"name"`
	Slug           string               `json:"slug"`
	Repository     *string              `json:"repository"`
	HealthPath     string               `json:"health_path"`
	Environments   []ServiceEnvironment `json:"environments"`
}

type Step struct {
	Ordinal    int        `json:"ordinal"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	SafeOutput string     `json:"safe_output"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type Rollback struct {
	ID                  string     `json:"id"`
	OrganizationID      string     `json:"organization_id"`
	TargetDeploymentID  string     `json:"target_deployment_id"`
	RestoreDeploymentID *string    `json:"restore_deployment_id"`
	FromVersion         string     `json:"from_version"`
	ToVersion           string     `json:"to_version"`
	Trigger             string     `json:"trigger"`
	Reason              string     `json:"reason"`
	Status              string     `json:"status"`
	ErrorCode           *string    `json:"error_code"`
	SafeOutput          string     `json:"safe_output"`
	CorrelationID       string     `json:"correlation_id"`
	Actor               string     `json:"actor"`
	Attempts            int        `json:"attempts"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
}

type Deployment struct {
	ID                   string     `json:"id"`
	OrganizationID       string     `json:"organization_id"`
	ServiceID            string     `json:"service_id"`
	ServiceSlug          string     `json:"service_slug"`
	EnvironmentID        string     `json:"environment_id"`
	EnvironmentSlug      string     `json:"environment_slug"`
	Version              string     `json:"version"`
	FailureProfile       string     `json:"failure_profile"`
	Source               string     `json:"source"`
	Actor                string     `json:"actor"`
	GitSHA               *string    `json:"git_sha"`
	Status               string     `json:"status"`
	PreviousVersion      *string    `json:"previous_version"`
	PreviousDeploymentID *string    `json:"previous_deployment_id"`
	ErrorCode            *string    `json:"error_code"`
	ErrorMessage         *string    `json:"error_message"`
	CorrelationID        string     `json:"correlation_id"`
	Attempts             int        `json:"attempts"`
	CreatedAt            time.Time  `json:"created_at"`
	StartedAt            *time.Time `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at"`
	Steps                []Step     `json:"steps,omitempty"`
	Rollbacks            []Rollback `json:"rollbacks,omitempty"`
}

type AuditEvent struct {
	ID            string          `json:"id"`
	Actor         string          `json:"actor"`
	Action        string          `json:"action"`
	SubjectType   string          `json:"subject_type"`
	SubjectID     string          `json:"subject_id"`
	CorrelationID string          `json:"correlation_id"`
	Metadata      json.RawMessage `json:"metadata"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

type OutboxEvent struct {
	ID             string
	OrganizationID string
	AggregateID    string
	EventType      string
	Payload        []byte
	CorrelationID  string
	Attempts       int
}

// CreateDeploymentInput is a validated request to deploy a version.
type CreateDeploymentInput struct {
	OrganizationID string
	ServiceID      string
	EnvironmentID  string
	Version        string
	FailureProfile string
	IdempotencyKey string
	Source         string
	Actor          string
	GitSHA         string
	CorrelationID  string
}
