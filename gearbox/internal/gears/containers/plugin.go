// Package containers provides Docker/container runtime monitoring as a gear.
package containers

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Gear implements the containers monitoring gear for the dashboard.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns gear metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "containers",
		DisplayName: "Containers",
		Description: "Monitor and manage Docker containers and Compose stacks",
		Version:     "1.0.0",
		Icon:        "container",
		Category:    "infrastructure",
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

// RegisterRoutes registers HTTP routes for the containers gear.
func (p *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/", p.handlers.ContainersPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/containers",
		Icon:               ContainersIcon(),
		DefaultOrder:       50,
		RequiresPermission: "containers:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return ContainersSettings(config)
}

// Permissions returns the permissions this gear uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "containers",
			Actions:     []string{"view", "manage"},
			Description: "View and manage containers and Compose stacks",
		},
	}
}
