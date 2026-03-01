// Package alerts provides alert management and notification functionality.
// This plugin shows active alerts, alert rules, and provides acknowledgement/resolution.
package alerts

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the alert management functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "alerts",
		DisplayName: "Alerts",
		Description: "Manage alerts, rules, and notifications for monitoring events",
		Version:     "1.0.0",
		Icon:        "bell",
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

// RegisterRoutes registers HTTP routes for the alerts gear.
// When mounted at /alerts, provides:
//   - GET /alerts/ - Main alerts page
//
// Note: API endpoints (/api/{serverID}/alerts/*, /api/alerts/*) remain in the
// main handler as they have complex server-scoped patterns.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.AlertsPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/alerts",
		Icon:               AlertsIcon(),
		DefaultOrder:       70,
		RequiresPermission: "alerts:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return AlertsSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "alerts",
			Actions:     []string{"view", "manage", "configure"},
			Description: "View, manage, and configure alerts and rules",
		},
	}
}
