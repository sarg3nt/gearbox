// Package certs provides certificate monitoring and management functionality.
package certs

import (
	"strings"
	"time"
)

// Certificate represents a TLS certificate with its metadata.
type Certificate struct {
	Domain          string    `json:"domain"`
	CommonName      string    `json:"common_name"`
	Issuer          string    `json:"issuer"`
	NotBefore       time.Time `json:"not_before"`
	NotAfter        time.Time `json:"not_after"`
	DaysUntilExpiry int       `json:"days_until_expiry"`
	IsExpired       bool      `json:"is_expired"`
	Source          string    `json:"source"`        // "certbot", "acme.sh", "self-signed", "manual"
	FilePath        string    `json:"file_path"`     // Path to certificate file
	SANs            []string  `json:"sans"`          // Subject Alternative Names
	SerialNumber    string    `json:"serial_number"` // Certificate serial number
	Fingerprint     string    `json:"fingerprint"`   // SHA256 fingerprint
}

// CertificateMetrics contains all certificate-related metrics and status.
type CertificateMetrics struct {
	Certificates []Certificate `json:"certificates"`

	// Certificate manager info (certbot or acme.sh)
	CertManager        string `json:"cert_manager"`                   // "certbot", "acme.sh", "none"
	CertManagerVersion string `json:"cert_manager_version,omitempty"` // Version string
	CertManagerHome    string `json:"cert_manager_home,omitempty"`    // Home directory

	// Auto-renewal status
	AutoRenewalEnabled bool   `json:"auto_renewal_enabled"`            // Timer/cron active
	RenewalMethod      string `json:"renewal_method,omitempty"`        // "systemd", "cron", "none"
	RenewalService     string `json:"renewal_service,omitempty"`       // Timer/service name or cron file path

	// Legacy fields for backwards compatibility (deprecated, use CertManager* fields)
	AcmeshInstalled bool   `json:"acmesh_installed"`
	AcmeshVersion   string `json:"acmesh_version,omitempty"`
	AcmeshHome      string `json:"acmesh_home,omitempty"`
	CronEnabled     bool   `json:"cron_enabled"`

	CollectedAt time.Time `json:"collected_at"`
	Error       string    `json:"error,omitempty"`
}

// RefreshResult contains the result of a certificate refresh operation.
// For certbot: certbot renew --cert-name <domain> -> deploy hook -> systemctl reload haproxy
// For acme.sh: acme.sh --renew -> install to /etc/haproxy/certs -> systemctl reload haproxy
type RefreshResult struct {
	Domain    string `json:"domain"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Output    string `json:"output,omitempty"`
	Renewed   bool   `json:"renewed"`   // Certificate was renewed
	Installed bool   `json:"installed"` // Certificate was installed to HAProxy
	Reloaded  bool   `json:"reloaded"`  // HAProxy was reloaded
}

// Common certificate sources.
const (
	SourceCertbot    = "certbot"
	SourceAcmeSH     = "acme.sh"
	SourceSelfSigned = "self-signed"
	SourceManual     = "manual"
)

// Certificate manager types.
const (
	CertManagerCertbot = "certbot"
	CertManagerAcmeSH  = "acme.sh"
	CertManagerNone    = "none"
)

// Renewal method types.
const (
	RenewalMethodSystemd = "systemd"
	RenewalMethodCron    = "cron"
	RenewalMethodNone    = "none"
)

// Common Let's Encrypt issuer patterns.
var letsEncryptIssuers = []string{
	"Let's Encrypt",
	"R3",
	"R10",
	"R11",
	"E1",
	"E5",
	"E6",
}

// IsLetsEncrypt checks if the issuer is Let's Encrypt.
// Checks both exact matches and contains for common patterns.
func IsLetsEncrypt(issuer string) bool {
	// Exact match check
	for _, le := range letsEncryptIssuers {
		if issuer == le {
			return true
		}
	}

	// Contains check for variations like "(STAGING) Let's Encrypt" or "ISRG Root"
	lowerIssuer := strings.ToLower(issuer)
	if strings.Contains(lowerIssuer, "let's encrypt") ||
		strings.Contains(lowerIssuer, "letsencrypt") ||
		strings.Contains(lowerIssuer, "isrg") {
		return true
	}

	return false
}
