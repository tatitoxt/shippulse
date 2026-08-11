package contracts

import (
	"time"
)

// Event Subjects for NATS JetStream
const (
	SubjectDeploymentCreated   = "shippulse.events.deployment.created"
	SubjectDeploymentStarted   = "shippulse.events.deployment.started"
	SubjectDeploymentCompleted = "shippulse.events.deployment.completed"
	SubjectDeploymentFailed    = "shippulse.events.deployment.failed"
	SubjectServiceHealthChanged = "shippulse.events.service.health_changed"
	SubjectAgentConnected      = "shippulse.events.agent.connected"
	SubjectAgentDisconnected   = "shippulse.events.agent.disconnected"
	SubjectIncidentCreated     = "shippulse.events.incident.created"
	SubjectIncidentUpdated     = "shippulse.events.incident.updated"
	SubjectIncidentResolved    = "shippulse.events.incident.resolved"
	SubjectAlertTriggered      = "shippulse.events.alert.triggered"
	SubjectAlertResolved        = "shippulse.events.alert.resolved"
)

// Event Envelope
type Event struct {
	ID             string                 `json:"id"`
	Version        string                 `json:"version"`
	Type           string                 `json:"type"`
	OrganizationID string                 `json:"organization_id"`
	Timestamp      time.Time              `json:"timestamp"`
	CorrelationID  string                 `json:"correlation_id,omitempty"`
	CausationID    string                 `json:"causation_id,omitempty"`
	Payload        map[string]interface{} `json:"payload"`
}

// Typed Event Payloads
type DeploymentEventPayload struct {
	DeploymentID   string `json:"deployment_id"`
	ServiceID      string `json:"service_id"`
	ServiceName    string `json:"service_name"`
	EnvironmentID  string `json:"environment_id"`
	Version        string `json:"version"`
	GitSHA         string `json:"git_sha"`
	Author         string `json:"author"`
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

type ServiceHealthChangedPayload struct {
	ServiceID     string `json:"service_id"`
	ServiceName   string `json:"service_name"`
	EnvironmentID string `json:"environment_id"`
	PreviousState string `json:"previous_state"`
	NewState      string `json:"new_state"`
	Reason        string `json:"reason"`
}

type AgentConnectedPayload struct {
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	Hostname     string `json:"hostname"`
	RuntimeType  string `json:"runtime_type"`
}

type IncidentEventPayload struct {
	IncidentID  string `json:"incident_id"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	ServiceID   string `json:"service_id,omitempty"`
}

type AlertEventPayload struct {
	AlertEventID   string  `json:"alert_event_id"`
	AlertRuleID    string  `json:"alert_rule_id"`
	RuleName       string  `json:"rule_name"`
	ServiceID      string  `json:"service_id"`
	TriggeredValue float64 `json:"triggered_value"`
	Message        string  `json:"message"`
}
