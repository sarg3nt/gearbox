// Package certificates provides SSL/TLS certificate monitoring and management.
// This plugin shows certificate expiration status and allows renewal via certbot.
package certificates

import (
	"context"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the certificate monitoring functionality.
type Gear struct {
	gear.BaseGear
	handlers *Handlers
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "certificates",
		DisplayName: "Certificates",
		Description: "Monitor and manage SSL/TLS certificates with certbot integration",
		Version:     "1.0.0",
		Icon:        "shield-check",
		Category:    "security",
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

// RegisterRoutes registers HTTP routes for the certificates gear.
// When mounted at /certificates, provides:
//   - GET /certificates/ - Main certificates page
//
// Note: API endpoints (/api/{serverID}/certificates, /api/{serverID}/certificates/*/refresh,
// /api/{serverID}/certificates/*/download) remain in the main handler.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Main page
	r.Get("/", p.handlers.CertificatesPage)
}

// SidebarItem returns the sidebar configuration.
func (p *Gear) SidebarItem() *gear.SidebarConfig {
	return &gear.SidebarConfig{
		Path:               "/certificates",
		Icon:               CertificatesIcon(),
		DefaultOrder:       60,
		RequiresPermission: "certificates:view",
	}
}

// SettingsPage returns the settings page component.
func (p *Gear) SettingsPage(config map[string]any) templ.Component {
	return CertificatesSettings(config)
}

// Permissions returns the permissions this plugin uses.
func (p *Gear) Permissions() []gear.PermissionDef {
	return []gear.PermissionDef{
		{
			Component:   "certificates",
			Actions:     []string{"view", "action", "download"},
			Description: "View, renew, and download SSL certificates",
		},
	}
}
