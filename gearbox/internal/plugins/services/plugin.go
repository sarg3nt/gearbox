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
