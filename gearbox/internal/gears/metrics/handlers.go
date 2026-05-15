package metrics

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers contains HTTP handlers for the metrics gear.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// MetricsPage renders the Metrics gear's main page (charts, KPI band,
// error insights). Served at /metrics on the dashboard.
func (h *Handlers) MetricsPage(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.deps.Auth.HasPermission(r, "metrics", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
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

	// Render the Metrics page
	component := pages.Metrics(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render metrics page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
