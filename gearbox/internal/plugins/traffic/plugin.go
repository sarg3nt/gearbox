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
