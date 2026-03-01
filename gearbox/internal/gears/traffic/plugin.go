// Package traffic provides traffic analysis and visualization.
// This plugin shows traffic sources, GeoIP data, and network diagrams.
package traffic

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the traffic analysis functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "traffic",
		DisplayName: "Traffic Analysis",
		Description: "Analyze traffic sources, GeoIP data, and network visualization",
		Version:     "1.0.0",
		Icon:        "globe-alt",
		Category:    "monitoring",
		Core:        false,
	}
}

// Initialize sets up the gear.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}

	p.handlers = NewHandlers(deps)

	return nil
}

// RegisterRoutes registers HTTP routes for the traffic gear.
// When mounted at /traffic, provides:
//   - GET /traffic/ - Main traffic analysis page
//
// Note: API endpoints (/api/{serverID}/traffic*, /api/{serverID}/traffic/sources,
// /api/{serverID}/traffic/network) remain in the main handler.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.TrafficPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/traffic",
		Icon:               TrafficIcon(),
		DefaultOrder:       35,
		RequiresPermission: "metrics:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return TrafficSettings(config)
}

// Permissions returns the permissions this plugin uses.
// Traffic uses the metrics permission since it's a view of metrics data.
func (p *Gear) Permissions() []gear.PermissionDef {
	return nil // Uses metrics:view permission
}
