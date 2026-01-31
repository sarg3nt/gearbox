// Package auth provides authentication utilities for the gearbox-agent framework.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifyHMACSHA256 verifies an HMAC-SHA256 signature.
// Returns true if the signature matches the expected value.
func VerifyHMACSHA256(secret string, payload []byte, signature string) bool {
	// Remove "sha256=" prefix if present
	signature = strings.TrimPrefix(signature, "sha256=")

	// Compute expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)
	expectedSig := hex.EncodeToString(expectedMAC)

	// Compare using constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// GenerateHMACSHA256 generates an HMAC-SHA256 signature for the given payload.
func GenerateHMACSHA256(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
