package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type ProjectsHandler struct {
	Postgres *db.PostgresDB
}

func NewProjectsHandler(pg *db.PostgresDB) *ProjectsHandler {
	return &ProjectsHandler{Postgres: pg}
}

func (h *ProjectsHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `SELECT id, organization_id, name, slug, description, created_at, updated_at FROM projects WHERE organization_id = $1 ORDER BY name ASC`
	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description, &p.CreatedAt, &p.UpdatedAt); err == nil {
			projects = append(projects, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (h *ProjectsHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.Project
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}

	tx, err := h.Postgres.Pool.Begin(r.Context())
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	query := `INSERT INTO projects (organization_id, name, slug, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`
	err = tx.QueryRow(r.Context(), query, req.OrganizationID, req.Name, req.Slug, req.Description).Scan(&req.ID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		http.Error(w, `{"error":"Failed to create project"}`, http.StatusInternalServerError)
		return
	}

	// Create default environments: Production, Staging, Development
	envs := []struct {
		name, slug, color string
	}{
		{"Production", "production", "#EF4444"},
		{"Staging", "staging", "#F59E0B"},
		{"Development", "development", "#10B981"},
	}

	for _, e := range envs {
		_, _ = tx.Exec(
			r.Context(),
			`INSERT INTO environments (organization_id, project_id, name, slug, color) VALUES ($1, $2, $3, $4, $5)`,
			orgID, req.ID, e.name, e.slug, e.color,
		)
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, `{"error":"Transaction failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

func (h *ProjectsHandler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `SELECT id, organization_id, project_id, name, slug, color, created_at FROM environments WHERE organization_id = $1 ORDER BY name ASC`
	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var envs []models.Environment
	for rows.Next() {
		var e models.Environment
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.ProjectID, &e.Name, &e.Slug, &e.Color, &e.CreatedAt); err == nil {
			envs = append(envs, e)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envs)
}
