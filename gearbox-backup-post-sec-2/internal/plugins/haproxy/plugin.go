// Package haproxy provides HAProxy monitoring widgets and dashboards.
// This plugin provides HAProxy-specific monitoring capabilities.
package haproxy

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements HAProxy monitoring functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "haproxy",
		DisplayName: "HAProxy",
		Description: "HAProxy reverse proxy monitoring with backend, frontend, and server statistics",
		Version:     "1.0.0",
		Icon:        "activity",
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

	// Register HAProxy widgets with the widget registry
	if err := RegisterHAProxyWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the HAProxy plugin.
// These routes provide HAProxy-specific views and data endpoints.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// HAProxy overview page
	r.Get("/", p.handlers.OverviewPage)

	// Status grid page
	r.Get("/status-grid", p.handlers.StatusGridPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:         "/haproxy",
		Icon:         HAProxyIcon(),
		DefaultOrder: 20,
		ShowAlways:   false,
	}
}

// SettingsPage returns nil as this plugin has no configurable settings.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "haproxy",
			Actions:     []string{"view"},
			Description: "View HAProxy statistics and monitoring data",
		},
	}
}

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	// Widget definitions are registered in widgets.go via RegisterHAProxyWidgets
	// This method is for the DashboardPlugin interface
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "HAProxy Overview",
			Description:    "Complete HAProxy monitoring dashboard with status summary, backend grid, and system metrics",
			Slug:           "haproxy-overview",
			Icon:           "activity",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: HAProxy Overview
description: HAProxy monitoring dashboard with status summary, backend grid, and system metrics
created_by: plugin:haproxy
plugin_name: haproxy
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: status-summary-1
    type: haproxy-status-summary
    position:
      row: 1
      column: 1
      width: 12
      height: auto
    config:
      server_id: ""
  - id: backend-grid-1
    type: haproxy-backend-grid
    position:
      row: 2
      column: 1
      width: 12
      height: auto
    config:
      default_collapsed: false
      server_id: ""
      show_filters: true
  - id: system-metrics-1
    type: system-metrics
    position:
      row: 3
      column: 1
      width: 12
      height: auto
    config:
      server_id: ""
      show_title: true
`,
		},
	}
}

// DataSources returns data sources exposed by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "haproxy.backends",
			Description: "HAProxy backend statistics",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of backend status objects",
			},
		},
		{
			Name:        "haproxy.frontends",
			Description: "HAProxy frontend statistics",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of frontend status objects",
			},
		},
		{
			Name:        "haproxy.servers",
			Description: "HAProxy server statistics",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of server status objects",
			},
		},
	}
}
