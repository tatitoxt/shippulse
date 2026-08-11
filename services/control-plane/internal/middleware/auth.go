package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/auth"
	"github.com/tatitoxt/shippulse/services/control-plane/internal/db"
)

type contextKey string

const (
	UserContextKey         contextKey = "user"
	OrganizationContextKey contextKey = "organization_id"
	RoleContextKey         contextKey = "role"
)

type AuthMiddleware struct {
	JWTSecret string
	Postgres  *db.PostgresDB
}

func NewAuthMiddleware(jwtSecret string, postgres *db.PostgresDB) *AuthMiddleware {
	return &AuthMiddleware{
		JWTSecret: jwtSecret,
		Postgres:  postgres,
	}
}

// Authenticate verifies JWT or API Key and attaches claims to context
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		apiKeyHeader := r.Header.Get("X-API-Key")

		// 1. API Key Authentication
		if apiKeyHeader != "" {
			orgID, scopes, err := m.validateAPIKey(r.Context(), apiKeyHeader)
			if err == nil && orgID != "" {
				ctx := context.WithValue(r.Context(), OrganizationContextKey, orgID)
				ctx = context.WithValue(ctx, RoleContextKey, "ENGINEER")
				_ = scopes
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 2. JWT Authentication
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := auth.ValidateJWT(m.JWTSecret, tokenStr)
			if err == nil && claims != nil {
				ctx := context.WithValue(r.Context(), UserContextKey, claims)
				ctx = context.WithValue(ctx, OrganizationContextKey, claims.OrganizationID)
				ctx = context.WithValue(ctx, RoleContextKey, claims.Role)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		http.Error(w, `{"error":"Unauthorized access"}`, http.StatusUnauthorized)
	})
}

// RequireRole checks if user has required role level
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole, ok := r.Context().Value(RoleContextKey).(string)
			if !ok || userRole == "" {
				http.Error(w, `{"error":"Forbidden: Missing role"}`, http.StatusForbidden)
				return
			}

			// Role hierarchy: OWNER > ADMIN > ENGINEER > VIEWER
			roleRank := map[string]int{
				"OWNER":    4,
				"ADMIN":    3,
				"ENGINEER": 2,
				"VIEWER":   1,
			}

			currentRank := roleRank[userRole]

			minRank := 5
			for _, role := range allowedRoles {
				if rRank := roleRank[role]; rRank < minRank {
					minRank = rRank
				}
			}

			if currentRank < minRank {
				http.Error(w, `{"error":"Forbidden: Insufficient permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (m *AuthMiddleware) validateAPIKey(ctx context.Context, keyStr string) (string, []string, error) {
	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])

	var orgID string
	var scopes []string

	query := `SELECT organization_id, scopes FROM api_keys WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())`
	err := m.Postgres.Pool.QueryRow(ctx, query, keyHash).Scan(&orgID, &scopes)
	if err != nil {
		return "", nil, err
	}

	// Update last_used_at async
	go func() {
		_, _ = m.Postgres.Pool.Exec(context.Background(), `UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1`, keyHash)
	}()

	return orgID, scopes, nil
}
