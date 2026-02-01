// Package dashboard provides HAProxy-specific widgets for custom dashboards.
package haproxy

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterHAProxyWidgets registers all HAProxy-specific widgets with the widget registry.
// These are pre-configured, domain-specific widgets that users can add to custom dashboards.
func RegisterHAProxyWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		statusSummaryDoughnutsDefinition(),
		backendStatusGridDefinition(),
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

// statusSummaryDoughnutsDefinition creates the HAProxy Status Summary widget.
// Shows 3 doughnuts: Frontends, Backends, and Servers health status.
func statusSummaryDoughnutsDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "haproxy-status-summary",
		Name:        "HAProxy Status Summary",
		Description: "Three doughnut charts showing health status of frontends, backends, and servers",
		Category:    "haproxy-monitoring",
		PluginName:  "haproxy",
		Icon:        "activity",
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
			// Get server ID from config or use empty string (widget should select first available)
			boxID := getStringFromConfig(config, "box_id", "")
			// For now, use HTMX to load the content dynamically
			return HAProxyStatusSummaryWidgetHTMX(boxID), nil
		},
	}
}

// Helper to get string from config
func getStringFromConfig(config map[string]any, key string, defaultVal string) string {
	if val, ok := config[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// backendStatusGridDefinition creates the Backend Status Grid widget.
// Shows collapsible sections with backend cards grouped by frontend.
func backendStatusGridDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "haproxy-backend-grid",
		Name:        "HAProxy Backend Grid",
		Description: "Collapsible grid of backend status cards grouped by frontend",
		Category:    "haproxy-monitoring",
		PluginName:  "haproxy",
		Icon:        "grid",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"show_filters": {
					Type:        "boolean",
					Description: "Show filter controls",
					Default:     true,
				},
				"default_collapsed": {
					Type:        "boolean",
					Description: "Start with sections collapsed",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			return HAProxyBackendGridWidgetHTMX(boxID), nil
		},
	}
}

