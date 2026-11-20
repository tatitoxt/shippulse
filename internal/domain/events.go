package domain

// Job event types published through the outbox. The subject on NATS is
// "shippulse.jobs." + type.
const (
	EventDeploymentRequested = "deployment.requested"
	EventRollbackRequested   = "rollback.requested"
)

// JobEvent is the versioned envelope carried on the bus. It contains IDs only;
// consumers always re-read authoritative state from PostgreSQL.
type JobEvent struct {
	EventID        string `json:"event_id"`
	Type           string `json:"type"`
	PayloadVersion int    `json:"payload_version"`
	OrganizationID string `json:"organization_id"`
	AggregateID    string `json:"aggregate_id"`
	CorrelationID  string `json:"correlation_id"`
}
