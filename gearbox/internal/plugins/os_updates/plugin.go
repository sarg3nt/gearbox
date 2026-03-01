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

	return nil
}

// RegisterRoutes registers HTTP routes for the OS updates plugin.
// When mounted at /os-updates, provides:
//   - GET /os-updates/ - Main OS updates page
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.OSUpdatesPage)
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
