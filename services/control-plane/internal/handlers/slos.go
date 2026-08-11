package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type SLOsHandler struct {
	Postgres *db.PostgresDB
}

func NewSLOsHandler(pg *db.PostgresDB) *SLOsHandler {
	return &SLOsHandler{Postgres: pg}
}

func (h *SLOsHandler) ListSLOs(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, service_id, name, target_percentage, time_window_days, slo_type, latency_threshold_ms, current_sli, error_budget_remaining, updated_at, created_at
		FROM slos
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []models.SLO
	for rows.Next() {
		var s models.SLO
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.ServiceID, &s.Name, &s.TargetPercentage, &s.TimeWindowDays, &s.SLOType, &s.LatencyThresholdMS, &s.CurrentSLI, &s.ErrorBudgetRemaining, &s.UpdatedAt, &s.CreatedAt); err == nil {
			list = append(list, s)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (h *SLOsHandler) CreateSLO(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.SLO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	req.CurrentSLI = 100.0
	req.ErrorBudgetRemaining = 100.0
	if req.TargetPercentage <= 0 {
		req.TargetPercentage = 99.9
	}
	if req.TimeWindowDays <= 0 {
		req.TimeWindowDays = 30
	}

	query := `
		INSERT INTO slos (organization_id, service_id, name, target_percentage, time_window_days, slo_type, latency_threshold_ms, current_sli, error_budget_remaining)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		req.OrganizationID, req.ServiceID, req.Name, req.TargetPercentage, req.TimeWindowDays,
		req.SLOType, req.LatencyThresholdMS, req.CurrentSLI, req.ErrorBudgetRemaining,
	).Scan(&req.ID, &req.CreatedAt, &req.UpdatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create SLO"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}
