package metrics

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterMetricsChartWidgets registers all metrics chart widgets with the widget registry.
// These are Chart.js-based historical metric charts for dashboard composition.
func RegisterMetricsChartWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		sessionsRequestsChartDefinition(),
		serverHealthChartDefinition(),
		cpuLoadChartDefinition(),
		memoryUsageChartDefinition(),
		networkThroughputChartDefinition(),
		responseTimesChartDefinition(),
		errorRatesChartDefinition(),
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

// sessionsRequestsChartDefinition creates the Sessions & Requests chart widget.
func sessionsRequestsChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-sessions-requests",
		Name:        "Sessions & Requests Chart",
		Description: "Historical chart showing sessions and requests over time",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "activity",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return SessionsRequestsChartPartial(boxID, hours, limit), nil
		},
	}
}

// serverHealthChartDefinition creates the Server Health chart widget.
func serverHealthChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-server-health",
		Name:        "Server Health Chart",
		Description: "Historical chart showing healthy vs total servers",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "heart",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return ServerHealthChartPartial(boxID, hours, limit), nil
		},
	}
}

// cpuLoadChartDefinition creates the CPU Load chart widget.
func cpuLoadChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-cpu-load",
		Name:        "CPU Load Chart",
		Description: "Historical chart showing CPU load average (1m, 5m, 15m)",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "cpu",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return CPULoadChartPartial(boxID, hours, limit), nil
		},
	}
}

// memoryUsageChartDefinition creates the Memory Usage chart widget.
func memoryUsageChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-memory-usage",
		Name:        "Memory Usage Chart",
		Description: "Historical chart showing memory usage percentage",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "database",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return MemoryUsageChartPartial(boxID, hours, limit), nil
		},
	}
}

// networkThroughputChartDefinition creates the Network Throughput chart widget.
func networkThroughputChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-network-throughput",
		Name:        "Network Throughput Chart",
		Description: "Historical chart showing network RX/TX throughput in Mbps",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "wifi",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return NetworkThroughputChartPartial(boxID, hours, limit), nil
		},
	}
}

// responseTimesChartDefinition creates the Response Times chart widget.
func responseTimesChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-response-times",
		Name:        "Response Times Chart",
		Description: "Historical chart showing average response times in milliseconds",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "clock",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return ResponseTimesChartPartial(boxID, hours, limit), nil
		},
	}
}

// errorRatesChartDefinition creates the Error Rates chart widget.
func errorRatesChartDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "chart-error-rates",
		Name:        "Error Rates Chart",
		Description: "Historical chart showing 5xx error counts",
		Category:    "historical-metrics",
		PluginName:  "metrics",
		Icon:        "alert-triangle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (required)",
				},
				"hours": {
					Type:        "number",
					Description: "Time range in hours",
					Default:     24,
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of data points",
					Default:     500,
				},
			},
			Required: []string{"box_id"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			hours := getIntFromConfig(config, "hours", 24)
			limit := getIntFromConfig(config, "limit", 500)
			return ErrorRatesChartPartial(boxID, hours, limit), nil
		},
	}
}
