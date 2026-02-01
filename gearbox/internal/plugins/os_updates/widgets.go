package os_updates

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterOSUpdatesWidgets registers all OS updates widgets with the widget registry.
func RegisterOSUpdatesWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		updateSummaryCardDefinition(),
		availableUpdatesListDefinition(),
		updateHistoryDefinition(),
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

// Helper to get string from config
func getStringFromConfig(config map[string]any, key string, defaultVal string) string {
	if val, ok := config[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// Helper to get bool from config
func getBoolFromConfig(config map[string]any, key string, defaultVal bool) bool {
	if val, ok := config[key].(bool); ok {
		return val
	}
	return defaultVal
}

// Helper to get int from config
func getIntFromConfig(config map[string]any, key string, defaultVal int) int {
	if val, ok := config[key].(int); ok {
		return val
	}
	if val, ok := config[key].(float64); ok {
		return int(val)
	}
	return defaultVal
}

// updateSummaryCardDefinition creates the Update Summary Card widget.
// Shows count of available updates and security updates.
func updateSummaryCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "update-summary-card",
		Name:        "Update Summary Card",
		Description: "Summary showing count of available updates and security updates",
		Category:    "system-management",
		PluginName:  "os_updates",
		Icon:        "information-circle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     300, // 5 minutes
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 300)
			return UpdateSummaryCardWidget(serverID, refreshSeconds), nil
		},
	}
}

// availableUpdatesListDefinition creates the Available Updates List widget.
// Shows table of packages with available updates.
func availableUpdatesListDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "available-updates-list",
		Name:        "Available Updates List",
		Description: "Table of packages with available updates, filterable by type and installable with selection",
		Category:    "system-management",
		PluginName:  "os_updates",
		Icon:        "clipboard-list",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     300, // 5 minutes
				},
				"show_controls": {
					Type:        "boolean",
					Description: "Show filter and action controls",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 300)
			showControls := getBoolFromConfig(config, "show_controls", true)
			return AvailableUpdatesListWidget(serverID, refreshSeconds, showControls), nil
		},
	}
}

// updateHistoryDefinition creates the Update History widget.
// Shows log of recently installed updates.
func updateHistoryDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "update-history",
		Name:        "Update History",
		Description: "Log of recently installed package updates with timestamps and version information",
		Category:    "system-management",
		PluginName:  "os_updates",
		Icon:        "clock",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"max_entries": {
					Type:        "number",
					Description: "Maximum number of history entries to display",
					Default:     50,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			maxEntries := getIntFromConfig(config, "max_entries", 50)
			return UpdateHistoryWidget(serverID, maxEntries), nil
		},
	}
}
