// Package metrics provides the metrics plugin for historical data viewing.
package metrics

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

// MetricsSettings returns the settings page component for the metrics gear.
// It renders configuration options for historical metrics storage.
//
// Supported configuration options:
//   - store_history (bool): Whether to store historical metrics data
//   - retention_days (int): How long to keep historical data (1, 7, 14, 30, 90)
func MetricsSettings(config map[string]any) templ.Component {
	return metricsSettingsComponent(config)
}

// getConfigInt is a convenience wrapper for gear.ConfigInt.
// Used by the templ component to extract integer values from config.
func getConfigInt(config map[string]any, key string, defaultValue int) int {
	return gear.ConfigInt(config, key, defaultValue)
}

// getConfigBool is a convenience wrapper for gear.ConfigBool.
// Used by the templ component to extract boolean values from config.
func getConfigBool(config map[string]any, key string, defaultValue bool) bool {
	return gear.ConfigBool(config, key, defaultValue)
}
