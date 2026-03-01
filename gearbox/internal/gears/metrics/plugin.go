// Package metrics provides historical metrics and charts for HAProxy monitoring.
// This plugin shows historical stats, system metrics, and graphs.
package metrics

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the metrics/history functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "metrics",
		DisplayName: "Metrics & History",
		Description: "View historical stats, system metrics, and performance graphs",
		Version:     "1.0.0",
		Icon:        "chart-bar",
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

// RegisterRoutes registers HTTP routes for the metrics gear.
// When mounted at /history, provides:
//   - GET /history/ - Main history/metrics page
//
// Note: API endpoints (/api/{serverID}/history/*, /api/{serverID}/metrics-history,
// /api/{serverID}/stats-history) remain in the main handler.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.HistoryPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/history",
		Icon:               MetricsIcon(),
		DefaultOrder:       30,
		RequiresPermission: "metrics:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return MetricsSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "metrics",
			Actions:     []string{"view", "configure"},
			Description: "View and configure historical metrics",
		},
	}
}
