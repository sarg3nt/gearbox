// Package services provides the services plugin for system service monitoring.
package services

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

// ServicesSettings returns the settings page component for the services plugin.
// It renders configuration options for service monitoring.
//
// Supported configuration options:
//   - auto_refresh (bool): Whether to enable automatic status refresh
//   - refresh_interval (int): How often to refresh status (5, 10, 30, 60 seconds)
func ServicesSettings(config map[string]any) templ.Component {
	return servicesSettingsComponent(config)
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
