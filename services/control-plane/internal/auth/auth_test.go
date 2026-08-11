package auth_test

import (
	"testing"

	"github.com/tatitoxt/shippulse/services/control-plane/internal/auth"
)

func TestPasswordHashing(t *testing.T) {
	password := "SecretPass123!"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !auth.CheckPassword(password, hash) {
		t.Errorf("Expected password check to succeed")
	}

	if auth.CheckPassword("WrongPass!", hash) {
		t.Errorf("Expected wrong password check to fail")
	}
}

func TestJWTGenerationAndValidation(t *testing.T) {
	secret := "test_secret_key"
	userID := "usr-123"
	email := "test@shippulse.dev"
	orgID := "org-456"
	role := "ENGINEER"

	token, err := auth.GenerateJWT(secret, userID, email, orgID, role)
	if err != nil {
		t.Fatalf("JWT generation failed: %v", err)
	}

	claims, err := auth.ValidateJWT(secret, token)
	if err != nil {
		t.Fatalf("JWT validation failed: %v", err)
	}

	if claims.UserID != userID || claims.Email != email || claims.OrganizationID != orgID || claims.Role != role {
		t.Errorf("Claims mismatch: got %+v", claims)
	}
}
