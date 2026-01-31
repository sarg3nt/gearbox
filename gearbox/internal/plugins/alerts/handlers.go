package alerts

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers provides HTTP request handlers for the alerts plugin.
// It handles rendering the alerts management page and related alert operations.
type Handlers struct {
	deps plugin.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps plugin.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// AlertsPage renders the alerts management page.
// It displays active alerts, alert history, and allows users to configure
// and manage alert rules. Requires the "alerts:view" permission.
func (h *Handlers) AlertsPage(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.deps.Auth.HasPermission(r, "alerts", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
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

	// Get user permissions for alert actions
	authAdapter, ok := h.deps.Auth.(*services.AuthAdapter)
	var perms *models.UserPermissions
	if ok {
		var err error
		perms, err = authAdapter.GetUserPermissions(r)
		if err != nil {
			h.deps.Logger.Error("failed to get user permissions", "error", err)
			perms = &models.UserPermissions{}
		}
	} else {
		perms = &models.UserPermissions{}
	}

	// Render the alerts page
	component := pages.AlertsPage(user, servers, perms)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render alerts page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
