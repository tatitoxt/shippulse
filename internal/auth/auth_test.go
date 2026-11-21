package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const secret = "test-secret-test-secret-test-secret!"

func TestIssueAndVerify(t *testing.T) {
	iss, err := NewIssuer(secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := iss.Issue("u1", "o1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	c, err := iss.Verify(tok)
	if err != nil || c.UserID != "u1" || c.OrgID != "o1" {
		t.Fatalf("verify: %+v %v", c, err)
	}
}

func TestVerifyRejectsTamperedExpiredAndForeignTokens(t *testing.T) {
	iss, _ := NewIssuer(secret, time.Hour)
	tok, _ := iss.Issue("u1", "o1", "a@example.com")
	if _, err := iss.Verify(tok[:len(tok)-2] + "xx"); err == nil {
		t.Error("tampered signature accepted")
	}
	other, _ := NewIssuer(strings.Repeat("z", 32), time.Hour)
	if _, err := other.Verify(tok); err == nil {
		t.Error("token signed with another secret accepted")
	}
	past, _ := NewIssuer(secret, time.Minute)
	past.Now = func() time.Time { return time.Now().Add(-time.Hour) }
	old, _ := past.Issue("u1", "o1", "a@example.com")
	if _, err := iss.Verify(old); err == nil {
		t.Error("expired token accepted")
	}
	none := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{UserID: "u1"})
	unsigned, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := iss.Verify(unsigned); err == nil {
		t.Error("alg=none accepted")
	}
}

func TestShortSecretRejected(t *testing.T) {
	if _, err := NewIssuer("short", time.Hour); err == nil {
		t.Fatal("short secret accepted")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") || CheckPassword(h, "wrong") {
		t.Fatal("password check mismatch")
	}
}
