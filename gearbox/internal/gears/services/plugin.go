// Package services provides monitoring and control of system services.
// This plugin shows the status of services like HAProxy, fail2ban, nftables, etc.
package services

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the services monitoring functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "services",
		DisplayName: "Services",
		Description: "Monitor and control system services (HAProxy, fail2ban, nftables, etc.)",
		Version:     "1.0.0",
		Icon:        "cog",
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

// RegisterRoutes registers HTTP routes for the services gear.
// When mounted at /services, provides:
//   - GET /services/ - Main services monitoring page
//
// Note: API endpoints (/api/{serverID}/services, /api/{serverID}/services-config,
// /api/{serverID}/service-control) remain in the main handler as they follow
// the server-scoped API pattern.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.ServicesPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/services",
		Icon:               ServicesIcon(),
		DefaultOrder:       50,
		RequiresPermission: "services:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return ServicesSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "services",
			Actions:     []string{"view", "control"},
			Description: "View and control system services",
		},
	}
}
