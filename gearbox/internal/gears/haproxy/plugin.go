// Package haproxy provides HAProxy monitoring functionality.
// This plugin provides HAProxy-specific monitoring capabilities.
package haproxy

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements HAProxy monitoring functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "haproxy",
		DisplayName: "HAProxy",
		Description: "HAProxy reverse proxy monitoring with backend, frontend, and server statistics",
		Version:     "1.0.0",
		Icon:        "activity",
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

// RegisterRoutes registers HTTP routes for the HAProxy gear.
// These routes provide HAProxy-specific views and data endpoints.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// HAProxy overview page
	r.Get("/", p.handlers.OverviewPage)

	// Status grid page
	r.Get("/status-grid", p.handlers.StatusGridPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:         "/haproxy",
		Icon:         HAProxyIcon(),
		DefaultOrder: 20,
		ShowAlways:   false,
	}
}

// SettingsPage returns nil as this plugin has no configurable settings.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions returns the permissions this plugin uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "haproxy",
			Actions:     []string{"view"},
			Description: "View HAProxy statistics and monitoring data",
		},
	}
}
