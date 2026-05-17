package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// staticKeyProvider always returns the same key (or nil).
type staticKeyProvider struct{ key []byte }

func (s *staticKeyProvider) Key() ([]byte, error) { return s.key, nil }

// validTestKey returns a fresh 32-byte key for tests.
func validTestKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

// withProvider temporarily replaces DefaultKeyProvider and restores it on cleanup.
func withProvider(t *testing.T, p KeyProvider) {
	t.Helper()
	original := DefaultKeyProvider
	DefaultKeyProvider = p
	t.Cleanup(func() { DefaultKeyProvider = original })
}

// ── isEncrypted ──────────────────────────────────────────────────────────────

func TestIsEncrypted_MagicHeader(t *testing.T) {
	data := append(magic[:], []byte("payload")...)
	if !isEncrypted(data) {
		t.Error("isEncrypted should return true for GBE1-prefixed data")
	}
}

func TestIsEncrypted_PlaintextData(t *testing.T) {
	data := []byte("plaintext secret value here")
	if isEncrypted(data) {
		t.Error("isEncrypted should return false for plaintext data")
	}
}

func TestIsEncrypted_TooShort(t *testing.T) {
	if isEncrypted([]byte("GBE")) {
		t.Error("isEncrypted should return false for data shorter than magic")
	}
	if isEncrypted(nil) {
		t.Error("isEncrypted should return false for nil data")
	}
}

// ── encrypt / decrypt round-trip ─────────────────────────────────────────────

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := validTestKey()
	plaintext := "super-secret-api-key-value-12345678901234567890"

	ct, err := encryptSecret(plaintext, key)
	if err != nil {
		t.Fatalf("encryptSecret error: %v", err)
	}
	if !isEncrypted(ct) {
		t.Error("encryptSecret output does not start with GBE1")
	}

	got, err := decryptSecret(ct, key)
	if err != nil {
		t.Fatalf("decryptSecret error: %v", err)
	}
	if got != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestEncryptSecret_ProducesUniqueNonces(t *testing.T) {
	key := validTestKey()
	plaintext := "same-plaintext"

	ct1, _ := encryptSecret(plaintext, key)
	ct2, _ := encryptSecret(plaintext, key)
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptSecret calls with the same input must not produce identical ciphertext")
	}
}

func TestDecryptSecret_WrongKey(t *testing.T) {
	key := validTestKey()
	ct, _ := encryptSecret("secret", key)

	wrongKey := make([]byte, 32)
	_, err := decryptSecret(ct, wrongKey)
	if err == nil {
		t.Error("decryptSecret with wrong key should return error")
	}
}

func TestDecryptSecret_CorruptedCiphertext(t *testing.T) {
	key := validTestKey()
	ct, _ := encryptSecret("secret", key)

	// Flip a byte in the ciphertext region (after magic + nonce).
	ct[len(magic)+12]++
	_, err := decryptSecret(ct, key)
	if err == nil {
		t.Error("decryptSecret with corrupted ciphertext should return error")
	}
}

func TestDecryptSecret_MissingMagic(t *testing.T) {
	_, err := decryptSecret([]byte("not-encrypted"), validTestKey())
	if err == nil {
		t.Error("decryptSecret without magic header should return error")
	}
}

func TestDecryptSecret_TooShort(t *testing.T) {
	// GBE1 magic + only 4 bytes (nonce needs 12).
	data := append(magic[:], 0, 0, 0, 0)
	_, err := decryptSecret(data, validTestKey())
	if err == nil {
		t.Error("decryptSecret with truncated payload should return error")
	}
}

// ── EnvKeyProvider ───────────────────────────────────────────────────────────

