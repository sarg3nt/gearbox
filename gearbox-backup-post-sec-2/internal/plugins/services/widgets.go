package services

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterServicesWidgets registers all services widgets with the widget registry.
func RegisterServicesWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		servicesOverviewCardDefinition(),
		servicesListDefinition(),
		failedServicesAlertDefinition(),
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

// servicesOverviewCardDefinition creates the Services Overview Card widget.
// Shows summary: X running, Y stopped, Z failed.
func servicesOverviewCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "services-overview-card",
		Name:        "Services Overview Card",
		Description: "Summary of service statuses: running, stopped, failed",
		Category:    "system-monitoring",
		Icon:        "activity",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     30,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 30)
			return ServicesOverviewCardWidget(serverID, refreshSeconds), nil
		},
	}
}

// servicesListDefinition creates the Services List widget.
// Full list of systemd services with status, controls for start/stop/restart.
func servicesListDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "services-list",
		Name:        "Services List",
		Description: "Complete list of system services with status and control buttons",
		Category:    "system-monitoring",
		Icon:        "list",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     30,
				},
				"show_filters": {
					Type:        "boolean",
					Description: "Show status and search filters in the widget (if false, use top bar controls)",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 30)
			showFilters := getBoolFromConfig(config, "show_filters", false)
			return ServicesListWidget(serverID, refreshSeconds, showFilters), nil
		},
		TopBarControlsProvider: func(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
			// Only provide top bar controls if in-widget filters are disabled
			showFilters := getBoolFromConfig(config, "show_filters", false)
			if showFilters {
				return []widget.TopBarControl{} // Use in-widget controls instead
			}
			return GetServicesListTopBarControls(ctx, config)
		},
	}
}

// failedServicesAlertDefinition creates the Failed Services Alert widget.
// Highlighted list of failed services.
func failedServicesAlertDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "failed-services-alert",
		Name:        "Failed Services Alert",
		Description: "Alert widget showing services that have failed",
		Category:    "system-monitoring",
		Icon:        "alert-triangle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     30,
				},
				"hide_when_empty": {
					Type:        "boolean",
					Description: "Hide widget when there are no failed services",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 30)
			hideWhenEmpty := getBoolFromConfig(config, "hide_when_empty", false)
			return FailedServicesAlertWidget(serverID, refreshSeconds, hideWhenEmpty), nil
		},
	}
}

// GetServicesListTopBarControls returns top bar controls for the services list widget.
func GetServicesListTopBarControls(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
	return []widget.TopBarControl{
		{
			ID:       "services-filter-status",
			Priority: 10,
			Group:    "filters",
			Component: templ.Raw(`
				<select id="service-status-filter" class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm">
					<option value="all">All Services</option>
					<option value="running">Running</option>
					<option value="stopped">Stopped</option>
					<option value="failed">Failed</option>
				</select>
			`),
		},
		{
			ID:       "services-search",
			Priority: 20,
			Group:    "filters",
			Component: templ.Raw(`
				<input
					type="text"
					id="service-search"
					placeholder="Search services..."
					class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm w-64"
				/>
			`),
		},
		{
			ID:       "services-refresh",
			Priority: 30,
			Group:    "actions",
			Component: templ.Raw(`
				<button
					id="refresh-services"
					class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-md text-sm font-medium transition-colors"
				>
					<svg class="w-4 h-4 inline-block mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
					</svg>
					Refresh
				</button>
			`),
		},
	}
}
