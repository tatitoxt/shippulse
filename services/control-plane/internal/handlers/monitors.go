package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type MonitorsHandler struct {
	Postgres *db.PostgresDB
}

func NewMonitorsHandler(pg *db.PostgresDB) *MonitorsHandler {
	return &MonitorsHandler{Postgres: pg}
}

func (h *MonitorsHandler) ListMonitors(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, service_id, environment_id, name, type, endpoint_url,
		       interval_seconds, timeout_seconds, expected_status, consecutive_failure_threshold,
		       consecutive_recovery_threshold, current_state, last_check_at, consecutive_failures,
		       consecutive_successes, created_at
		FROM monitors
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var monitors []models.Monitor
	for rows.Next() {
		var m models.Monitor
		if err := rows.Scan(
			&m.ID, &m.OrganizationID, &m.ServiceID, &m.EnvironmentID, &m.Name, &m.Type, &m.EndpointURL,
			&m.IntervalSeconds, &m.TimeoutSeconds, &m.ExpectedStatus, &m.ConsecutiveFailureThreshold,
			&m.ConsecutiveRecoveryThreshold, &m.CurrentState, &m.LastCheckAt, &m.ConsecutiveFailures,
			&m.ConsecutiveSuccesses, &m.CreatedAt,
		); err == nil {
			monitors = append(monitors, m)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(monitors)
}

func (h *MonitorsHandler) CreateMonitor(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.Monitor
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	req.CurrentState = "UNKNOWN"
	if req.Type == "" {
		req.Type = "HTTP"
	}
	if req.IntervalSeconds <= 0 {
		req.IntervalSeconds = 15
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 5
	}
	if req.ExpectedStatus <= 0 {
		req.ExpectedStatus = 200
	}
	if req.ConsecutiveFailureThreshold <= 0 {
		req.ConsecutiveFailureThreshold = 3
	}
	if req.ConsecutiveRecoveryThreshold <= 0 {
		req.ConsecutiveRecoveryThreshold = 2
	}

	query := `
		INSERT INTO monitors (organization_id, service_id, environment_id, name, type, endpoint_url, interval_seconds, timeout_seconds, expected_status, consecutive_failure_threshold, consecutive_recovery_threshold, current_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		req.OrganizationID, req.ServiceID, req.EnvironmentID, req.Name, req.Type, req.EndpointURL,
		req.IntervalSeconds, req.TimeoutSeconds, req.ExpectedStatus, req.ConsecutiveFailureThreshold,
		req.ConsecutiveRecoveryThreshold, req.CurrentState,
	).Scan(&req.ID, &req.CreatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create monitor"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}
