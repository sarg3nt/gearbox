package crypto

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	key, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	// Key should be 64 hex characters (32 bytes * 2)
	if len(key) != SecretLength*2 {
		t.Errorf("GenerateAPIKey() length = %d, want %d", len(key), SecretLength*2)
	}

	// Key should be unique (generate another and compare)
	key2, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() second call error = %v", err)
	}

	if key == key2 {
		t.Error("GenerateAPIKey() should generate unique keys")
	}
}

func TestLoadOrCreateAPIKey_NewKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "api-key")

	key, isNew, err := LoadOrCreateAPIKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateAPIKey() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateAPIKey() isNew = false, want true for new key")
	}

	if len(key) != SecretLength*2 {
		t.Errorf("LoadOrCreateAPIKey() key length = %d, want %d", len(key), SecretLength*2)
	}

	// Verify file was created with correct permissions
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}

	if info.Mode().Perm() != APIKeyFilePerms {
		t.Errorf("Key file permissions = %o, want %o", info.Mode().Perm(), APIKeyFilePerms)
	}
}

func TestLoadOrCreateAPIKey_ExistingKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "api-key")

	// Create key first time
	originalKey, _, err := LoadOrCreateAPIKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateAPIKey() first call error = %v", err)
	}

	// Load key second time
	loadedKey, isNew, err := LoadOrCreateAPIKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateAPIKey() second call error = %v", err)
	}

	if isNew {
		t.Error("LoadOrCreateAPIKey() isNew = true, want false for existing key")
	}

	if loadedKey != originalKey {
		t.Error("LoadOrCreateAPIKey() should return same key when loading existing")
	}
}

func TestLoadOrCreateAPIKey_InvalidKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "api-key")

	// Write an invalid (too short) key
	if err := os.WriteFile(keyPath, []byte("tooshort"), APIKeyFilePerms); err != nil {
		t.Fatalf("Failed to write invalid key: %v", err)
	}

	// Should regenerate when key is invalid
	key, isNew, err := LoadOrCreateAPIKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateAPIKey() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateAPIKey() should regenerate invalid key")
	}

	if len(key) != SecretLength*2 {
		t.Errorf("LoadOrCreateAPIKey() key length = %d, want %d", len(key), SecretLength*2)
	}
}

func TestLoadOrCreateAPIKey_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "subdir", "deep", "api-key")

	key, isNew, err := LoadOrCreateAPIKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateAPIKey() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateAPIKey() isNew = false, want true")
	}

	if len(key) == 0 {
		t.Error("LoadOrCreateAPIKey() returned empty key")
	}

	// Verify file exists
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("Key file not created: %v", err)
	}
}

func TestReadAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "api-key")

	expectedKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(keyPath, []byte(expectedKey), APIKeyFilePerms); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	key, err := ReadAPIKey(keyPath)
	if err != nil {
		t.Fatalf("ReadAPIKey() error = %v", err)
	}

	if key != expectedKey {
		t.Errorf("ReadAPIKey() = %q, want %q", key, expectedKey)
	}
}

func TestReadAPIKey_NotFound(t *testing.T) {
	_, err := ReadAPIKey("/nonexistent/path/key")
	if err == nil {
		t.Error("ReadAPIKey() expected error for missing file")
	}
}

func TestLoadOrCreateWebhookSecret(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "webhook-secret")

	secret, isNew, err := LoadOrCreateWebhookSecret(secretPath)
	if err != nil {
		t.Fatalf("LoadOrCreateWebhookSecret() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateWebhookSecret() isNew = false, want true")
	}

	if len(secret) != SecretLength*2 {
		t.Errorf("LoadOrCreateWebhookSecret() length = %d, want %d", len(secret), SecretLength*2)
	}
}

func TestReadWebhookSecret(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "webhook-secret")

	expectedSecret := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := os.WriteFile(secretPath, []byte(expectedSecret), APIKeyFilePerms); err != nil {
		t.Fatalf("Failed to write secret: %v", err)
	}

	secret, err := ReadWebhookSecret(secretPath)
	if err != nil {
		t.Fatalf("ReadWebhookSecret() error = %v", err)
	}

	if secret != expectedSecret {
		t.Errorf("ReadWebhookSecret() = %q, want %q", secret, expectedSecret)
	}
}

