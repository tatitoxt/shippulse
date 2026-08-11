package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type ServicesHandler struct {
	Postgres *db.PostgresDB
	Nats     *db.NatsClient
}

func NewServicesHandler(pg *db.PostgresDB, nats *db.NatsClient) *ServicesHandler {
	return &ServicesHandler{
		Postgres: pg,
		Nats:     nats,
	}
}

func (h *ServicesHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, project_id, name, slug, description, repository_url,
		       runtime, owner_team, health_endpoint, current_version, deployment_strategy,
		       tags, created_at, updated_at
		FROM services
		WHERE organization_id = $1
		ORDER BY name ASC
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Failed to fetch services"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var serviceList []models.Service
	for rows.Next() {
		var s models.Service
		err := rows.Scan(
			&s.ID, &s.OrganizationID, &s.ProjectID, &s.Name, &s.Slug, &s.Description, &s.RepositoryURL,
			&s.Runtime, &s.OwnerTeam, &s.HealthEndpoint, &s.CurrentVersion, &s.DeploymentStrategy,
			&s.Tags, &s.CreatedAt, &s.UpdatedAt,
		)
		if err == nil {
			serviceList = append(serviceList, s)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(serviceList)
}

func (h *ServicesHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.Service
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	if req.HealthEndpoint == "" {
		req.HealthEndpoint = "/health"
	}
	if req.Runtime == "" {
		req.Runtime = "docker"
	}
	if req.DeploymentStrategy == "" {
		req.DeploymentStrategy = "rolling"
	}

	query := `
		INSERT INTO services (organization_id, project_id, name, slug, description, repository_url, runtime, owner_team, health_endpoint, current_version, deployment_strategy, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at, updated_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		req.OrganizationID, req.ProjectID, req.Name, req.Slug, req.Description, req.RepositoryURL,
		req.Runtime, req.OwnerTeam, req.HealthEndpoint, req.CurrentVersion, req.DeploymentStrategy, req.Tags,
	).Scan(&req.ID, &req.CreatedAt, &req.UpdatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create service"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

type DependencyGraphNode struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Runtime        string   `json:"runtime"`
	HealthState    string   `json:"health_state"`
	DependsOnIDs   []string `json:"depends_on_ids"`
}

func (h *ServicesHandler) GetDependencyGraph(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	// Fetch services
	servicesQuery := `SELECT id, name, slug, runtime FROM services WHERE organization_id = $1`
	sRows, err := h.Postgres.Pool.Query(r.Context(), servicesQuery, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer sRows.Close()

	nodeMap := make(map[string]*DependencyGraphNode)
	for sRows.Next() {
		var n DependencyGraphNode
		if err := sRows.Scan(&n.ID, &n.Name, &n.Slug, &n.Runtime); err == nil {
			n.DependsOnIDs = []string{}
			n.HealthState = "HEALTHY" // Default state
			nodeMap[n.ID] = &n
		}
	}

	// Fetch dependencies
	depQuery := `SELECT service_id, depends_on_service_id FROM service_dependencies WHERE organization_id = $1`
	dRows, err := h.Postgres.Pool.Query(r.Context(), depQuery, orgID)
	if err == nil {
		defer dRows.Close()
		for dRows.Next() {
			var sID, depID string
			if err := dRows.Scan(&sID, &depID); err == nil {
				if node, ok := nodeMap[sID]; ok {
					node.DependsOnIDs = append(node.DependsOnIDs, depID)
				}
			}
		}
	}

	var graph []DependencyGraphNode
	for _, node := range nodeMap {
		graph = append(graph, *node)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(graph)
}

func (h *ServicesHandler) GetServiceByID(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	serviceID := chi.URLParam(r, "id")

	query := `
		SELECT id, organization_id, project_id, name, slug, description, repository_url,
		       runtime, owner_team, health_endpoint, current_version, deployment_strategy,
		       tags, created_at, updated_at
		FROM services
		WHERE organization_id = $1 AND id = $2
	`

	var s models.Service
	err := h.Postgres.Pool.QueryRow(r.Context(), query, orgID, serviceID).Scan(
		&s.ID, &s.OrganizationID, &s.ProjectID, &s.Name, &s.Slug, &s.Description, &s.RepositoryURL,
		&s.Runtime, &s.OwnerTeam, &s.HealthEndpoint, &s.CurrentVersion, &s.DeploymentStrategy,
		&s.Tags, &s.CreatedAt, &s.UpdatedAt,
	)

	if err != nil {
		http.Error(w, `{"error":"Service not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
