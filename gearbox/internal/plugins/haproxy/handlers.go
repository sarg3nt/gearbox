package haproxy

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers provides HTTP request handlers for the HAProxy plugin.
// It serves the main overview page and status grid for HAProxy monitoring.
type Handlers struct {
	deps plugin.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps plugin.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// OverviewPage serves the HAProxy overview page.
// If no servers are configured, redirects to the servers settings page.
func (h *Handlers) OverviewPage(w http.ResponseWriter, r *http.Request) {
	servers := h.getServers()

	// If no servers configured, redirect to servers settings page
	if len(servers) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	pages.Overview(user, servers).Render(r.Context(), w) //nolint:errcheck
}

// StatusGridPage serves the status grid page.
func (h *Handlers) StatusGridPage(w http.ResponseWriter, r *http.Request) {
	servers := h.getServers()

	if len(servers) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	pages.StatusGrid(user, servers).Render(r.Context(), w) //nolint:errcheck
}

// getServers returns the list of enabled servers.
func (h *Handlers) getServers() []models.BoxConfig {
	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("failed to get server adapter - unexpected type")
		return nil
	}
	return serverAdapter.GetEnabledServersAsModels()
}

// getUser returns the user from the auth context, or a fallback user.
func (h *Handlers) getUser(r *http.Request) *models.User {
	if h.deps.Auth != nil {
		pluginUser := h.deps.Auth.GetUser(r)
		if pluginUser != nil {
			return &models.User{
				ID:    pluginUser.ID,
				Email: pluginUser.Email,
			}
		}
	}
	return &models.User{Email: "unknown"}
}
