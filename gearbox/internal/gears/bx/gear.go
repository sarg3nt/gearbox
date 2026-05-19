// Package bx provides the Bx (Boxes) gear — Gearbox's fleet-overview
// page. It lists every configured box in one place with status dots,
// per-box quick metrics, and click-through to that box's gears.
//
// The Bx gear is box-agnostic: it has no per-box rows in the gears table,
// it is install-wide, and it is always visible in the sidebar regardless
// of whether a specific box is currently active. It is also non-disable-able
// (`Core: true`) because hiding it would orphan the box-switcher chrome.
//
// See [internal/framework/gear.Scope] for the full scope semantics.
package bx

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Gear implements the Bx fleet-view gear.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
	monitor  *statusMonitor
}

// Info returns gear metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "bx",
		DisplayName: "Bx",
		Description: "Fleet overview: list, status, and switcher for every configured box",
		Version:     "0.1.0",
		Icon:        "bx",
		Category:    "system",
		Core:        true, // non-disable-able — it's the box switcher's home
		Scope:       gear.ScopeBoxAgnostic,
	}
}

// Initialize wires handlers and constructs the background status monitor.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.monitor = newStatusMonitor(deps)
	g.handlers = NewHandlers(deps, g.monitor)
	return nil
}

// Start launches the per-box status poller and subscribes to
// box-config-changed events so toggle/edit operations refresh the
// monitor's snapshot for the affected box immediately, instead of
// waiting on the next 30s poll. Without this subscription, /bx would
// keep rendering stale ConsoleEnabled / Enabled values for up to 30s
// after the user clicked save.
func (g *Gear) Start(ctx context.Context) error {
	if g.monitor != nil {
		g.monitor.Start(ctx)
	}
	if g.monitor != nil {
		if hub := g.GetEventHub(); hub != nil {
			hub.Subscribe("box.config_changed", func(e gear.Event) {
				if e.ServerID == "" {
					return
				}
				g.monitor.PokeBox(ctx, e.ServerID)
			})
		}
	}
	return nil
}

// Stop signals the background poller to wind down.
func (g *Gear) Stop(ctx context.Context) error {
	if g.monitor != nil {
		g.monitor.Stop()
	}
	return nil
}

// RegisterRoutes mounts the gear's HTTP routes under /bx.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/", g.handlers.IndexPage)
	r.Route("/api", func(r chi.Router) {
		r.Get("/status", g.handlers.StatusList)   // JSON: all boxes' current status
		r.Get("/events", g.handlers.EventsStream) // SSE: live status pushes
	})
}

// SidebarItem makes Bx the first entry in the sidebar so the fleet view is
// the obvious entry point. The icon is rendered via [BxIcon].
func (g *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/bx",
		Icon:               BxIcon(),
		DefaultOrder:       0, // sits above Home (which is order 1)
		ShowAlways:         true,
		RequiresPermission: "bx:view",
	}
}

// SettingsPage returns nil — the Bx gear has no per-install configuration
// today. Box CRUD lives in /settings/boxes (unchanged).
func (g *Gear) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions declares the access control for the gear.
func (g *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "bx",
			Actions:     []string{"view"},
			Description: "View the Bx fleet overview",
		},
	}
}
