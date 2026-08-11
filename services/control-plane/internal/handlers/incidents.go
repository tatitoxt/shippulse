package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tatitoxt/shippulse/packages/contracts"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type IncidentsHandler struct {
	Postgres *db.PostgresDB
	Nats     *db.NatsClient
}

func NewIncidentsHandler(pg *db.PostgresDB, nats *db.NatsClient) *IncidentsHandler {
	return &IncidentsHandler{
		Postgres: pg,
		Nats:     nats,
	}
}

func (h *IncidentsHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, project_id, environment_id, service_id, deployment_id,
		       title, summary, severity, status, owner_user_id, started_at, detected_at, resolved_at, created_at, updated_at
		FROM incidents
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch incidents"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var incidents []models.Incident
	for rows.Next() {
		var inc models.Incident
		err := rows.Scan(
			&inc.ID, &inc.OrganizationID, &inc.ProjectID, &inc.EnvironmentID, &inc.ServiceID, &inc.DeploymentID,
			&inc.Title, &inc.Summary, &inc.Severity, &inc.Status, &inc.OwnerUserID,
			&inc.StartedAt, &inc.DetectedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt,
		)
		if err == nil {
			incidents = append(incidents, inc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(incidents)
}

func (h *IncidentsHandler) CreateIncident(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.Incident
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	req.Status = "OPEN"
	if req.Severity == "" {
		req.Severity = "SEV2"
	}

	query := `
		INSERT INTO incidents (organization_id, project_id, environment_id, service_id, deployment_id, title, summary, severity, status, detected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		RETURNING id, started_at, detected_at, created_at, updated_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		req.OrganizationID, req.ProjectID, req.EnvironmentID, req.ServiceID, req.DeploymentID,
		req.Title, req.Summary, req.Severity, req.Status,
	).Scan(&req.ID, &req.StartedAt, &req.DetectedAt, &req.CreatedAt, &req.UpdatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create incident"}`, http.StatusInternalServerError)
		return
	}

	// Add initial timeline event
	_, _ = h.Postgres.Pool.Exec(
		r.Context(),
		`INSERT INTO incident_events (incident_id, event_type, message, actor) VALUES ($1, 'incident_created', $2, 'System')`,
		req.ID, "Incident opened: "+req.Title,
	)

	// Publish NATS event
	_ = h.Nats.PublishEvent(contracts.SubjectIncidentCreated, contracts.Event{
		ID:             uuid.New().String(),
		Version:        "1.0",
		Type:           contracts.SubjectIncidentCreated,
		OrganizationID: orgID,
		Payload: map[string]interface{}{
			"incident_id": req.ID,
			"title":       req.Title,
			"severity":    req.Severity,
			"status":      req.Status,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

func (h *IncidentsHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	incidentID := chi.URLParam(r, "id")

	var payload struct {
		Status  string `json:"status"`  // OPEN, INVESTIGATING, MONITORING, RESOLVED
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	query := `UPDATE incidents SET status = $1, summary = COALESCE($2, summary), updated_at = NOW()`
	if payload.Status == "RESOLVED" {
		query += `, resolved_at = NOW()`
	}
	query += ` WHERE id = $3 AND organization_id = $4`

	_, err := h.Postgres.Pool.Exec(r.Context(), query, payload.Status, payload.Summary, incidentID, orgID)
	if err != nil {
		http.Error(w, `{"error":"Failed to update incident"}`, http.StatusInternalServerError)
		return
	}

	// Add timeline event
	_, _ = h.Postgres.Pool.Exec(
		r.Context(),
		`INSERT INTO incident_events (incident_id, event_type, message, actor) VALUES ($1, 'status_changed', $2, 'Engineer')`,
		incidentID, "Incident status updated to "+payload.Status,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": payload.Status})
}

func (h *IncidentsHandler) GetIncidentByID(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	incidentID := chi.URLParam(r, "id")

	var inc models.Incident
	query := `
		SELECT id, organization_id, project_id, environment_id, service_id, deployment_id,
		       title, summary, severity, status, owner_user_id, started_at, detected_at, resolved_at, created_at, updated_at
		FROM incidents
		WHERE id = $1 AND organization_id = $2
	`

	err := h.Postgres.Pool.QueryRow(r.Context(), query, incidentID, orgID).Scan(
		&inc.ID, &inc.OrganizationID, &inc.ProjectID, &inc.EnvironmentID, &inc.ServiceID, &inc.DeploymentID,
		&inc.Title, &inc.Summary, &inc.Severity, &inc.Status, &inc.OwnerUserID,
		&inc.StartedAt, &inc.DetectedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt,
	)

	if err != nil {
		http.Error(w, `{"error":"Incident not found"}`, http.StatusNotFound)
		return
	}

	// Fetch timeline
	tRows, err := h.Postgres.Pool.Query(r.Context(), `SELECT id, incident_id, event_type, message, actor, created_at FROM incident_events WHERE incident_id = $1 ORDER BY created_at ASC`, incidentID)
	var events []models.IncidentEvent
	if err == nil {
		defer tRows.Close()
		for tRows.Next() {
			var ev models.IncidentEvent
			if err := tRows.Scan(&ev.ID, &ev.IncidentID, &ev.EventType, &ev.Message, &ev.Actor, &ev.CreatedAt); err == nil {
				events = append(events, ev)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"incident": inc,
		"timeline": events,
	})
}
