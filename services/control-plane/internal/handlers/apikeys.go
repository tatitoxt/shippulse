package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type APIKeysHandler struct {
	Postgres *db.PostgresDB
}

func NewAPIKeysHandler(pg *db.PostgresDB) *APIKeysHandler {
	return &APIKeysHandler{Postgres: pg}
}

type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type CreateAPIKeyResponse struct {
	APIKey models.APIKey `json:"api_key"`
	Secret string        `json:"secret_token"` // Displayed only ONCE upon creation
}

func (h *APIKeysHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
		return
	}

	rawBytes := make([]byte, 24)
	_, _ = rand.Read(rawBytes)
	prefix := "sp_live_"
	secret := prefix + hex.EncodeToString(rawBytes)

	hash := sha256.Sum256([]byte(secret))
	keyHash := hex.EncodeToString(hash[:])

	if len(req.Scopes) == 0 {
		req.Scopes = []string{"deployments:read", "deployments:write", "services:read"}
	}

	var apiKey models.APIKey
	query := `
		INSERT INTO api_keys (organization_id, name, key_prefix, key_hash, scopes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, organization_id, name, key_prefix, scopes, created_at
	`

	err := h.Postgres.Pool.QueryRow(
		r.Context(), query,
		orgID, req.Name, prefix, keyHash, req.Scopes,
	).Scan(&apiKey.ID, &apiKey.OrganizationID, &apiKey.Name, &apiKey.KeyPrefix, &apiKey.Scopes, &apiKey.CreatedAt)

	if err != nil {
		http.Error(w, `{"error":"Failed to create API key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateAPIKeyResponse{
		APIKey: apiKey,
		Secret: secret,
	})
}

func (h *APIKeysHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)

	query := `SELECT id, organization_id, name, key_prefix, scopes, last_used_at, expires_at, created_at FROM api_keys WHERE organization_id = $1 ORDER BY created_at DESC`
	rows, err := h.Postgres.Pool.Query(r.Context(), query, orgID)
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.OrganizationID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt); err == nil {
			keys = append(keys, k)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func (h *APIKeysHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID := r.Context().Value(middleware.OrganizationContextKey).(string)
	keyID := chi.URLParam(r, "id")

	_, err := h.Postgres.Pool.Exec(r.Context(), `DELETE FROM api_keys WHERE id = $1 AND organization_id = $2`, keyID, orgID)
	if err != nil {
		http.Error(w, `{"error":"Failed to revoke API key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}
