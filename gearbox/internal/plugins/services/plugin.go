// Package services provides monitoring and control of system services.
// This plugin shows the status of services like HAProxy, fail2ban, nftables, etc.
package services

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the services monitoring functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "services",
		DisplayName: "Services",
		Description: "Monitor and control system services (HAProxy, fail2ban, nftables, etc.)",
		Version:     "1.0.0",
		Icon:        "cog",
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

	// Register services widgets with the widget registry
	if err := RegisterServicesWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the services plugin.
// When mounted at /services, provides:
//   - GET /services/ - Main services monitoring page
//
// Note: API endpoints (/api/{serverID}/services, /api/{serverID}/services-config,
// /api/{serverID}/service-control) remain in the main handler as they follow
// the server-scoped API pattern.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.ServicesPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/services",
		Icon:               ServicesIcon(),
		DefaultOrder:       50,
		RequiresPermission: "services:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return ServicesSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "services",
			Actions:     []string{"view", "control"},
			Description: "View and control system services",
		},
	}
}

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	// Widget definitions are registered in widgets.go via RegisterServicesWidgets
	// This method is for the DashboardPlugin interface
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "Services",
			Description:    "System services monitoring with overview, failed services alert, and full service list",
			Slug:           "services",
			Icon:           "cog",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: Services
description: System services monitoring dashboard
created_by: plugin:services
plugin_name: services
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: services-overview-1
    type: services-overview-card
    position:
      row: 1
      column: 1
      width: 4
      height: 2
    config:
      server_id: ""
      refresh_seconds: 30
  - id: failed-services-1
    type: failed-services-alert
    position:
      row: 1
      column: 5
      width: 8
      height: 2
    config:
      server_id: ""
      refresh_seconds: 30
      hide_when_empty: false
  - id: services-list-1
    type: services-list
    position:
      row: 2
      column: 1
      width: 12
      height: 10
    config:
      server_id: ""
      refresh_seconds: 30
      show_filters: true
`,
		},
	}
}

// DataSources returns data sources exposed by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "services.all",
			Description: "All system services",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of service status objects",
			},
		},
		{
			Name:        "services.overview",
			Description: "Services overview summary",
			Schema: plugin.DataSourceSchema{
				Type:        "object",
				Description: "Summary counts of running, stopped, and failed services",
			},
		},
		{
			Name:        "services.failed",
			Description: "Failed services",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of failed service objects",
			},
		},
	}
}
