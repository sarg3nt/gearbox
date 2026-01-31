package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// GenerateUUID generates a new UUID v4 for user IDs.
// UUIDs prevent user enumeration attacks and ID collision.
func GenerateUUID() string {
	return uuid.New().String()
}

// GenerateSessionToken generates a cryptographically secure 128-bit session token.
// This token is stored in the database and validated on each request.
// OWASP 2026 recommendation: minimum 128 bits for session tokens.
func GenerateSessionToken() (string, error) {
	// 16 bytes = 128 bits
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateCSRFToken generates a cryptographically secure CSRF token.
func GenerateCSRFToken() (string, error) {
	// 32 bytes = 256 bits for CSRF tokens
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
