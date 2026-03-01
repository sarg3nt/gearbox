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
