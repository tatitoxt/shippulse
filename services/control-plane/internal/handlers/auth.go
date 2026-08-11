package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/auth"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/config"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/middleware"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/models"
)

type AuthHandler struct {
	Cfg      *config.Config
	Postgres *db.PostgresDB
}

func NewAuthHandler(cfg *config.Config, pg *db.PostgresDB) *AuthHandler {
	return &AuthHandler{
		Cfg:      cfg,
		Postgres: pg,
	}
}

type RegisterRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	FullName     string `json:"full_name"`
	OrgName      string `json:"organization_name"`
}

type AuthResponse struct {
	Token        string               `json:"token"`
	User         models.User          `json:"user"`
	Organization models.Organization `json:"organization"`
	Role         string               `json:"role"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" || req.FullName == "" || req.OrgName == "" {
		http.Error(w, `{"error":"Missing required fields"}`, http.StatusBadRequest)
		return
	}

	pwdHash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, `{"error":"Internal error"}`, http.StatusInternalServerError)
		return
	}

	tx, err := h.Postgres.Pool.Begin(r.Context())
	if err != nil {
		http.Error(w, `{"error":"Database error"}`, http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// 1. Create User
	var user models.User
	userQuery := `INSERT INTO users (email, password_hash, full_name) VALUES ($1, $2, $3) RETURNING id, email, full_name, created_at, updated_at`
	err = tx.QueryRow(r.Context(), userQuery, req.Email, pwdHash, req.FullName).Scan(&user.ID, &user.Email, &user.FullName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		http.Error(w, `{"error":"User registration failed or email taken"}`, http.StatusConflict)
		return
	}

	// 2. Create Organization
	var org models.Organization
	orgSlug := slugify(req.OrgName)
	orgQuery := `INSERT INTO organizations (name, slug) VALUES ($1, $2) RETURNING id, name, slug, created_at, updated_at`
	err = tx.QueryRow(r.Context(), orgQuery, req.OrgName, orgSlug).Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		http.Error(w, `{"error":"Organization creation failed"}`, http.StatusInternalServerError)
		return
	}

	// 3. Create Owner Membership
	role := "OWNER"
	memQuery := `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, $3)`
	_, err = tx.Exec(r.Context(), memQuery, org.ID, user.ID, role)
	if err != nil {
		http.Error(w, `{"error":"Membership creation failed"}`, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, `{"error":"Transaction commit failed"}`, http.StatusInternalServerError)
		return
	}

	token, err := auth.GenerateJWT(h.Cfg.JWTSecret, user.ID, user.Email, org.ID, role)
	if err != nil {
		http.Error(w, `{"error":"JWT generation failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AuthResponse{
		Token:        token,
		User:         user,
		Organization: org,
		Role:         role,
	})
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	var user models.User
	var pwdHash string
	query := `SELECT id, email, password_hash, full_name, created_at, updated_at FROM users WHERE email = $1`
	err := h.Postgres.Pool.QueryRow(r.Context(), query, req.Email).Scan(&user.ID, &user.Email, &pwdHash, &user.FullName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil || !auth.CheckPassword(req.Password, pwdHash) {
		http.Error(w, `{"error":"Invalid email or password"}`, http.StatusUnauthorized)
		return
	}

	// Fetch primary organization and role
	var org models.Organization
	var role string
	orgQuery := `
		SELECT o.id, o.name, o.slug, o.created_at, o.updated_at, m.role
		FROM organizations o
		JOIN organization_memberships m ON m.organization_id = o.id
		WHERE m.user_id = $1
		LIMIT 1
	`
	err = h.Postgres.Pool.QueryRow(r.Context(), orgQuery, user.ID).Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt, &org.UpdatedAt, &role)
	if err != nil {
		http.Error(w, `{"error":"No organization found for user"}`, http.StatusForbidden)
		return
	}

	token, err := auth.GenerateJWT(h.Cfg.JWTSecret, user.ID, user.Email, org.ID, role)
	if err != nil {
		http.Error(w, `{"error":"Token generation failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Token:        token,
		User:         user,
		Organization: org,
		Role:         role,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(middleware.UserContextKey).(*auth.Claims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":         claims.UserID,
		"email":           claims.Email,
		"organization_id": claims.OrganizationID,
		"role":            claims.Role,
	})
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}
