package dashboard

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
)

// Handlers provides HTTP request handlers for the dashboard plugin.
// It serves the main overview page and status grid for HAProxy monitoring.
type Handlers struct {
	deps plugin.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps plugin.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// OverviewPage serves the main dashboard page.
// It redirects to the first enabled plugin page, or to setup pages if not configured.
func (h *Handlers) OverviewPage(w http.ResponseWriter, r *http.Request) {
	// Get enabled servers using the ServerAdapter
	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("failed to get server adapter - unexpected type")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	servers := serverAdapter.GetEnabledServersAsModels()

	// If no servers configured, redirect to servers settings page
	if len(servers) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	// Redirect to the first enabled plugin page if available
	if integrations, ok := auth.GetPluginOrderFromContext(r.Context()); ok {
		for _, integration := range integrations {
			if integration.Enabled {
				http.Redirect(w, r, integrationPathFromName(integration.Name), http.StatusSeeOther)
				return
			}
		}
	}

	// No plugins enabled — send to plugins settings page
	http.Redirect(w, r, "/settings/plugins", http.StatusSeeOther)
}

// integrationPathFromName maps an integration name to its URL path.
func integrationPathFromName(name string) string {
	switch name {
	case "haproxy":
		return "/haproxy"
	case "metrics":
		return "/history"
	case "logs":
		return "/logs"
	case "services":
		return "/services"
	case "certificates":
		return "/certificates"
	case "traffic":
		return "/traffic"
	case "alerts":
		return "/alerts"
	case "os_updates":
		return "/os-updates"
	default:
		return "/dashboards/dashboard"
	}
}

// StatusGridPage serves the status grid page.
// For now, redirects to the overview page which handles routing.
func (h *Handlers) StatusGridPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
