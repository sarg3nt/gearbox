// Package home provides the Home dashboard gear — a box-agnostic start
// page with launcher tiles, service widgets, and bookmarks.
package home

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Gear implements the Home dashboard gear. Unlike most gears it is system-scoped
// (Info.Scope == ScopeSystem) — there is one row per install, not one per box.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns gear metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "home",
		DisplayName: "Home",
		Description: "App dashboard with launcher tiles, service widgets, and bookmarks",
		Version:     "0.1.0",
		Icon:        "home",
		Category:    "system",
		Core:        false,
		Scope:       gear.ScopeSystem,
	}
}

// Initialize wires the request handlers.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	p.handlers = NewHandlers(deps)
	return nil
}

// RegisterRoutes mounts the gear's HTTP routes under /home.
func (p *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/", p.handlers.IndexPage)
	r.Get("/b/{slug}", p.handlers.IndexPage) // board-specific render — same handler for now

	r.Route("/api", func(r chi.Router) {
		// Boards
		r.Get("/boards", p.handlers.ListBoards)
		r.Post("/boards", p.handlers.CreateBoard)
		r.Patch("/boards/{id}", p.handlers.UpdateBoard)
		r.Delete("/boards/{id}", p.handlers.DeleteBoard)

		// Tiles (nested under board for create/list, flat for update/delete)
		r.Get("/boards/{id}/tiles", p.handlers.ListTiles)
		r.Post("/boards/{id}/tiles", p.handlers.CreateTile)
		r.Patch("/tiles/{id}", p.handlers.UpdateTile)
		r.Delete("/tiles/{id}", p.handlers.DeleteTile)

		// Per-user landing-path setter
		r.Post("/landing-path", p.handlers.SetLandingPath)
	})
}

// SidebarItem describes the gear's sidebar entry.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/home",
		Icon:               HomeIcon(),
		DefaultOrder:       1, // Home sits at the top of navigation when enabled.
		RequiresPermission: "home:view",
	}
}

// SettingsPage returns nil — the Home gear's configuration is managed
// from the dashboard itself rather than from the standard gear-settings page.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions declares the access controls used by the gear.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "home",
			Actions:     []string{"view", "edit", "admin"},
			Description: "View, edit, or administer the Home dashboard",
		},
	}
}
