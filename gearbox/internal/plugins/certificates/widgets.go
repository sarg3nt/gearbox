package certificates

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterCertificatesWidgets registers all certificates widgets with the widget registry.
func RegisterCertificatesWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		certExpirySummaryDefinition(),
		certificatesListDefinition(),
		expiringCertsAlertDefinition(),
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

// Helper to get string from config
func getStringFromConfig(config map[string]any, key string, defaultVal string) string {
	if val, ok := config[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// Helper to get bool from config
func getBoolFromConfig(config map[string]any, key string, defaultVal bool) bool {
	if val, ok := config[key].(bool); ok {
		return val
	}
	return defaultVal
}

// Helper to get int from config
func getIntFromConfig(config map[string]any, key string, defaultVal int) int {
	if val, ok := config[key].(int); ok {
		return val
	}
	if val, ok := config[key].(float64); ok {
		return int(val)
	}
	return defaultVal
}

// certExpirySummaryDefinition creates the Certificate Expiry Summary widget.
func certExpirySummaryDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "cert-expiry-summary",
		Name:        "Certificate Expiry Summary",
		Description: "Summary cards showing expiring soon, expired, and total certificates",
		Category:    "security",
		PluginName:  "certificates",
		Icon:        "shield",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     300,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 300)
			return CertExpirySummaryWidget(serverID, refreshSeconds), nil
		},
	}
}

// certificatesListDefinition creates the Certificates List widget.
func certificatesListDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "certificates-list",
		Name:        "Certificates List",
		Description: "Complete table of all certificates with expiry dates and renewal options",
		Category:    "security",
		PluginName:  "certificates",
		Icon:        "list",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     300,
				},
				"show_filters": {
					Type:        "boolean",
					Description: "Show sort and filter controls in the widget (if false, use top bar controls)",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 300)
			showFilters := getBoolFromConfig(config, "show_filters", false)
			return CertificatesListWidget(serverID, refreshSeconds, showFilters), nil
		},
		TopBarControlsProvider: func(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
			showFilters := getBoolFromConfig(config, "show_filters", false)
			if showFilters {
				return []widget.TopBarControl{}
			}
			return GetCertificatesListTopBarControls(ctx, config)
		},
	}
}

// expiringCertsAlertDefinition creates the Expiring Certificates Alert widget.
func expiringCertsAlertDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "expiring-certs-alert",
		Name:        "Expiring Certificates Alert",
		Description: "Highlighted list of certificates expiring within 30 days",
		Category:    "security",
		PluginName:  "certificates",
		Icon:        "alert-circle",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"server_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"refresh_seconds": {
					Type:        "number",
					Description: "Refresh interval in seconds",
					Default:     300,
				},
				"days_threshold": {
					Type:        "number",
					Description: "Alert for certificates expiring within this many days",
					Default:     30,
				},
				"hide_when_empty": {
					Type:        "boolean",
					Description: "Hide widget when there are no expiring certificates",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			serverID := getStringFromConfig(config, "server_id", "")
			refreshSeconds := getIntFromConfig(config, "refresh_seconds", 300)
			daysThreshold := getIntFromConfig(config, "days_threshold", 30)
			hideWhenEmpty := getBoolFromConfig(config, "hide_when_empty", false)
			return ExpiringCertsAlertWidget(serverID, refreshSeconds, daysThreshold, hideWhenEmpty), nil
		},
	}
}

// GetCertificatesListTopBarControls returns top bar controls for the certificates list widget.
func GetCertificatesListTopBarControls(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
	return []widget.TopBarControl{
		{
			ID:       "certs-sort",
			Priority: 10,
			Group:    "filters",
			Component: templ.Raw(`
				<select id="cert-sort" class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm">
					<option value="expiry-asc">Expiring Soonest</option>
					<option value="expiry-desc">Expiring Latest</option>
					<option value="domain-asc">Domain A-Z</option>
					<option value="domain-desc">Domain Z-A</option>
				</select>
			`),
		},
		{
			ID:       "certs-filter",
			Priority: 20,
			Group:    "filters",
			Component: templ.Raw(`
				<select id="cert-filter" class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm">
					<option value="all">All Certificates</option>
					<option value="expiring">Expiring Soon</option>
					<option value="expired">Expired</option>
					<option value="valid">Valid</option>
				</select>
			`),
		},
	}
}