func TestLoadOrCreateTLSCert_NewCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	tlsCfg, isNew, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"localhost", "test.example.com"})
	if err != nil {
		t.Fatalf("LoadOrCreateTLSCert() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateTLSCert() isNew = false, want true")
	}

	if tlsCfg.CertPath != certPath {
		t.Errorf("CertPath = %q, want %q", tlsCfg.CertPath, certPath)
	}

	if tlsCfg.KeyPath != keyPath {
		t.Errorf("KeyPath = %q, want %q", tlsCfg.KeyPath, keyPath)
	}

	// Verify files were created
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("Cert file not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("Key file not created: %v", err)
	}

	// Verify cert file permissions
	certInfo, _ := os.Stat(certPath)
	if certInfo.Mode().Perm() != TLSCertFilePerms {
		t.Errorf("Cert file permissions = %o, want %o", certInfo.Mode().Perm(), TLSCertFilePerms)
	}

	// Verify key file permissions
	keyInfo, _ := os.Stat(keyPath)
	if keyInfo.Mode().Perm() != TLSKeyFilePerms {
		t.Errorf("Key file permissions = %o, want %o", keyInfo.Mode().Perm(), TLSKeyFilePerms)
	}
}

func TestLoadOrCreateTLSCert_ExistingCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// Create cert first time
	_, _, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"localhost"})
	if err != nil {
		t.Fatalf("LoadOrCreateTLSCert() first call error = %v", err)
	}

	// Get original file mod times
	certInfo1, _ := os.Stat(certPath)
	certModTime1 := certInfo1.ModTime()

	// Load cert second time
	tlsCfg, isNew, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"localhost"})
	if err != nil {
		t.Fatalf("LoadOrCreateTLSCert() second call error = %v", err)
	}

	if isNew {
		t.Error("LoadOrCreateTLSCert() isNew = true, want false for existing cert")
	}

	// Verify cert was not regenerated (mod time unchanged)
	certInfo2, _ := os.Stat(certPath)
	if !certInfo2.ModTime().Equal(certModTime1) {
		t.Error("LoadOrCreateTLSCert() should not regenerate valid cert")
	}

	if tlsCfg.CertPath != certPath {
		t.Errorf("CertPath = %q, want %q", tlsCfg.CertPath, certPath)
	}
}

func TestLoadOrCreateTLSCert_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "tls", "certs", "server.crt")
	keyPath := filepath.Join(tmpDir, "tls", "private", "server.key")

	tlsCfg, isNew, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"localhost"})
	if err != nil {
		t.Fatalf("LoadOrCreateTLSCert() error = %v", err)
	}

	if !isNew {
		t.Error("LoadOrCreateTLSCert() isNew = false, want true")
	}

	// Verify directories were created
	if _, err := os.Stat(filepath.Dir(certPath)); err != nil {
		t.Errorf("Cert directory not created: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(keyPath)); err != nil {
		t.Errorf("Key directory not created: %v", err)
	}

	if tlsCfg.CertPath != certPath {
		t.Errorf("CertPath = %q, want %q", tlsCfg.CertPath, certPath)
	}
}

func TestLoadTLSCert_Success(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// First create a cert using LoadOrCreateTLSCert
	_, _, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to create test cert: %v", err)
	}

	// Now test LoadTLSCert
	tlsCfg, err := LoadTLSCert(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadTLSCert() error = %v", err)
	}

	if tlsCfg.CertPath != certPath {
		t.Errorf("CertPath = %q, want %q", tlsCfg.CertPath, certPath)
	}
	if tlsCfg.KeyPath != keyPath {
		t.Errorf("KeyPath = %q, want %q", tlsCfg.KeyPath, keyPath)
	}
}

func TestLoadTLSCert_MissingCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "nonexistent.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// Create only the key file
	if err := os.WriteFile(keyPath, []byte("dummy"), 0600); err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}

	_, err := LoadTLSCert(certPath, keyPath)
	if err == nil {
		t.Error("LoadTLSCert() expected error for missing cert")
	}
}

func TestLoadTLSCert_MissingKey(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "nonexistent.key")

	// First create a valid cert
	_, _, err := LoadOrCreateTLSCert(certPath, filepath.Join(tmpDir, "temp.key"), []string{"localhost"})
	if err != nil {
		t.Fatalf("Failed to create test cert: %v", err)
	}

	// Now test with missing key
	_, err = LoadTLSCert(certPath, keyPath)
	if err == nil {
		t.Error("LoadTLSCert() expected error for missing key")
	}
}

func TestLoadTLSCert_InvalidCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// Write invalid cert content
	if err := os.WriteFile(certPath, []byte("not a valid certificate"), 0644); err != nil {
		t.Fatalf("Failed to write invalid cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("dummy key"), 0600); err != nil {
		t.Fatalf("Failed to write key: %v", err)
	}

	_, err := LoadTLSCert(certPath, keyPath)
	if err == nil {
		t.Error("LoadTLSCert() expected error for invalid cert")
	}
}

