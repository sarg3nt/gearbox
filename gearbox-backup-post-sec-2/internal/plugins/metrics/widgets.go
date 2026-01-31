package metrics

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterMetricsWidgets registers all metrics widgets with the widget registry.
// These are pre-configured, domain-specific widgets that users can add to custom dashboards.
func RegisterMetricsWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		cpuUsageGraphDefinition(),
		memoryUsageGraphDefinition(),
		diskUsageGraphDefinition(),
		diskIOGraphDefinition(),
		networkTrafficGraphDefinition(),
		loadAverageGraphDefinition(),
		uptimeDisplayDefinition(),
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

// Helper to get bool from config
func getBoolFromConfig(config map[string]any, key string, defaultVal bool) bool {
	if val, ok := config[key].(bool); ok {
		return val
	}
	return defaultVal
}

// cpuUsageGraphDefinition creates the CPU Usage Graph widget.
// Time-series graph of CPU usage percentage.
func cpuUsageGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "cpu-usage-graph",
		Name:        "CPU Usage Graph",
		Description: "Time-series graph showing CPU usage percentage over time",
		Category:    "system-metrics",
		Icon:        "cpu",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     5,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 5)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return CPUUsageGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// memoryUsageGraphDefinition creates the Memory Usage Graph widget.
// Time-series graph of memory usage.
func memoryUsageGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "memory-usage-graph",
		Name:        "Memory Usage Graph",
		Description: "Time-series graph showing memory usage over time",
		Category:    "system-metrics",
		Icon:        "database",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     5,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 5)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return MemoryUsageGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// diskUsageGraphDefinition creates the Disk Usage Graph widget.
// Time-series graph of disk usage by mount point.
func diskUsageGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "disk-usage-graph",
		Name:        "Disk Usage Graph",
		Description: "Time-series graph showing disk usage by mount point",
		Category:    "system-metrics",
		Icon:        "hard-drive",
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
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 30)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return DiskUsageGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// diskIOGraphDefinition creates the Disk I/O Graph widget.
// Time-series graph of disk read/write rates.
func diskIOGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "disk-io-graph",
		Name:        "Disk I/O Graph",
		Description: "Time-series graph showing disk read/write rates",
		Category:    "system-metrics",
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
					Default:     5,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 5)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return DiskIOGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// networkTrafficGraphDefinition creates the Network Traffic Graph widget.
// Time-series graph of network bandwidth.
func networkTrafficGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "network-traffic-graph",
		Name:        "Network Traffic Graph",
		Description: "Time-series graph showing network bandwidth usage",
		Category:    "system-metrics",
		Icon:        "wifi",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     5,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 5)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return NetworkTrafficGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// loadAverageGraphDefinition creates the Load Average Graph widget.
// Time-series graph of system load average.
func loadAverageGraphDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "load-average-graph",
		Name:        "Load Average Graph",
		Description: "Time-series graph showing system load average (1m, 5m, 15m)",
		Category:    "system-metrics",
		Icon:        "trending-up",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     5,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 5)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return LoadAverageGraphWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}

// uptimeDisplayDefinition creates the Uptime Display widget.
// Simple display of system uptime.
func uptimeDisplayDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "uptime-display",
		Name:        "System Uptime",
		Description: "Displays system uptime with formatted output",
		Category:    "system-metrics",
		Icon:        "clock",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     60,
				},
				"show_title": {
					Type:        "boolean",
					Description: "Show widget title",
					Default:     true,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 60)
			showTitle := getBoolFromConfig(config, "show_title", true)
			return UptimeDisplayWidget(serverID, refreshSeconds, showTitle), nil
		},
	}
}
