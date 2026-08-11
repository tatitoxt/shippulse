package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/config"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
)

type WebhooksHandler struct {
	Cfg      *config.Config
	Postgres *db.PostgresDB
	Redis    *db.RedisClient
	Nats     *db.NatsClient
}

func NewWebhooksHandler(cfg *config.Config, pg *db.PostgresDB, redis *db.RedisClient, nats *db.NatsClient) *WebhooksHandler {
	return &WebhooksHandler{
		Cfg:      cfg,
		Postgres: pg,
		Redis:    redis,
		Nats:     nats,
	}
}

type GitHubDeploymentEvent struct {
	Action     string `json:"action"`
	Deployment struct {
		ID          int64  `json:"id"`
		SHA         string `json:"sha"`
		Ref         string `json:"ref"`
		Environment string `json:"environment"`
		Description string `json:"description"`
	} `json:"deployment"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (h *WebhooksHandler) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	signature := r.Header.Get("X-Hub-Signature-256")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")

	if signature == "" || deliveryID == "" {
		http.Error(w, `{"error":"Missing GitHub webhook headers"}`, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"Failed to read body"}`, http.StatusBadRequest)
		return
	}

	// 1. Verify HMAC-SHA256 signature
	if !verifyHMACSignature(body, signature, h.Cfg.GithubSecret) {
		http.Error(w, `{"error":"Invalid webhook signature"}`, http.StatusUnauthorized)
		return
	}

	// 2. Idempotency Check using Redis
	dedupKey := "webhook:github:" + deliveryID
	set, err := h.Redis.Client.SetNX(r.Context(), dedupKey, "1", 24*time.Hour).Result()
	if err == nil && !set {
		// Already processed delivery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored_duplicate"})
		return
	}

	// 3. Process Deployment Event
	if eventType == "deployment" || eventType == "push" {
		var payload GitHubDeploymentEvent
		if err := json.Unmarshal(body, &payload); err == nil {
			// Find service matching repository_url
			var serviceID, orgID, projectID, envID string
			findQuery := `
				SELECT s.id, s.organization_id, s.project_id, e.id
				FROM services s
				JOIN environments e ON e.project_id = s.project_id
				WHERE LOWER(s.repository_url) LIKE '%' || LOWER($1) || '%'
				LIMIT 1
			`
			repoName := payload.Repository.FullName
			if repoName != "" {
				err := h.Postgres.Pool.QueryRow(r.Context(), findQuery, repoName).Scan(&serviceID, &orgID, &projectID, &envID)
				if err == nil {
					// Create deployment record
					version := payload.Deployment.SHA
					if len(version) > 8 {
						version = version[:8]
					}
					if version == "" {
						version = "v1.0.0-push"
					}

					insQuery := `
						INSERT INTO deployments (organization_id, project_id, environment_id, service_id, version, git_sha, git_branch, author, source, status, commit_message)
						VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'github_webhook', 'QUEUED', $9)
					`
					_, _ = h.Postgres.Pool.Exec(
						r.Context(), insQuery,
						orgID, projectID, envID, serviceID, version, payload.Deployment.SHA,
						payload.Deployment.Ref, payload.Sender.Login, payload.Deployment.Description,
					)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func verifyHMACSignature(body []byte, signatureHeader, secret string) bool {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}
	expectedHash := strings.TrimPrefix(signatureHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	calculatedHash := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(calculatedHash), []byte(expectedHash))
}
