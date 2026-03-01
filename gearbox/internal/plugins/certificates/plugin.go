// Package certificates provides SSL/TLS certificate monitoring and management.
// This plugin shows certificate expiration status and allows renewal via certbot.
package certificates

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin implements the certificate monitoring functionality.
type Plugin struct {
	plugin.BasePlugin
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "certificates",
		DisplayName: "Certificates",
		Description: "Monitor and manage SSL/TLS certificates with certbot integration",
		Version:     "1.0.0",
		Icon:        "shield-check",
		Category:    "security",
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

// RegisterRoutes registers HTTP routes for the certificates plugin.
// When mounted at /certificates, provides:
//   - GET /certificates/ - Main certificates page
//
// Note: API endpoints (/api/{serverID}/certificates, /api/{serverID}/certificates/*/refresh,
// /api/{serverID}/certificates/*/download) remain in the main handler.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.CertificatesPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Plugin) SidebarItem() *plugin.SidebarConfig {
	return &plugin.SidebarConfig{
		Path:               "/certificates",
		Icon:               CertificatesIcon(),
		DefaultOrder:       60,
		RequiresPermission: "certificates:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Plugin) SettingsPage(config map[string]any) templ.Component {
	return CertificatesSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Plugin) Permissions() []plugin.PermissionDef {
	return []plugin.PermissionDef{
		{
			Component:   "certificates",
			Actions:     []string{"view", "action", "download"},
			Description: "View, renew, and download SSL certificates",
		},
	}
}
