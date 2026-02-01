// Package dashboard provides HAProxy-specific widgets for custom dashboards.
package dashboard

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
		systemMetricsWidgetDefinition(),
		serviceStatusWidgetDefinition(),
		certificateWarningsWidgetDefinition(),
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
		PluginName:  "dashboard",
		Icon:        "activity",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			// Get server ID from config or use first available server
			serverID := getStringFromConfig(config, "server_id", "")
			// For now, use HTMX to load the content dynamically
			return HAProxyStatusSummaryWidgetHTMX(serverID), nil
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
		PluginName:  "dashboard",
		Icon:        "grid",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
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
			serverID := getStringFromConfig(config, "server_id", "")
			return HAProxyBackendGridWidgetHTMX(serverID), nil
		},
	}
}

// systemMetricsWidgetDefinition creates the System Metrics widget.
// Shows CPU, Memory, Disk, and Network metrics.
func systemMetricsWidgetDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "system-metrics",
		Name:        "System Metrics",
		Description: "CPU, memory, disk, and network metrics with real-time updates",
		Category:    "system-monitoring",
		PluginName:  "dashboard",
		Icon:        "cpu",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show section title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			return SystemMetricsWidgetHTMX(serverID), nil
		},
	}
}

// serviceStatusWidgetDefinition creates the Service Status widget.
// Shows status of HAProxy, gearbox-agent, nftables, and fail2ban services.
func serviceStatusWidgetDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "service-status",
		Name:        "Service Status",
		Description: "Status indicators for critical system services",
		Category:    "system-monitoring",
		PluginName:  "dashboard",
		Icon:        "shield",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			// Service status is included in the metrics widget, so just show metrics
			return SystemMetricsWidgetHTMX(serverID), nil
		},
	}
}

// certificateWarningsWidgetDefinition creates the Certificate Warnings widget.
// Shows warnings for expired or expiring SSL/TLS certificates.
func certificateWarningsWidgetDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "certificate-warnings",
		Name:        "Certificate Warnings",
		Description: "Warnings for expired or expiring SSL/TLS certificates",
		Category:    "security-monitoring",
		PluginName:  "dashboard",
		Icon:        "alert-triangle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"hide_when_empty": {
					Type:        "boolean",
					Description: "Hide widget when there are no warnings",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			// Certificate warnings are loaded via JavaScript on the overview page
			// For now, return an empty component - this will be implemented with proper data sources later
			return templ.Raw("<!-- Certificate warnings loaded via JavaScript -->"), nil
		},
	}
}
