package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// TLSCertFilePerms is the file permission for TLS certificate files.
	TLSCertFilePerms = 0644
	// TLSKeyFilePerms is the file permission for TLS key files.
	TLSKeyFilePerms = 0600
)

// TLSConfig holds paths to TLS certificate and key files.
type TLSConfig struct {
	CertPath string
	KeyPath  string
}

// LoadTLSCert loads existing TLS certificates without attempting to generate new ones.
// Use this for user-provided certificates (e.g., Let's Encrypt).
// Returns an error if the certificates don't exist or are invalid.
func LoadTLSCert(certPath, keyPath string) (*TLSConfig, error) {
	if !fileExists(certPath) {
		return nil, fmt.Errorf("certificate file not found: %s", certPath)
	}
	if !fileExists(keyPath) {
		return nil, fmt.Errorf("key file not found: %s", keyPath)
	}

	// Verify the cert is readable (but don't fail on expiry warnings for external certs)
	if err := verifyCertReadable(certPath); err != nil {
		return nil, fmt.Errorf("invalid certificate: %w", err)
	}

	return &TLSConfig{CertPath: certPath, KeyPath: keyPath}, nil
}

// LoadOrCreateTLSCert loads existing TLS certificates or generates self-signed ones.
// Returns paths to cert and key files, and a boolean indicating if new certs were created.
//
// A cert is considered still valid (and reused) when it exists, parses, is not
// near expiry, AND covers every host in `hosts` as a SAN. If a previously
// generated cert is missing a host (e.g. the operator added a new entry to
// HAPROXY_AGENT_TLS_HOSTS) it is regenerated — otherwise the new env value
// would silently never take effect.
func LoadOrCreateTLSCert(certPath, keyPath string, hosts []string) (*TLSConfig, bool, error) {
	// Check if both files exist and are valid
	if fileExists(certPath) && fileExists(keyPath) {
		if err := verifyCert(certPath); err == nil {
			if err := verifyCertCoversHosts(certPath, hosts); err == nil {
				return &TLSConfig{CertPath: certPath, KeyPath: keyPath}, false, nil
			}
			// SAN coverage gap — fall through to regenerate.
		}
		// Cert is expired, invalid, or missing required SANs — regenerate.
	}

	// Generate new self-signed certificate
	if err := generateSelfSignedCert(certPath, keyPath, hosts); err != nil {
		return nil, false, err
	}

	return &TLSConfig{CertPath: certPath, KeyPath: keyPath}, true, nil
}

// fileExists checks if a file exists and is accessible.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// verifyCertReadable checks that the cert file can be read and parsed.
// It does NOT check expiry - use this for external certs managed by others (e.g., Let's Encrypt).
func verifyCertReadable(certPath string) error {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block")
	}

	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	return nil
}

// verifyCert checks that the cert file is valid and not expiring soon.
// Used for self-signed certs that we manage and can regenerate.
func verifyCert(certPath string) error {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Check if cert expires within 30 days
	if time.Now().Add(30 * 24 * time.Hour).After(cert.NotAfter) {
		return fmt.Errorf("certificate expires soon or is expired")
	}

	return nil
}

// verifyCertCoversHosts loads the cert at certPath and confirms every entry
// in `hosts` is present as a SAN (DNS name for hostnames, IPAddresses entry
// for IPs). Returns nil if all hosts are covered.
//
// DNS comparison is case-insensitive per RFC 6125 §6.4 (TLS host matching),
// so changing only the casing of an entry in HAPROXY_AGENT_TLS_HOSTS will
// not force a spurious regeneration. A single trailing dot on FQDNs is also
// stripped on both sides ("example.com." and "example.com" are equivalent
// per DNS).
func verifyCertCoversHosts(certPath string, hosts []string) error {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	dnsSet := make(map[string]struct{}, len(cert.DNSNames))
	for _, d := range cert.DNSNames {
		dnsSet[normaliseDNSName(d)] = struct{}{}
	}
	ipSet := make(map[string]struct{}, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ipSet[ip.String()] = struct{}{}
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			if _, ok := ipSet[ip.String()]; !ok {
				return fmt.Errorf("certificate missing IP SAN %s", ip.String())
			}
			continue
		}
		if _, ok := dnsSet[normaliseDNSName(h)]; !ok {
			return fmt.Errorf("certificate missing DNS SAN %s", h)
		}
	}
	return nil
}

// normaliseDNSName lowercases and strips a single trailing dot so that
// "Example.COM.", "example.com.", and "example.com" all compare equal.
// DNS names are case-insensitive and the trailing dot is the
// FQDN/relative distinction, not a real character.
func normaliseDNSName(s string) string {
	s = strings.ToLower(s)
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// generateSelfSignedCert creates a new self-signed TLS certificate and key.
// The certificate is valid for 1 year and includes the specified hosts as SANs.
func generateSelfSignedCert(certPath, keyPath string, hosts []string) error {
	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate serial number
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Gearbox Agent"},
			CommonName:   "gearbox-agent",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add hosts to certificate
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	// Always add localhost
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"))
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("::1"))
	template.DNSNames = append(template.DNSNames, "localhost")

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Dir(certPath), 0750); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0750); err != nil {
		return fmt.Errorf("failed to create key directory: %w", err)
	}

	// Write certificate
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, TLSCertFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, TLSKeyFilePerms)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}