func TestEnvKeyProvider_ValidKey(t *testing.T) {
	key := validTestKey()
	hexKey := make([]byte, 64)
	for i, b := range key {
		hexKey[i*2] = "0123456789abcdef"[b>>4]
		hexKey[i*2+1] = "0123456789abcdef"[b&0xf]
	}
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", string(hexKey))

	p := &EnvKeyProvider{EnvVar: "GEARBOX_AGENT_ENCRYPTION_KEY"}
	got, err := p.Key()
	if err != nil {
		t.Fatalf("Key() error: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Error("Key() returned wrong bytes")
	}
}

func TestEnvKeyProvider_Unset(t *testing.T) {
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", "")

	p := &EnvKeyProvider{EnvVar: "GEARBOX_AGENT_ENCRYPTION_KEY"}
	got, err := p.Key()
	if err != nil {
		t.Fatalf("Key() error: %v", err)
	}
	if got != nil {
		t.Error("Key() should return nil when env var is unset")
	}
}

func TestEnvKeyProvider_InvalidHex(t *testing.T) {
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", "not-valid-hex!!")

	p := &EnvKeyProvider{EnvVar: "GEARBOX_AGENT_ENCRYPTION_KEY"}
	_, err := p.Key()
	if err == nil {
		t.Error("Key() should return error for invalid hex")
	}
}

func TestEnvKeyProvider_WrongLength(t *testing.T) {
	// 16 bytes (128-bit) — too short for AES-256.
	t.Setenv("GEARBOX_AGENT_ENCRYPTION_KEY", "0102030405060708090a0b0c0d0e0f10")

	p := &EnvKeyProvider{EnvVar: "GEARBOX_AGENT_ENCRYPTION_KEY"}
	_, err := p.Key()
	if err == nil {
		t.Error("Key() should return error for non-32-byte key")
	}
}

// ── loadOrCreateSecret ───────────────────────────────────────────────────────

func TestLoadOrCreateSecret_NewEncrypted(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: validTestKey()})
	path := filepath.Join(t.TempDir(), "secret")

	secret, isNew, err := loadOrCreateSecret(path, "test")
	if err != nil {
		t.Fatalf("loadOrCreateSecret error: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for first creation")
	}
	if len(secret) != SecretLength*2 {
		t.Errorf("secret length %d, want %d", len(secret), SecretLength*2)
	}

	// Verify the on-disk file is encrypted.
	data, _ := os.ReadFile(path)
	if !isEncrypted(data) {
		t.Error("file on disk should be encrypted (GBE1 prefix) when key is set")
	}
}

func TestLoadOrCreateSecret_NewPlaintext(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: nil})
	path := filepath.Join(t.TempDir(), "secret")

	_, isNew, err := loadOrCreateSecret(path, "test")
	if err != nil {
		t.Fatalf("loadOrCreateSecret error: %v", err)
	}
	if !isNew {
		t.Error("expected isNew=true for first creation")
	}

	// File should be plaintext when no key.
	data, _ := os.ReadFile(path)
	if isEncrypted(data) {
		t.Error("file should be plaintext when no key is configured")
	}
}

func TestLoadOrCreateSecret_ReloadEncrypted(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: validTestKey()})
	path := filepath.Join(t.TempDir(), "secret")

	original, _, _ := loadOrCreateSecret(path, "test")
	reloaded, isNew, err := loadOrCreateSecret(path, "test")
	if err != nil {
		t.Fatalf("second load error: %v", err)
	}
	if isNew {
		t.Error("expected isNew=false on reload")
	}
	if reloaded != original {
		t.Errorf("reloaded secret %q != original %q", reloaded, original)
	}
}

func TestLoadOrCreateSecret_AutoMigratesPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")

	// Write a valid plaintext secret.
	plainSecret := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" //gitleaks:allow — deterministic test fixture
	if err := os.WriteFile(path, []byte(plainSecret), 0600); err != nil {
		t.Fatalf("write plaintext: %v", err)
	}

	// Now load with encryption enabled — should auto-migrate.
	withProvider(t, &staticKeyProvider{key: validTestKey()})
	got, isNew, err := loadOrCreateSecret(path, "test")
	if err != nil {
		t.Fatalf("loadOrCreateSecret error: %v", err)
	}
	if isNew {
		t.Error("auto-migration should not set isNew=true")
	}
	if got != plainSecret {
		t.Errorf("got %q, want %q", got, plainSecret)
	}

	// File must now be encrypted on disk.
	data, _ := os.ReadFile(path)
	if !isEncrypted(data) {
		t.Error("file should be encrypted after auto-migration")
	}
}

