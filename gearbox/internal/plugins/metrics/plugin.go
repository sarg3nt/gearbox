// Package metrics provides historical metrics and charts for HAProxy monitoring.
// This plugin shows historical stats, system metrics, and graphs.
package metrics

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the metrics/history functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "metrics",
		DisplayName: "Metrics & History",
		Description: "View historical stats, system metrics, and performance graphs",
		Version:     "1.0.0",
		Icon:        "chart-bar",
		Category:    "monitoring",
		Core:        false,
	}
}

// Initialize sets up the plugin.
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
	if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
		return err
	}

	p.handlers = NewHandlers(deps)

	// Register metrics widgets with the widget registry
	if err := RegisterMetricsWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the metrics plugin.
// When mounted at /history, provides:
//   - GET /history/ - Main history/metrics page
//
// Note: API endpoints (/api/{serverID}/history/*, /api/{serverID}/metrics-history,
// /api/{serverID}/stats-history) remain in the main handler.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.HistoryPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/history",
		Icon:               MetricsIcon(),
		DefaultOrder:       30,
		RequiresPermission: "metrics:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return MetricsSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "metrics",
			Actions:     []string{"view", "configure"},
			Description: "View and configure historical metrics",
		},
	}
}

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	// Widget definitions are registered in widgets.go via RegisterMetricsWidgets
	// This method is for the DashboardPlugin interface
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "System Metrics",
			Description:    "Complete system metrics dashboard with CPU, memory, disk, network, and load graphs",
			Slug:           "system-metrics",
			Icon:           "chart-bar",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: System Metrics
description: System metrics dashboard with CPU, memory, disk, network, and load graphs
created_by: plugin:metrics
plugin_name: metrics
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: cpu-graph-1
    type: cpu-usage-graph
    position:
      row: 1
      column: 1
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 5
      show_title: true
  - id: memory-graph-1
    type: memory-usage-graph
    position:
      row: 1
      column: 7
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 5
      show_title: true
  - id: disk-graph-1
    type: disk-usage-graph
    position:
      row: 2
      column: 1
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 30
      show_title: true
  - id: disk-io-graph-1
    type: disk-io-graph
    position:
      row: 2
      column: 7
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 5
      show_title: true
  - id: network-graph-1
    type: network-traffic-graph
    position:
      row: 3
      column: 1
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 5
      show_title: true
  - id: load-graph-1
    type: load-average-graph
    position:
      row: 3
      column: 7
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 5
      show_title: true
  - id: uptime-1
    type: uptime-display
    position:
      row: 4
      column: 1
      width: 12
      height: 2
    config:
      server_id: ""
      refresh_seconds: 60
      show_title: true
`,
		},
	}
}

// DataSources returns data sources exposed by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "metrics.cpu",
			Description: "CPU usage metrics over time",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of CPU usage percentages",
			},
		},
		{
			Name:        "metrics.memory",
			Description: "Memory usage metrics over time",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of memory usage",
			},
		},
		{
			Name:        "metrics.disk",
			Description: "Disk usage metrics by mount point",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of disk usage by mount point",
			},
		},
		{
			Name:        "metrics.disk-io",
			Description: "Disk I/O read/write rates",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of disk I/O operations",
			},
		},
		{
			Name:        "metrics.network",
			Description: "Network bandwidth usage",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of network traffic",
			},
		},
		{
			Name:        "metrics.load",
			Description: "System load average",
			Schema: plugin.DataSourceSchema{
				Type:        "timeseries",
				Description: "Time-series data of system load average (1m, 5m, 15m)",
			},
		},
		{
			Name:        "metrics.uptime",
			Description: "System uptime",
			Schema: plugin.DataSourceSchema{
				Type:        "object",
				Description: "System uptime information",
			},
		},
	}
}
