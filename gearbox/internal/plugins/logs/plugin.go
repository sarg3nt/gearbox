// Package logs provides system and HAProxy log viewing functionality.
// This is a compile-time plugin that registers itself via init().
package logs

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the logs viewing functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "logs",
		DisplayName: "System Logs",
		Description: "View and search HAProxy and system logs in real-time",
		Version:     "1.0.0",
		Icon:        "document-text",
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

// RegisterRoutes registers HTTP routes for the logs plugin.
// When mounted at /logs, provides:
//   - GET /logs/ - Main logs viewer page
//
// Note: The API endpoints (/api/{serverID}/logs/{logName} and /api/{serverID}/log-sources)
// remain in the main handler for now, as they follow the server-scoped API pattern.
// These will be migrated when we implement the plugin API contribution system.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.LogsPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/logs",
		Icon:               LogsIcon(),
		DefaultOrder:       40,
		RequiresPermission: "logs:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return LogsSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "logs",
			Actions:     []string{"view", "configure"},
			Description: "View system and HAProxy logs",
		},
	}
}