func TestLoadOrCreateSecret_EncryptedNoKey(t *testing.T) {
	// Create an encrypted file with a key...
	path := filepath.Join(t.TempDir(), "secret")
	withProvider(t, &staticKeyProvider{key: validTestKey()})
	loadOrCreateSecret(path, "test") //nolint:errcheck

	// ... then try to load it without a key.
	withProvider(t, &staticKeyProvider{key: nil})
	_, _, err := loadOrCreateSecret(path, "test")
	if err == nil {
		t.Fatal("expected ErrKeyRequired for encrypted file without key")
	}
	if err != ErrKeyRequired {
		t.Errorf("expected ErrKeyRequired, got: %v", err)
	}
}

// ── readSecret ───────────────────────────────────────────────────────────────

func TestReadSecret_Plaintext(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: nil})
	path := filepath.Join(t.TempDir(), "secret")
	expected := "some-plaintext-value"
	os.WriteFile(path, []byte(expected), 0600)

	got, err := readSecret(path, "test")
	if err != nil {
		t.Fatalf("readSecret error: %v", err)
	}
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestReadSecret_Encrypted(t *testing.T) {
	key := validTestKey()
	plaintext := "the-real-secret-value-0123456789"

	path := filepath.Join(t.TempDir(), "secret")
	ct, _ := encryptSecret(plaintext, key)
	os.WriteFile(path, ct, 0600)

	withProvider(t, &staticKeyProvider{key: key})
	got, err := readSecret(path, "test")
	if err != nil {
		t.Fatalf("readSecret error: %v", err)
	}
	if got != plaintext {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestReadSecret_EncryptedNoKey(t *testing.T) {
	key := validTestKey()
	path := filepath.Join(t.TempDir(), "secret")
	ct, _ := encryptSecret("value", key)
	os.WriteFile(path, ct, 0600)

	withProvider(t, &staticKeyProvider{key: nil})
	_, err := readSecret(path, "test")
	if err == nil {
		t.Fatal("expected ErrKeyRequired")
	}
	if err != ErrKeyRequired {
		t.Errorf("expected ErrKeyRequired, got: %v", err)
	}
}

func TestReadSecret_Missing(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: nil})
	_, err := readSecret("/nonexistent/path/secret", "test")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ── WriteAPIKey ───────────────────────────────────────────────────────────────

func TestWriteAPIKey_EncryptsWhenKeySet(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: validTestKey()})
	path := filepath.Join(t.TempDir(), "api-key")
	apiKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" //gitleaks:allow — deterministic test fixture

	if err := WriteAPIKey(path, apiKey); err != nil {
		t.Fatalf("WriteAPIKey error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !isEncrypted(data) {
		t.Error("WriteAPIKey should write encrypted file when key is set")
	}

	// Verify we can read it back.
	got, err := ReadAPIKey(path)
	if err != nil {
		t.Fatalf("ReadAPIKey error: %v", err)
	}
	if got != apiKey {
		t.Errorf("ReadAPIKey returned %q, want %q", got, apiKey)
	}
}

func TestWriteAPIKey_PlaintextWhenNoKey(t *testing.T) {
	withProvider(t, &staticKeyProvider{key: nil})
	path := filepath.Join(t.TempDir(), "api-key")
	apiKey := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" //gitleaks:allow — deterministic test fixture

	if err := WriteAPIKey(path, apiKey); err != nil {
		t.Fatalf("WriteAPIKey error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if isEncrypted(data) {
		t.Error("WriteAPIKey should write plaintext when no key is set")
	}
}
