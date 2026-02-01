package haproxy

import (
	"net/http"

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

// OverviewPage serves the HAProxy overview page.
// It redirects to the HAProxy dashboard which uses the widget system.
// If no servers are configured, redirects to the servers settings page.
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
		http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
		return
	}

	// Redirect to the HAProxy dashboard (widget-based dashboard)
	http.Redirect(w, r, "/dashboards/haproxy-overview", http.StatusSeeOther)
}

// StatusGridPage serves the status grid page.
// For now, redirects to the HAProxy dashboard. In the future, this could
// render a different dashboard layout focused on status grid view.
func (h *Handlers) StatusGridPage(w http.ResponseWriter, r *http.Request) {
	// Redirect to HAProxy dashboard for now
	http.Redirect(w, r, "/dashboards/haproxy-overview", http.StatusSeeOther)
}
