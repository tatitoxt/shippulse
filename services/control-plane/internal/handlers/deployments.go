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

type DeploymentsHandler struct {
	Postgres *db.PostgresDB
	Nats     *db.NatsClient
}

func NewDeploymentsHandler(pg *db.PostgresDB, nats *db.NatsClient) *DeploymentsHandler {
	return &DeploymentsHandler{
		Postgres: pg,
		Nats:     nats,
	}
}

func (h *DeploymentsHandler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `
		SELECT id, organization_id, project_id, environment_id, service_id, version,
		       git_sha, git_branch, commit_message, author, source, status, deployment_strategy,
		       image_tag, rollback_target_deployment_id, started_at, finished_at, error_message, created_at
		FROM deployments
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`

	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Failed to query deployments"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var deployments []models.Deployment
	for rows.Next() {
		var d models.Deployment
		err := rows.Scan(
			&d.ID, &d.OrganizationID, &d.ProjectID, &d.EnvironmentID, &d.ServiceID, &d.Version,
			&d.GitSHA, &d.GitBranch, &d.CommitMessage, &d.Author, &d.Source, &d.Status, &d.DeploymentStrategy,
			&d.ImageTag, &d.RollbackTargetDeploymentID, &d.StartedAt, &d.FinishedAt, &d.ErrorMessage, &d.CreatedAt,
		)
		if err == nil {
			deployments = append(deployments, d)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(deployments)
}

func (h *DeploymentsHandler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req models.Deployment
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	req.OrganizationID = orgID
	req.Status = "QUEUED"
	if req.Source == "" {
		req.Source = "manual"
	}

	query := `
		INSERT INTO deployments (organization_id, project_id, environment_id, service_id, version, git_sha, git_branch, commit_message, author, source, status, deployment_strategy, image_tag)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		req.OrganizationID, req.ProjectID, req.EnvironmentID, req.ServiceID, req.Version,
		req.GitSHA, req.GitBranch, req.CommitMessage, req.Author, req.Source, req.Status,
		req.DeploymentStrategy, req.ImageTag,
	).Scan(&req.ID, &req.CreatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create deployment"}`, http.StatusInternalServerError)
		return
	}

	// Create initial deployment steps
	steps := []string{"Pulling Container Image", "Running Pre-Deploy Checks", "Updating Service Task", "Post-Deploy Health Probe"}
	for i, name := range steps {
		_, _ = h.Postgres.Pool.Exec(
			r.Context(),
			`INSERT INTO deployment_steps (deployment_id, step_order, name, status) VALUES ($1, $2, $3, 'PENDING')`,
			req.ID, i+1, name,
		)
	}

	// Publish Event to NATS
	_ = h.Nats.PublishEvent(contracts.SubjectDeploymentCreated, contracts.Event{
		ID:             uuid.New().String(),
		Version:        "1.0",
		Type:           contracts.SubjectDeploymentCreated,
		OrganizationID: orgID,
		Payload: map[string]interface{}{
			"deployment_id": req.ID,
			"service_id":    req.ServiceID,
			"version":       req.Version,
			"status":        req.Status,
		},
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(req)
}

func (h *DeploymentsHandler) TriggerRollback(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	deploymentID := chi.URLParam(r, "id")

	// Get target deployment to roll back
	var target models.Deployment
	query := `SELECT id, project_id, environment_id, service_id, version, image_tag FROM deployments WHERE id = $1 AND organization_id = $2`
	err := h.Postgres.Pool.QueryRow(r.Context(), query, deploymentID, orgID).Scan(
		&target.ID, &target.ProjectID, &target.EnvironmentID, &target.ServiceID, &target.Version, &target.ImageTag,
	)
	if err != nil {
		http.Error(w, `{"error":"Target deployment not found"}`, http.StatusNotFound)
		return
	}

	// Create new rollback deployment
	var rollback models.Deployment
	rollback.OrganizationID = orgID
	rollback.ProjectID = target.ProjectID
	rollback.EnvironmentID = target.EnvironmentID
	rollback.ServiceID = target.ServiceID
	rollback.Version = target.Version + "-rollback"
	rollback.Source = "rollback"
	rollback.Status = "RUNNING"
	rollback.RollbackTargetDeploymentID = &target.ID
	rollback.CommitMessage = "Emergency rollback to " + target.Version

	insQuery := `
		INSERT INTO deployments (organization_id, project_id, environment_id, service_id, version, source, status, rollback_target_deployment_id, commit_message, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		RETURNING id, created_at, started_at
	`

	err = h.Postgres.Pool.QueryRow(
		r.Context(), insQuery,
		rollback.OrganizationID, rollback.ProjectID, rollback.EnvironmentID, rollback.ServiceID,
		rollback.Version, rollback.Source, rollback.Status, rollback.RollbackTargetDeploymentID, rollback.CommitMessage,
	).Scan(&rollback.ID, &rollback.CreatedAt, &rollback.StartedAt)

	if err != nil {
		http.Error(w, `{"error":"Rollback failed"}`, http.StatusInternalServerError)
		return
	}

	// Mark original as ROLLED_BACK
	_, _ = h.Postgres.Pool.Exec(r.Context(), `UPDATE deployments SET status = 'ROLLED_BACK' WHERE id = $1`, target.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rollback)
}

func (h *DeploymentsHandler) GetDeploymentByID(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	deploymentID := chi.URLParam(r, "id")

	var d models.Deployment
	query := `
		SELECT id, organization_id, project_id, environment_id, service_id, version,
		       git_sha, git_branch, commit_message, author, source, status, deployment_strategy,
		       image_tag, rollback_target_deployment_id, started_at, finished_at, error_message, created_at
		FROM deployments
		WHERE id = $1 AND organization_id = $2
	`

	err := h.Postgres.Pool.QueryRow(r.Context(), query, deploymentID, orgID).Scan(
		&d.ID, &d.OrganizationID, &d.ProjectID, &d.EnvironmentID, &d.ServiceID, &d.Version,
		&d.GitSHA, &d.GitBranch, &d.CommitMessage, &d.Author, &d.Source, &d.Status, &d.DeploymentStrategy,
		&d.ImageTag, &d.RollbackTargetDeploymentID, &d.StartedAt, &d.FinishedAt, &d.ErrorMessage, &d.CreatedAt,
	)

	if err != nil {
		http.Error(w, `{"error":"Deployment not found"}`, http.StatusNotFound)
		return
	}

	// Fetch steps
	stepRows, err := h.Postgres.Pool.Query(r.Context(), `SELECT id, deployment_id, step_order, name, status, log_output, started_at, finished_at FROM deployment_steps WHERE deployment_id = $1 ORDER BY step_order ASC`, deploymentID)
	var steps []models.DeploymentStep
	if err == nil {
		defer stepRows.Close()
		for stepRows.Next() {
			var st models.DeploymentStep
			if err := stepRows.Scan(&st.ID, &st.DeploymentID, &st.StepOrder, &st.Name, &st.Status, &st.LogOutput, &st.StartedAt, &st.FinishedAt); err == nil {
				steps = append(steps, st)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deployment": d,
		"steps":      steps,
	})
}
