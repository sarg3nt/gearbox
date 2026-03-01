// Package certificates provides the certificates plugin for SSL/TLS certificate monitoring.
package certificates

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

// CertificatesSettings returns the settings page component for the certificates gear.
// It renders configuration options for certificate expiration thresholds.
//
// Supported configuration options:
//   - warning_days (int): Days before expiration to show warning (14, 30, 60, 90)
//   - critical_days (int): Days before expiration to show critical (3, 7, 14)
func CertificatesSettings(config map[string]any) templ.Component {
	return certificatesSettingsComponent(config)
}

// getConfigInt is a convenience wrapper for gear.ConfigInt.
// Used by the templ component to extract integer values from config.
func getConfigInt(config map[string]any, key string, defaultValue int) int {
	return gear.ConfigInt(config, key, defaultValue)
}
