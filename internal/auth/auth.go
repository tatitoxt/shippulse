// Package auth issues and verifies session tokens (HS256 JWT) and hashes passwords.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID string `json:"uid"`
	OrgID  string `json:"org"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

type Issuer struct {
	Secret []byte
	TTL    time.Duration
	Now    func() time.Time
}

func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET must be at least 32 characters")
	}
	return &Issuer{Secret: []byte(secret), TTL: ttl, Now: time.Now}, nil
}

func (i *Issuer) Issue(userID, orgID, email string) (string, error) {
	now := i.Now()
	claims := Claims{UserID: userID, OrgID: orgID, Email: email, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "shippulse", Subject: userID,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(i.TTL)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.Secret)
}

func (i *Issuer) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return i.Secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("shippulse"), jwt.WithTimeFunc(i.Now))
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	return claims, nil
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
