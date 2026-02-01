package alerts

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterAlertsWidgets registers all alerts widgets with the widget registry.
// These are pre-configured, domain-specific widgets that users can add to custom dashboards.
func RegisterAlertsWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		activeAlertsCardDefinition(),
		acknowledgedAlertsCardDefinition(),
		resolvedAlertsCardDefinition(),
		alertRulesCardDefinition(),
		alertsGridDefinition(),
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

// activeAlertsCardDefinition creates the Active Alerts Card widget.
// Shows count of active alerts requiring attention.
func activeAlertsCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "active-alerts-card",
		Name:        "Active Alerts Card",
		Description: "Displays count of active alerts requiring attention",
		Category:    "alerts",
		PluginName:  "alerts",
		Icon:        "alert-triangle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			return ActiveAlertsCardWidget(boxID), nil
		},
	}
}

// acknowledgedAlertsCardDefinition creates the Acknowledged Alerts Card widget.
// Shows count of alerts being investigated.
func acknowledgedAlertsCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "acknowledged-alerts-card",
		Name:        "Acknowledged Alerts Card",
		Description: "Displays count of acknowledged alerts being investigated",
		Category:    "alerts",
		PluginName:  "alerts",
		Icon:        "check-circle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			return AcknowledgedAlertsCardWidget(boxID), nil
		},
	}
}

// resolvedAlertsCardDefinition creates the Resolved Alerts Card widget.
// Shows count of total resolved alerts.
func resolvedAlertsCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "resolved-alerts-card",
		Name:        "Resolved Alerts Card",
		Description: "Displays count of resolved alerts",
		Category:    "alerts",
		PluginName:  "alerts",
		Icon:        "check-square",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			return ResolvedAlertsCardWidget(boxID), nil
		},
	}
}

// alertRulesCardDefinition creates the Alert Rules Card widget.
// Shows count of enabled alert rules with config link.
func alertRulesCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "alert-rules-card",
		Name:        "Alert Rules Card",
		Description: "Displays count of enabled alert rules with configuration link",
		Category:    "alerts",
		PluginName:  "alerts",
		Icon:        "sliders",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			return AlertRulesCardWidget(boxID), nil
		},
	}
}

// alertsGridDefinition creates the Alerts Grid widget.
// Full alert list with filtering, sorting, and actions.
func alertsGridDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "alerts-grid",
		Name:        "Alerts Grid",
		Description: "Full alert list with filtering, sorting, and action buttons",
		Category:    "alerts",
		PluginName:  "alerts",
		Icon:        "list",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"show_filters": {
					Type:        "boolean",
					Description: "Show status and search filters in the widget (if false, use top bar controls)",
					Default:     false,
				},
				"default_status": {
					Type:        "string",
					Description: "Initial status filter (all, active, acknowledged, resolved)",
					Default:     "active",
					Enum:        []string{"all", "active", "acknowledged", "resolved"},
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			showFilters := getBoolFromConfig(config, "show_filters", false)
			defaultStatus := getStringFromConfig(config, "default_status", "active")
			return AlertsGridWidget(boxID, showFilters, defaultStatus), nil
		},
		TopBarControlsProvider: func(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
			// Only provide top bar controls if in-widget filters are disabled
			showFilters := getBoolFromConfig(config, "show_filters", false)
			if showFilters {
				return []widget.TopBarControl{} // Use in-widget controls instead
			}
			return GetAlertsGridTopBarControls(ctx, config)
		},
	}
}
