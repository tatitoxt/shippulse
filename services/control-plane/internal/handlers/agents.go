package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type AgentsHandler struct {
	Postgres *db.PostgresDB
	Redis    *db.RedisClient
}

func NewAgentsHandler(pg *db.PostgresDB, redis *db.RedisClient) *AgentsHandler {
	return &AgentsHandler{
		Postgres: pg,
		Redis:    redis,
	}
}

type CreateTokenResponse struct {
	RegistrationToken string    `json:"registration_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (h *AgentsHandler) CreateRegistrationToken(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	rawTokenBytes := make([]byte, 32)
	_, _ = rand.Read(rawTokenBytes)
	rawToken := hex.EncodeToString(rawTokenBytes)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	expiresAt := time.Now().Add(1 * time.Hour)

	query := `INSERT INTO agent_registration_tokens (organization_id, token_hash, expires_at) VALUES ($1, $2, $3)`
	_, err := h.Postgres.Pool.Exec(r.Context(), query, orgID, tokenHash, expiresAt)
	if err != nil {
		http.Error(w, `{"error":"Failed to create token"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateTokenResponse{
		RegistrationToken: rawToken,
		ExpiresAt:         expiresAt,
	})
}

type RegisterAgentRequest struct {
	RegistrationToken string   `json:"registration_token"`
	Name              string   `json:"name"`
	Hostname          string   `json:"hostname"`
	IPAddress         string   `json:"ip_address"`
	Version           string   `json:"version"`
	RuntimeType       string   `json:"runtime_type"` // docker, kubernetes
	Capabilities      []string `json:"capabilities"`
}

type RegisterAgentResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
	Status     string `json:"status"`
}

func (h *AgentsHandler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	hash := sha256.Sum256([]byte(req.RegistrationToken))
	tokenHash := hex.EncodeToString(hash[:])

	var orgID string
	var isUsed bool
	var expiresAt time.Time

	tokenQuery := `SELECT organization_id, is_used, expires_at FROM agent_registration_tokens WHERE token_hash = $1`
	err := h.Postgres.Pool.QueryRow(r.Context(), tokenQuery, tokenHash).Scan(&orgID, &isUsed, &expiresAt)
	if err != nil || isUsed || time.Now().After(expiresAt) {
		http.Error(w, `{"error":"Invalid or expired registration token"}`, http.StatusUnauthorized)
		return
	}

	// Mark token used
	_, _ = h.Postgres.Pool.Exec(r.Context(), `UPDATE agent_registration_tokens SET is_used = true WHERE token_hash = $1`, tokenHash)

	// Create long-lived agent identity
	var agent models.Agent
	agent.OrganizationID = orgID
	agent.Name = req.Name
	agent.Hostname = req.Hostname
	agent.IPAddress = req.IPAddress
	agent.Version = req.Version
	agent.RuntimeType = req.RuntimeType
	agent.Capabilities = req.Capabilities
	agent.Status = "ONLINE"

	if req.RuntimeType == "" {
		agent.RuntimeType = "docker"
	}

	agentQuery := `
		INSERT INTO agents (organization_id, name, hostname, ip_address, version, runtime_type, capabilities, status, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id
	`

	err = h.Postgres.Pool.QueryRow(
		r.Context(), agentQuery,
		agent.OrganizationID, agent.Name, agent.Hostname, agent.IPAddress, agent.Version,
		agent.RuntimeType, agent.Capabilities, agent.Status,
	).Scan(&agent.ID)

	if err != nil {
		http.Error(w, `{"error":"Agent creation failed"}`, http.StatusInternalServerError)
		return
	}

	// Generate agent auth token
	agentTokenBytes := make([]byte, 32)
	_, _ = rand.Read(agentTokenBytes)
	agentToken := "sp_agent_" + hex.EncodeToString(agentTokenBytes)

	// Store agent token in DB/Cache
	apiKeyHash := sha256.Sum256([]byte(agentToken))
	keyHashStr := hex.EncodeToString(apiKeyHash[:])

	_, _ = h.Postgres.Pool.Exec(
		r.Context(),
		`INSERT INTO api_keys (organization_id, name, key_prefix, key_hash, scopes) VALUES ($1, $2, 'sp_agent', $3, '{"agents:heartbeat", "deployments:read", "deployments:write"}')`,
		orgID, "Agent - "+req.Name, keyHashStr,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(RegisterAgentResponse{
		AgentID:    agent.ID,
		AgentToken: agentToken,
		Status:     "ONLINE",
	})
}

func (h *AgentsHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req struct {
		AgentID string   `json:"agent_id"`
		Status  string   `json:"status"`
		Metrics map[string]interface{} `json:"metrics,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	_, err := h.Postgres.Pool.Exec(
		r.Context(),
		`UPDATE agents SET status = COALESCE(NULLIF($1, ''), 'ONLINE'), last_heartbeat_at = NOW() WHERE id = $2 AND organization_id = $3`,
		req.Status, req.AgentID, orgID,
	)

	if err != nil {
		http.Error(w, `{"error":"Heartbeat failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
}

func (h *AgentsHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `SELECT id, organization_id, name, hostname, ip_address, version, runtime_type, capabilities, status, last_heartbeat_at, created_at FROM agents WHERE organization_id = $1`
	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var agents []models.Agent
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(&a.ID, &a.OrganizationID, &a.Name, &a.Hostname, &a.IPAddress, &a.Version, &a.RuntimeType, &a.Capabilities, &a.Status, &a.LastHeartbeatAt, &a.CreatedAt); err == nil {
			agents = append(agents, a)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}
