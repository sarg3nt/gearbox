package logs

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers provides HTTP request handlers for the logs plugin.
// It handles rendering the logs page and will handle API endpoints
// when the plugin API route contribution system is implemented.
type Handlers struct {
	deps plugin.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps plugin.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// LogsPage renders the main logs viewer page.
// It displays available log sources and provides real-time log streaming.
// Requires the "logs:view" permission.
func (h *Handlers) LogsPage(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.deps.Auth.HasPermission(r, "logs", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view logs", http.StatusForbidden)
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

	// Check if logs integration is enabled for the default server
	defaultServerID := servers[0].ID
	if !h.deps.Servers.IsPluginEnabled(defaultServerID, database.PluginLogs) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Render the logs page
	component := pages.Logs(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render logs page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Note: API handlers (GetLogSources, GetLogs) will be added when we implement
// the plugin API route contribution system. For now, the API routes remain
// in the main handler at /api/{serverID}/logs/{logName} and /api/{serverID}/log-sources.
