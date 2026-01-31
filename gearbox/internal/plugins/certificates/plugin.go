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

	// Register certificates widgets with the widget registry
	if err := RegisterCertificatesWidgets(deps.WidgetRegistry); err != nil {
		return err
	}

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

// Widgets returns the widget definitions provided by this plugin.
func (p *Plugin) Widgets() []plugin.WidgetDefinition {
	return []plugin.WidgetDefinition{}
}

// PredefinedDashboards returns predefined dashboard templates.
func (p *Plugin) PredefinedDashboards() []plugin.DashboardDefinition {
	return []plugin.DashboardDefinition{
		{
			Name:           "Certificates",
			Description:    "TLS certificate monitoring with expiry summary, expiring alerts, and full certificate list",
			Slug:           "certificates",
			Icon:           "shield-check",
			DefaultEnabled: true,
			YAML: `version: "1.0"
name: Certificates
description: TLS certificate monitoring dashboard
created_by: plugin:certificates
plugin_name: certificates
editable: false
layout:
  columns: 12
  gap: 4
widgets:
  - id: cert-summary-1
    type: cert-expiry-summary
    position:
      row: 1
      column: 1
      width: 12
      height: 2
    config:
      server_id: ""
      refresh_seconds: 300
  - id: expiring-certs-1
    type: expiring-certs-alert
    position:
      row: 2
      column: 1
      width: 6
      height: 4
    config:
      server_id: ""
      refresh_seconds: 300
      days_threshold: 30
      hide_when_empty: false
  - id: certs-list-1
    type: certificates-list
    position:
      row: 3
      column: 1
      width: 12
      height: 8
    config:
      server_id: ""
      refresh_seconds: 300
      show_filters: true
`,
		},
	}
}

// DataSources returns data sources exposed by this plugin.
func (p *Plugin) DataSources() []plugin.DataSourceDefinition {
	return []plugin.DataSourceDefinition{
		{
			Name:        "certificates.all",
			Description: "All SSL/TLS certificates",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of certificate objects with expiry dates",
			},
		},
		{
			Name:        "certificates.summary",
			Description: "Certificate expiry summary",
			Schema: plugin.DataSourceSchema{
				Type:        "object",
				Description: "Summary counts of expiring, expired, and total certificates",
			},
		},
		{
			Name:        "certificates.expiring",
			Description: "Expiring certificates",
			Schema: plugin.DataSourceSchema{
				Type:        "array",
				Description: "Array of certificates expiring within threshold days",
			},
		},
	}
}
