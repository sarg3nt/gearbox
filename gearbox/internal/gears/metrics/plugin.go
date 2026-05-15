// Package metrics provides the dashboard's Metrics gear: time-series
// stats, system metrics, KPI band, and error insights, served at
// /metrics. (Earlier revisions of this package called the page
// "/history" — issue #97 renamed it; "history" now refers only to
// distinct concepts like OS-update apt history.)
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

// Gear implements the Metrics dashboard page.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "metrics",
		DisplayName: "Metrics",
		Description: "Time-series stats, system metrics, KPIs, and error insights",
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
// When mounted at /metrics, provides:
//   - GET /metrics/ — main Metrics page
//
// Per-box API endpoints live in the framework handler under
// /api/{boxID}/metrics/* (see cmd/server/main.go).
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.MetricsPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/metrics",
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
