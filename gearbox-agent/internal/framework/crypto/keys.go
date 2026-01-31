// Package crypto provides cryptographic utilities for the gearbox-agent.
package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// SecretLength is the length of generated secrets in bytes (32 bytes = 64 hex chars).
	SecretLength = 32
	// APIKeyFilePerms is the file permission for secret files (owner read/write only).
	APIKeyFilePerms = 0600
)

// For backwards compatibility.
const APIKeyLength = SecretLength

// generateSecret generates a cryptographically secure random hex string.
func generateSecret() (string, error) {
	bytes := make([]byte, SecretLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// loadOrCreateSecret is a generic helper for loading or creating secrets.
func loadOrCreateSecret(secretPath, secretType string) (string, bool, error) {
	// Try to read existing secret
	data, err := os.ReadFile(secretPath)
	if err == nil {
		secret := string(data)
		if len(secret) >= SecretLength*2 { // hex encoded is 2x bytes
			return secret, false, nil
		}
		// Secret file exists but is invalid, regenerate
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("failed to read %s file: %w", secretType, err)
	}

	// Generate new secret
	secret, err := generateSecret()
	if err != nil {
		return "", false, err
	}

	// Ensure directory exists
	dir := filepath.Dir(secretPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", false, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write secret to file
	if err := os.WriteFile(secretPath, []byte(secret), APIKeyFilePerms); err != nil {
		return "", false, fmt.Errorf("failed to write %s file: %w", secretType, err)
	}

	return secret, true, nil
}

// readSecret is a generic helper for reading secrets from files.
func readSecret(secretPath, secretType string) (string, error) {
	data, err := os.ReadFile(secretPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", secretType, err)
	}
	return string(data), nil
}

// GenerateAPIKey generates a cryptographically secure random API key.
func GenerateAPIKey() (string, error) {
	return generateSecret()
}

// LoadOrCreateAPIKey loads an existing API key from the file or creates a new one.
// Returns the API key and a boolean indicating if a new key was created.
func LoadOrCreateAPIKey(keyPath string) (string, bool, error) {
	return loadOrCreateSecret(keyPath, "API key")
}

// ReadAPIKey reads an API key from the specified file path.
func ReadAPIKey(keyPath string) (string, error) {
	return readSecret(keyPath, "API key")
}

// LoadOrCreateWebhookSecret loads an existing webhook secret from the file or creates a new one.
// Returns the secret and a boolean indicating if a new secret was created.
func LoadOrCreateWebhookSecret(secretPath string) (string, bool, error) {
	return loadOrCreateSecret(secretPath, "webhook secret")
}

// ReadWebhookSecret reads a webhook secret from the specified file path.
func ReadWebhookSecret(secretPath string) (string, error) {
	return readSecret(secretPath, "webhook secret")
}
