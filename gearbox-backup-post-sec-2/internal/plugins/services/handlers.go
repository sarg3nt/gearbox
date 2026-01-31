package services

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers contains HTTP handlers for the services plugin.
type Handlers struct {
	deps plugin.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps plugin.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// ServicesPage renders the services monitoring page.
func (h *Handlers) ServicesPage(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.deps.Auth.HasPermission(r, "services", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view services", http.StatusForbidden)
		return
	}

	// Get the full user from context (needed for templates)
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get enabled servers using the ServerAdapter
	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("failed to get server adapter - unexpected type")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	servers := serverAdapter.GetEnabledServersAsModels()

	if len(servers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Check if services integration is enabled for the default server
	defaultServerID := servers[0].ID
	if !h.deps.Servers.IsPluginEnabled(defaultServerID, database.PluginServices) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Render the services page
	// Services config is fetched via API per-server, template handles this
	component := pages.Services(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render services page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
