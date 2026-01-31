// Package dashboard provides the main monitoring dashboard.
// This is a core plugin that is always enabled and cannot be disabled.
package dashboard

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the main dashboard functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "dashboard",
		DisplayName: "Dashboard",
		Description: "Main monitoring dashboard with HAProxy stats overview",
		Version:     "1.0.0",
		Icon:        "home",
		Category:    "core",
		Core:        true,
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

// RegisterRoutes registers HTTP routes for the dashboard plugin.
// The dashboard mounts at /dashboard but is also accessible at root.
//
// Note: HTMX partials (/htmx/{serverID}/stats, /htmx/{serverID}/metrics) and
// detail pages (/server/{serverID}/frontend/{name}, /server/{serverID}/backend/{name})
// remain in the main handler because they require collector access which is not
// yet available through the plugin dependency system.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main dashboard page
	r.Get("/", p.handlers.OverviewPage)

	// Status grid page
	r.Get("/status-grid", p.handlers.StatusGridPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:         "/",
		Icon:         DashboardIcon(),
		DefaultOrder: 10, // First item in sidebar
		ShowAlways:   true,
	}
}

// SettingsPage returns nil as core plugins don't have configurable settings.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions returns the permissions this plugin uses.
// Dashboard is viewable by all authenticated users.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return nil // No special permissions required
}
