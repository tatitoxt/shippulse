package handlers_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func verifyHMAC(body []byte, signatureHeader, secret string) bool {
	if len(signatureHeader) < 7 || signatureHeader[:7] != "sha256=" {
		return false
	}
	expectedHash := signatureHeader[7:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	calculatedHash := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(calculatedHash), []byte(expectedHash))
}

func TestGitHubHMACVerification(t *testing.T) {
	secret := "my_webhook_secret_key"
	payload := []byte(`{"action":"completed","deployment":{"id":123,"sha":"a7f92bc"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyHMAC(payload, validSig, secret) {
		t.Errorf("Expected valid HMAC signature to pass verification")
	}

	invalidSig := "sha256=0000000000000000000000000000000000000000000000000000000000000000"
	if verifyHMAC(payload, invalidSig, secret) {
		t.Errorf("Expected invalid HMAC signature to fail verification")
	}
}
