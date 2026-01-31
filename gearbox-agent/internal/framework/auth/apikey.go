package auth

import (
	"crypto/subtle"
	"strings"
)

// ValidateAPIKey validates an API key using constant-time comparison.
// Returns true if the provided key matches the expected key.
func ValidateAPIKey(providedKey, expectedKey string) bool {
	return subtle.ConstantTimeCompare([]byte(providedKey), []byte(expectedKey)) == 1
}

// ExtractBearerToken extracts the bearer token from an Authorization header.
// Returns the token and true if successful, or empty string and false otherwise.
func ExtractBearerToken(authHeader string) (string, bool) {
	if authHeader == "" {
		return "", false
	}

	// Expect "Bearer <token>" format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", false
	}

	return parts[1], true
}
