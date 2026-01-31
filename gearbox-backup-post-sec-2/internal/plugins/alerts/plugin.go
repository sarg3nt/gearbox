// Package alerts provides alert management and notification functionality.
// This plugin shows active alerts, alert rules, and provides acknowledgement/resolution.
package alerts

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the alert management functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "alerts",
		DisplayName: "Alerts",
		Description: "Manage alerts, rules, and notifications for monitoring events",
		Version:     "1.0.0",
		Icon:        "bell",
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

	// Register widgets with the widget registry
	if err := RegisterAlertsWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

	return nil
}

// RegisterRoutes registers HTTP routes for the alerts plugin.
// When mounted at /alerts, provides:
//   - GET /alerts/ - Main alerts page
//
// Note: API endpoints (/api/{serverID}/alerts/*, /api/alerts/*) remain in the
// main handler as they have complex server-scoped patterns.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.AlertsPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/alerts",
		Icon:               AlertsIcon(),
		DefaultOrder:       70,
		RequiresPermission: "alerts:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return AlertsSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "alerts",
			Actions:     []string{"view", "manage", "configure"},
			Description: "View, manage, and configure alerts and rules",
		},
	}
}

// Widgets returns the list of widgets provided by this plugin.
// Widget definitions are registered in widgets.go via RegisterAlertsWidgets.
// This method is for the DashboardPlugin interface.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns the default dashboard for this plugin.
// This satisfies the DashboardPlugin interface.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Slug:           "alerts",
			Name:           "Alerts",
			Description:    "Monitor and manage system alerts",
			Icon:           "bell",
			DefaultEnabled: true,
			YAML: `
name: Alerts
slug: alerts
description: Monitor and manage system alerts
icon: bell

widgets:
  # Summary cards row
  - id: active-alerts
    type: active-alerts-card
    x: 0
    y: 0
    width: 3
    height: 2
    config:
      server_id: ""

  - id: acknowledged-alerts
    type: acknowledged-alerts-card
    x: 3
    y: 0
    width: 3
    height: 2
    config:
      server_id: ""

  - id: resolved-alerts
    type: resolved-alerts-card
    x: 6
    y: 0
    width: 3
    height: 2
    config:
      server_id: ""

  - id: alert-rules
    type: alert-rules-card
    x: 9
    y: 0
    width: 3
    height: 2
    config:
      server_id: ""

  # Full alerts grid
  - id: alerts-list
    type: alerts-grid
    x: 0
    y: 2
    width: 12
    height: 10
    config:
      server_id: ""
      show_filters: false
      default_status: "active"
`,
		},
	}
}

// DataSources returns the data sources provided by this plugin.
// This satisfies the DashboardPlugin interface.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "alerts.active",
			Description: "Active alerts from the monitoring system",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of active alert objects",
			},
		},
		{
			Name:        "alerts.acknowledged",
			Description: "Acknowledged alerts being investigated",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of acknowledged alert objects",
			},
		},
		{
			Name:        "alerts.resolved",
			Description: "Resolved alerts",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of resolved alert objects",
			},
		},
		{
			Name:        "alert-rules",
			Description: "Configured alert rules",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of alert rule objects",
			},
		},
	}
}
