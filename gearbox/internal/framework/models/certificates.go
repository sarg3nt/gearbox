package models

import "time"

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
	AutoRenewalEnabled bool   `json:"auto_renewal_enabled"`           // Timer/cron active
	RenewalMethod      string `json:"renewal_method,omitempty"`       // "systemd", "cron", "none"
	RenewalService     string `json:"renewal_service,omitempty"`      // Timer/service name or cron file path

	// Legacy fields for backwards compatibility (deprecated)
	AcmeshInstalled bool   `json:"acmesh_installed"`
	AcmeshVersion   string `json:"acmesh_version,omitempty"`
	AcmeshHome      string `json:"acmesh_home,omitempty"`
	CronEnabled     bool   `json:"cron_enabled"`

	CollectedAt time.Time `json:"collected_at"`
	Error       string    `json:"error,omitempty"`
}

// Certificate manager constants.
const (
	CertManagerCertbot = "certbot"
	CertManagerAcmeSH  = "acme.sh"
	CertManagerNone    = "none"
)

// Certificate source constants.
const (
	SourceCertbot    = "certbot"
	SourceAcmeSH     = "acme.sh"
	SourceSelfSigned = "self-signed"
	SourceManual     = "manual"
)

// Renewal method constants.
const (
	RenewalMethodSystemd = "systemd"
	RenewalMethodCron    = "cron"
	RenewalMethodNone    = "none"
)

// RefreshResult contains the result of a certificate refresh operation.
type RefreshResult struct {
	Domain    string `json:"domain"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Output    string `json:"output,omitempty"`
	Renewed   bool   `json:"renewed"`   // Certificate was renewed
	Installed bool   `json:"installed"` // Certificate was installed to HAProxy
	Reloaded  bool   `json:"reloaded"`  // HAProxy was reloaded
}

// CertificateSummary provides a summary of certificate health status.
type CertificateSummary struct {
	TotalCount    int `json:"total_count"`
	ExpiredCount  int `json:"expired_count"`
	ExpiringCount int `json:"expiring_count"` // Expiring within 30 days
	CriticalCount int `json:"critical_count"` // Expiring within 7 days
	HealthyCount  int `json:"healthy_count"`
}

// GetCertificateSummary calculates summary statistics from certificate metrics.
func (m *CertificateMetrics) GetCertificateSummary() CertificateSummary {
	summary := CertificateSummary{
		TotalCount: len(m.Certificates),
	}

	for _, cert := range m.Certificates {
		if cert.IsExpired {
			summary.ExpiredCount++
		} else if cert.DaysUntilExpiry <= 7 {
			summary.CriticalCount++
		} else if cert.DaysUntilExpiry <= 30 {
			summary.ExpiringCount++
		} else {
			summary.HealthyCount++
		}
	}

	return summary
}

// HasCriticalCertificates returns true if there are expired or critically expiring certificates.
func (m *CertificateMetrics) HasCriticalCertificates() bool {
	for _, cert := range m.Certificates {
		if cert.IsExpired || cert.DaysUntilExpiry <= 7 {
			return true
		}
	}
	return false
}

// HasWarningCertificates returns true if there are certificates expiring soon (within 30 days).
func (m *CertificateMetrics) HasWarningCertificates() bool {
	for _, cert := range m.Certificates {
		if cert.DaysUntilExpiry <= 30 && !cert.IsExpired {
			return true
		}
	}
	return false
}
