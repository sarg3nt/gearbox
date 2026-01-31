// Package traffic provides traffic analysis and visualization.
// This plugin shows traffic sources, GeoIP data, and network diagrams.
package traffic

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the traffic analysis functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "traffic",
		DisplayName: "Traffic Analysis",
		Description: "Analyze traffic sources, GeoIP data, and network visualization",
		Version:     "1.0.0",
		Icon:        "globe-alt",
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

	// Register traffic widgets with the widget registry
	if err := RegisterTrafficWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the traffic plugin.
// When mounted at /traffic, provides:
//   - GET /traffic/ - Main traffic analysis page
//
// Note: API endpoints (/api/{serverID}/traffic*, /api/{serverID}/traffic/sources,
// /api/{serverID}/traffic/network) remain in the main handler.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.TrafficPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/traffic",
		Icon:               TrafficIcon(),
		DefaultOrder:       35,
		RequiresPermission: "metrics:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return TrafficSettings(config)
}

// Permissions returns the permissions this plugin uses.
// Traffic uses the metrics permission since it's a view of metrics data.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return nil // Uses metrics:view permission
}

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	// Widget definitions are registered in widgets.go via RegisterTrafficWidgets
	// This method is for the DashboardPlugin interface
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "Traffic Analysis",
			Description:    "Complete traffic analysis dashboard with summary cards, flow visualization, and detailed tables",
			Slug:           "traffic-analysis",
			Icon:           "globe-alt",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: Traffic Analysis
description: Traffic analysis dashboard with summary, visualization, and tables
created_by: plugin:traffic
plugin_name: traffic
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: traffic-summary-1
    type: traffic-summary-card
    position:
      row: 1
      column: 1
      width: 12
      height: 2
    config:
      server_id: ""
      refresh_seconds: 10
  - id: traffic-flow-viz-1
    type: traffic-flow-visualization
    position:
      row: 2
      column: 1
      width: 12
      height: 8
    config:
      server_id: ""
      height: 550
      show_controls: true
  - id: top-sources-table-1
    type: top-sources-table
    position:
      row: 3
      column: 1
      width: 6
      height: 6
    config:
      server_id: ""
      refresh_seconds: 30
      max_rows: 10
  - id: backend-traffic-table-1
    type: backend-traffic-table
    position:
      row: 3
      column: 7
      width: 6
      height: 6
    config:
      server_id: ""
      refresh_seconds: 30
`,
		},
	}
}

// DataSources returns the data source definitions provided by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "traffic-summary",
			Description: "Traffic summary metrics (requests, bandwidth, sources, error rate, response codes)",
		},
		{
			Name:        "traffic-flow",
			Description: "Network visualization data for traffic flow between sources, HAProxy, and backends",
		},
		{
			Name:        "top-sources",
			Description: "Most active IP addresses with request counts and bandwidth",
		},
		{
			Name:        "backend-traffic",
			Description: "Traffic distribution, latency, and error rates across all backends",
		},
	}
}
