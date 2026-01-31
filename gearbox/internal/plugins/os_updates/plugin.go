// Package os_updates provides OS package update management functionality.
// This plugin shows available updates, security updates, and update history.
package os_updates

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the OS updates management functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "os_updates",
		DisplayName: "OS Updates",
		Description: "Manage operating system package updates and security patches",
		Version:     "1.0.0",
		Icon:        "cloud-download",
		Category:    "system-management",
		Core:        false,
	}
}

// Initialize sets up the plugin.
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
	if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
		return err
	}

	p.handlers = NewHandlers(deps)

	// Register OS updates widgets with the widget registry
	if err := RegisterOSUpdatesWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the OS updates plugin.
// When mounted at /os-updates, provides:
//   - GET /os-updates/ - Main OS updates page
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.OSUpdatesPage)

	// API endpoints can be added here when implemented
	// r.Post("/install", p.handlers.InstallUpdates)
	// r.Post("/check", p.handlers.CheckForUpdates)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/os-updates",
		Icon:               OSUpdatesIcon(),
		DefaultOrder:       45,
		RequiresPermission: "system:manage",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return OSUpdatesSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "system",
			Actions:     []string{"view", "manage"},
			Description: "View and install system updates",
		},
	}
}

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	// Widget definitions are registered in widgets.go via RegisterOSUpdatesWidgets
	// This method is for the DashboardPlugin interface
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "OS Updates",
			Description:    "Operating system update management dashboard with available updates, security patches, and history",
			Slug:           "os-updates",
			Icon:           "cloud-download",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: OS Updates
description: OS update management dashboard
created_by: plugin:os_updates
plugin_name: os_updates
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: update-summary-1
    type: update-summary-card
    position:
      row: 1
      column: 1
      width: 6
      height: 2
    config:
      server_id: ""
      refresh_seconds: 300
  - id: available-updates-1
    type: available-updates-list
    position:
      row: 2
      column: 1
      width: 12
      height: 8
    config:
      server_id: ""
      refresh_seconds: 300
      show_controls: true
  - id: update-history-1
    type: update-history
    position:
      row: 3
      column: 1
      width: 12
      height: 6
    config:
      server_id: ""
      max_entries: 50
`,
		},
	}
}

// DataSources returns the data source definitions provided by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "update-summary",
			Description: "Summary of available updates including security updates",
		},
		{
			Name:        "available-updates",
			Description: "List of all available package updates",
		},
		{
			Name:        "update-history",
			Description: "History of recently installed updates",
		},
	}
}
