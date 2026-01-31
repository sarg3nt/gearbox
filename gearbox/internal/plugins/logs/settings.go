// Package logs provides the logs plugin for viewing system and HAProxy logs.
package logs

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

// LogsSettings returns the settings page component for the logs plugin.
// It renders configuration options for log viewing behavior.
//
// Supported configuration options:
//   - default_lines (int): Number of log lines to display by default (50, 100, 200, 500)
//   - auto_refresh (bool): Whether to enable real-time log streaming
func LogsSettings(config map[string]any) templ.Component {
	return logsSettingsComponent(config)
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
