package agent_keyring

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

const (
	// SecretLength matches the agent's crypto.SecretLength — 32 bytes
	// / 256 bits, the AES-256 entropy floor.
	SecretLength = 32

	// KIDByteLength is the random bytes used to derive a kid. 3 bytes
	// hex-encoded → 6-char kid, same as the agent's NewKID.
	KIDByteLength = 3
)

// newKID returns a fresh 6-hex-char keyring entry id.
func newKID() (string, error) {
	b := make([]byte, KIDByteLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newSecret returns 32 cryptographically-random bytes.
func newSecret() ([]byte, error) {
	b := make([]byte, SecretLength)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// base64URLNoPad mirrors the agent's wire format — base64url, no
// padding, applied to the raw secret bytes.
func base64URLNoPad(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
