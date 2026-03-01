// Package haproxy provides HAProxy monitoring functionality.
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
