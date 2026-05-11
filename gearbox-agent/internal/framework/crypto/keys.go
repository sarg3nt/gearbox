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

// writeSecret persists secret to path, encrypting it when the provider has a key.
func writeSecret(path, secret string, provider KeyProvider) error {
	key, err := provider.Key()
	if err != nil {
		return err
	}

	var data []byte
	if key != nil {
		data, err = encryptSecret(secret, key)
		if err != nil {
			return fmt.Errorf("encrypt secret: %w", err)
		}
	} else {
		data = []byte(secret)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return os.WriteFile(path, data, APIKeyFilePerms)
}

// loadOrCreateSecret loads a secret from path or generates and saves a new one.
//
// Encryption behaviour:
//   - Encrypted file, key present    → decrypt and return
//   - Encrypted file, no key         → fatal: return ErrKeyRequired
//   - Plaintext file, key present    → auto-migrate: re-encrypt in place and return
//   - Plaintext file, no key         → return as-is (warn at caller level)
//   - File missing                   → generate new secret, write (encrypted if key present)
func loadOrCreateSecret(secretPath, secretType string) (string, bool, error) {
	key, err := DefaultKeyProvider.Key()
	if err != nil {
		return "", false, fmt.Errorf("key provider error: %w", err)
	}

	data, err := os.ReadFile(secretPath)
	if err == nil {
		// File exists — handle all four cases above.
		if isEncrypted(data) {
			if key == nil {
				return "", false, ErrKeyRequired
			}
			secret, err := decryptSecret(data, key)
			if err != nil {
				return "", false, fmt.Errorf("failed to decrypt %s: %w", secretType, err)
			}
			if len(secret) < SecretLength*2 {
				// Decrypted payload is invalid; fall through to regenerate below.
			} else {
				return secret, false, nil
			}
		} else {
			// Plaintext file.
			secret := string(data)
			if len(secret) >= SecretLength*2 {
				if key != nil {
					// Auto-migrate: re-encrypt the existing plaintext secret.
					if err := writeSecret(secretPath, secret, DefaultKeyProvider); err != nil {
						return "", false, fmt.Errorf("failed to encrypt %s during migration: %w", secretType, err)
					}
				}
				return secret, false, nil
			}
			// Secret file exists but is too short; fall through to regenerate.
		}
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("failed to read %s file: %w", secretType, err)
	}

	// Generate a new secret and persist it.
	secret, err := generateSecret()
	if err != nil {
		return "", false, err
	}
	if err := writeSecret(secretPath, secret, DefaultKeyProvider); err != nil {
		return "", false, fmt.Errorf("failed to write %s file: %w", secretType, err)
	}
	return secret, true, nil
}

// readSecret reads a secret from path, decrypting it when necessary.
func readSecret(secretPath, secretType string) (string, error) {
	data, err := os.ReadFile(secretPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", secretType, err)
	}
	if isEncrypted(data) {
		key, err := DefaultKeyProvider.Key()
		if err != nil {
			return "", fmt.Errorf("key provider error: %w", err)
		}
		if key == nil {
			return "", ErrKeyRequired
		}
		return decryptSecret(data, key)
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

// ReadAPIKey reads an API key from the specified file path, decrypting if needed.
func ReadAPIKey(keyPath string) (string, error) {
	return readSecret(keyPath, "API key")
}

// WriteAPIKey writes an API key to the specified file path, encrypting if a key
// provider is configured. Used by the --rotate-api-key flag.
func WriteAPIKey(keyPath, key string) error {
	return writeSecret(keyPath, key, DefaultKeyProvider)
}

// LoadOrCreateWebhookSecret loads an existing webhook secret from the file or creates a new one.
// Returns the secret and a boolean indicating if a new secret was created.
func LoadOrCreateWebhookSecret(secretPath string) (string, bool, error) {
	return loadOrCreateSecret(secretPath, "webhook secret")
}

// ReadWebhookSecret reads a webhook secret from the specified file path, decrypting if needed.
func ReadWebhookSecret(secretPath string) (string, error) {
	return readSecret(secretPath, "webhook secret")
}
