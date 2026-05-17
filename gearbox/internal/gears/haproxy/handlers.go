package haproxy

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers provides HTTP request handlers for the HAProxy gear.
// It serves the main overview page and status grid for HAProxy monitoring.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// OverviewPage serves the HAProxy overview page.
// If no servers are configured, redirects to the servers settings page.
//
// Only servers whose agent probe table reports the haproxy gear as
// available are rendered — boxes without HAProxy (e.g. a container-mode
// agent on a TrueNAS host) would otherwise show empty 503-storming
// stat tiles. See issue #112.
func (h *Handlers) OverviewPage(w http.ResponseWriter, r *http.Request) {
	servers := h.getHAProxyServers()

	// If no servers configured, redirect to servers settings page
	if len(servers) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	pages.Overview(user, servers).Render(r.Context(), w) //nolint:errcheck
}

// StatusGridPage serves the status grid page.
//
// Filtered to HAProxy-capable boxes for the same reason as OverviewPage.
func (h *Handlers) StatusGridPage(w http.ResponseWriter, r *http.Request) {
	servers := h.getHAProxyServers()

	if len(servers) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	pages.StatusGrid(user, servers).Render(r.Context(), w) //nolint:errcheck
}

// getHAProxyServers returns the list of enabled servers whose agent
// reports the haproxy gear available. Fail-open: boxes whose capabilities
// can't be fetched are still included so a transient agent outage doesn't
// hide the page entirely.
func (h *Handlers) getHAProxyServers() []models.BoxConfig {
	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("failed to get server adapter - unexpected type")
		return nil
	}
	return serverAdapter.GetEnabledServersWithGearAvailable("haproxy")
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
