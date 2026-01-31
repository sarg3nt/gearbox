package traffic

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterTrafficWidgets registers all traffic widgets with the widget registry.
func RegisterTrafficWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		trafficSummaryCardDefinition(),
		trafficFlowVisualizationDefinition(),
		topSourcesTableDefinition(),
		backendTrafficTableDefinition(),
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

// trafficSummaryCardDefinition creates the Traffic Summary Card widget.
// Shows 5 cards: requests, bandwidth, unique sources, error rate, response codes.
func trafficSummaryCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "traffic-summary-card",
		Name:        "Traffic Summary Card",
		Description: "5-card summary showing requests, bandwidth, unique sources, error rate, and response codes",
		Category:    "traffic-monitoring",
		Icon:        "chart-bar",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     10,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 10)
			return TrafficSummaryCardWidget(serverID, refreshSeconds), nil
		},
	}
}

// trafficFlowVisualizationDefinition creates the Traffic Flow Visualization widget.
// Shows D3.js network visualization with controls.
func trafficFlowVisualizationDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "traffic-flow-visualization",
		Name:        "Traffic Flow Visualization",
		Description: "Interactive D3.js network visualization showing traffic flow between sources, HAProxy, and backends",
		Category:    "traffic-monitoring",
		Icon:        "share",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"height": {
					Type:        "number",
					Description: "Height of visualization in pixels",
					Default:     550,
				},
				"show_controls": {
					Type:        "boolean",
					Description: "Show filter and physics controls",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			height := getIntFromConfig(config, "height", 550)
			showControls := getBoolFromConfig(config, "show_controls", true)
			return TrafficFlowVisualizationWidget(serverID, height, showControls), nil
		},
	}
}

// topSourcesTableDefinition creates the Top Sources Table widget.
// Shows most active traffic sources.
func topSourcesTableDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "top-sources-table",
		Name:        "Top Traffic Sources Table",
		Description: "Table showing the most active IP addresses with request counts, bandwidth, and status",
		Category:    "traffic-monitoring",
		Icon:        "users",
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
				"max_rows": {
					Type:        "number",
					Description: "Maximum number of rows to display",
					Default:     10,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 30)
			maxRows := getIntFromConfig(config, "max_rows", 10)
			return TopSourcesTableWidget(serverID, refreshSeconds, maxRows), nil
		},
	}
}

// backendTrafficTableDefinition creates the Backend Traffic Table widget.
// Shows traffic distribution across backends.
func backendTrafficTableDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "backend-traffic-table",
		Name:        "Backend Traffic Table",
		Description: "Table showing traffic distribution, latency, and error rates across all backends",
		Category:    "traffic-monitoring",
		Icon:        "server",
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
			return BackendTrafficTableWidget(serverID, refreshSeconds), nil
		},
	}
}
