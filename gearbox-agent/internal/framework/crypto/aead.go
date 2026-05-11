package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// magic is the 4-byte header written at the start of every encrypted secret file.
// Its presence tells the loader to decrypt rather than treat the file as plaintext.
var magic = [4]byte{'G', 'B', 'E', '1'}

// ErrKeyRequired is returned when an encrypted file is found but no encryption key
// has been configured. Key loss means rotation — there is no recovery path.
var ErrKeyRequired = errors.New(
	"secret file is encrypted (GBE1) but GEARBOX_AGENT_ENCRYPTION_KEY is not set; " +
		"set the key to decrypt, or rotate the secret (--rotate-api-key / --generate-webhook-secret)",
)

// KeyProvider supplies the 32-byte AES-256 key used to protect on-disk secrets.
// Implementations include [EnvKeyProvider] (reads from an environment variable) and,
// in future releases, cloud-KMS backends (see GitHub issue #54).
type KeyProvider interface {
	// Key returns the 32-byte AES-256 key, or nil if encryption is not configured.
	// A nil key means secrets are stored as plaintext; a non-nil key means they are
	// encrypted with AES-256-GCM.
	Key() ([]byte, error)
}

// EnvKeyProvider reads the encryption key from an environment variable.
// The variable must contain exactly 64 lowercase hex characters (32 bytes / 256 bits).
// Generate a suitable value with: openssl rand -hex 32
type EnvKeyProvider struct {
	EnvVar string // environment variable name; default "GEARBOX_AGENT_ENCRYPTION_KEY"
}

// DefaultKeyProvider is the KeyProvider used by loadOrCreateSecret and readSecret
// unless overridden in tests.
var DefaultKeyProvider KeyProvider = &EnvKeyProvider{EnvVar: "GEARBOX_AGENT_ENCRYPTION_KEY"}

// Key implements [KeyProvider]. Returns nil, nil when the variable is unset.
func (e *EnvKeyProvider) Key() ([]byte, error) {
	val := os.Getenv(e.EnvVar)
	if val == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(val)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid hex value: %w", e.EnvVar, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf(
			"%s: must be exactly 64 hex characters (32 bytes); got %d bytes — generate with: openssl rand -hex 32",
			e.EnvVar, len(key),
		)
	}
	return key, nil
}

// EncryptionConfigured returns true when the default key provider has a key set.
// Used at startup to emit a warning when encryption is not configured.
func EncryptionConfigured() (bool, error) {
	key, err := DefaultKeyProvider.Key()
	if err != nil {
		return false, err
	}
	return key != nil, nil
}

// isEncrypted returns true when data begins with the GBE1 magic header.
func isEncrypted(data []byte) bool {
	if len(data) < len(magic) {
		return false
	}
	return data[0] == magic[0] && data[1] == magic[1] &&
		data[2] == magic[2] && data[3] == magic[3]
}

// encryptSecret encrypts plaintext using AES-256-GCM and returns the
// magic-prefixed blob: magic(4) + nonce(12) + GCM-ciphertext-with-tag.
func encryptSecret(plaintext string, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	out := make([]byte, 0, len(magic)+len(nonce)+len(sealed))
	out = append(out, magic[:]...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// decryptSecret decrypts a blob produced by [encryptSecret].
// data must start with the GBE1 magic header.
func decryptSecret(data []byte, key []byte) (string, error) {
	if !isEncrypted(data) {
		return "", errors.New("decryptSecret: data does not start with GBE1 magic")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}
	payload := data[len(magic):]
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", errors.New("decryptSecret: ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, payload[:nonceSize], payload[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("gcm.Open: %w (key may be wrong or file corrupted)", err)
	}
	return string(plaintext), nil
}
