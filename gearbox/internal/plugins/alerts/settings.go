// Package alerts provides alert management and notification functionality.
package alerts

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

// AlertsSettings returns the settings page component for the alerts plugin.
// It renders configuration options for alert behavior and notifications.
//
// Supported configuration options:
//   - enable_notifications (bool): Whether to send email notifications for alerts
//   - retention_days (int): How long to keep resolved alerts (7, 30, 90, 365)
func AlertsSettings(config map[string]any) templ.Component {
	return alertsSettingsComponent(config)
}

// getConfigInt is a convenience wrapper for plugin.ConfigInt.
// Used by the templ component to extract integer values from config.
func getConfigInt(config map[string]any, key string, defaultValue int) int {
	return plugin.ConfigInt(config, key, defaultValue)
}

// getConfigBool is a convenience wrapper for plugin.ConfigBool.
// Used by the templ component to extract boolean values from config.
func getConfigBool(config map[string]any, key string, defaultValue bool) bool {
	return plugin.ConfigBool(config, key, defaultValue)
}
