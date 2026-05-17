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
//
// Only servers whose agent probe table reports the haproxy gear as
// available are rendered — boxes without HAProxy (e.g. a container-mode
// agent on a TrueNAS host) would otherwise show empty 503-storming
// stat tiles. See issue #112.
//
// Empty-state handling:
//   - No enabled boxes at all → redirect to /settings/boxes so the
//     operator can configure one.
//   - Enabled boxes exist but none advertise the haproxy gear → render
//     the page with a tailored InfoAlert pointing at the Bx fleet view
//     and the boxes settings, instead of redirecting (the operator is
//     on the HAProxy page on purpose; we should explain why it's empty,
//     not bounce them away from it).
func (h *Handlers) OverviewPage(w http.ResponseWriter, r *http.Request) {
	all, haproxyServers := h.getServerLists()

	if len(all) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	emptyReason := ""
	if len(haproxyServers) == 0 {
		emptyReason = "None of your connected boxes report an HAProxy gear. " +
			"Check the agent's probe table from the Bx fleet view, or add " +
			"a box that runs HAProxy via Settings → Boxes."
	}
	pages.Overview(user, haproxyServers, emptyReason).Render(r.Context(), w) //nolint:errcheck
}

// StatusGridPage serves the status grid page.
//
// Same empty-state treatment as OverviewPage: redirect to /settings/boxes
// only when there are no enabled boxes; otherwise render with the
// haproxy-capable subset (which may be empty, in which case the template
// shows its own empty state).
func (h *Handlers) StatusGridPage(w http.ResponseWriter, r *http.Request) {
	all, haproxyServers := h.getServerLists()

	if len(all) == 0 {
		http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
		return
	}

	user := h.getUser(r)
	pages.StatusGrid(user, haproxyServers).Render(r.Context(), w) //nolint:errcheck
}

// getServerLists returns (allEnabled, haproxyCapable) in one pair of
// calls so OverviewPage / StatusGridPage can distinguish "no boxes at
// all" (redirect to settings) from "boxes exist but none have HAProxy"
// (render the empty-state in place).
//
// Fail-open: a box whose capabilities aren't reachable is still counted
// as haproxy-capable, matching the behavior in
// ServerAdapter.GetEnabledServersWithGearAvailable.
func (h *Handlers) getServerLists() (all, haproxyCapable []models.BoxConfig) {
	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("failed to get server adapter - unexpected type")
		return nil, nil
	}
	all = serverAdapter.GetEnabledServersAsModels()
	haproxyCapable = serverAdapter.GetEnabledServersWithGearAvailable("haproxy")
	return all, haproxyCapable
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
