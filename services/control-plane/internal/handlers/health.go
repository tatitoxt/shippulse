package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type HealthAndAuditHandler struct {
	Postgres *db.PostgresDB
}

func NewHealthAndAuditHandler(pg *db.PostgresDB) *HealthAndAuditHandler {
	return &HealthAndAuditHandler{Postgres: pg}
}

func (h *HealthAndAuditHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "shippulse-control-plane",
	})
}

func (h *HealthAndAuditHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, actor_id, actor_email, action, resource_type, resource_id, ip_address, details, created_at
		FROM audit_events
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []models.AuditEvent
	for rows.Next() {
		var a models.AuditEvent
		if err := rows.Scan(&a.ID, &a.OrganizationID, &a.ActorID, &a.ActorEmail, &a.Action, &a.ResourceType, &a.ResourceID, &a.IPAddress, &a.Details, &a.CreatedAt); err == nil {
			logs = append(logs, a)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
