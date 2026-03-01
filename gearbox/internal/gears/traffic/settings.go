// Package traffic provides the traffic plugin for traffic analysis and visualization.
package traffic

import (
	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

// TrafficSettings returns the settings page component for the traffic gear.
// It renders configuration options for traffic analysis.
//
// Supported configuration options:
//   - enable_geoip (bool): Whether to enable GeoIP enrichment for traffic sources
//   - top_sources_count (int): Number of top sources to display (5, 10, 20, 50)
func TrafficSettings(config map[string]any) templ.Component {
	return trafficSettingsComponent(config)
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