func TestGenerateSelfSignedCert_IncludesHosts(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	hosts := []string{"example.com", "192.168.1.1"}
	_, _, err := LoadOrCreateTLSCert(certPath, keyPath, hosts)
	if err != nil {
		t.Fatalf("LoadOrCreateTLSCert() error = %v", err)
	}

	cert := loadCertForTest(t, certPath)

	// Requested SANs must be present.
	if !containsString(cert.DNSNames, "example.com") {
		t.Errorf("cert DNSNames missing example.com: %v", cert.DNSNames)
	}
	if !containsIPString(cert.IPAddresses, "192.168.1.1") {
		t.Errorf("cert IPAddresses missing 192.168.1.1: %v", cert.IPAddresses)
	}

	// Loopback SANs are always added.
	if !containsString(cert.DNSNames, "localhost") {
		t.Errorf("cert DNSNames missing localhost: %v", cert.DNSNames)
	}
	if !containsIPString(cert.IPAddresses, "127.0.0.1") {
		t.Errorf("cert IPAddresses missing 127.0.0.1: %v", cert.IPAddresses)
	}
	if !containsIPString(cert.IPAddresses, "::1") {
		t.Errorf("cert IPAddresses missing ::1: %v", cert.IPAddresses)
	}
}

// TestLoadOrCreateTLSCert_RegenOnMissingSAN verifies that a previously
// generated cert is regenerated when LoadOrCreateTLSCert is called with a
// host that the existing cert does not cover. Without this behaviour an
// operator who adds a new entry to HAPROXY_AGENT_TLS_HOSTS would have to
// manually wipe the cert file before the change takes effect.
func TestLoadOrCreateTLSCert_RegenOnMissingSAN(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "server.crt")
	keyPath := filepath.Join(tmpDir, "server.key")

	// Initial cert covers example.com only.
	_, isNew, err := LoadOrCreateTLSCert(certPath, keyPath, []string{"example.com"})
	if err != nil {
		t.Fatalf("initial LoadOrCreateTLSCert() error = %v", err)
	}
	if !isNew {
		t.Fatal("initial call should report isNew = true")
	}
	originalSerial := loadCertForTest(t, certPath).SerialNumber.String()

	// Same hosts → reuse, no regen.
	_, isNew, err = LoadOrCreateTLSCert(certPath, keyPath, []string{"example.com"})
	if err != nil {
		t.Fatalf("second LoadOrCreateTLSCert() error = %v", err)
	}
	if isNew {
		t.Error("call with same hosts should report isNew = false")
	}
	if loadCertForTest(t, certPath).SerialNumber.String() != originalSerial {
		t.Error("cert serial changed despite same hosts (unexpected regeneration)")
	}

	// New host added → regenerate.
	_, isNew, err = LoadOrCreateTLSCert(certPath, keyPath, []string{"example.com", "172.16.2.3"})
	if err != nil {
		t.Fatalf("third LoadOrCreateTLSCert() error = %v", err)
	}
	if !isNew {
		t.Fatal("call with added host should report isNew = true")
	}
	regenerated := loadCertForTest(t, certPath)
	if regenerated.SerialNumber.String() == originalSerial {
		t.Error("cert serial unchanged after adding new host (regeneration did not happen)")
	}
	if !containsIPString(regenerated.IPAddresses, "172.16.2.3") {
		t.Errorf("regenerated cert missing 172.16.2.3 SAN: %v", regenerated.IPAddresses)
	}
	if !containsString(regenerated.DNSNames, "example.com") {
		t.Errorf("regenerated cert lost example.com SAN: %v", regenerated.DNSNames)
	}
	regeneratedSerial := regenerated.SerialNumber.String()

	// Casing change on the same DNS name → reuse, no spurious regen
	// (RFC 6125 §6.4: DNS host matching is case-insensitive).
	_, isNew, err = LoadOrCreateTLSCert(certPath, keyPath, []string{"EXAMPLE.com", "172.16.2.3"})
	if err != nil {
		t.Fatalf("fourth LoadOrCreateTLSCert() error = %v", err)
	}
	if isNew {
		t.Error("call differing only in DNS casing should not regenerate")
	}
	if loadCertForTest(t, certPath).SerialNumber.String() != regeneratedSerial {
		t.Error("cert serial changed across casing-only difference (unexpected regeneration)")
	}

	// Trailing-dot equivalence (example.com. == example.com per DNS).
	_, isNew, err = LoadOrCreateTLSCert(certPath, keyPath, []string{"example.com.", "172.16.2.3"})
	if err != nil {
		t.Fatalf("fifth LoadOrCreateTLSCert() error = %v", err)
	}
	if isNew {
		t.Error("trailing-dot variant should not regenerate")
	}
}

func loadCertForTest(t *testing.T, certPath string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func containsIPString(haystack []net.IP, needle string) bool {
	for _, ip := range haystack {
		if ip.String() == needle {
			return true
		}
	}
	return false
}
