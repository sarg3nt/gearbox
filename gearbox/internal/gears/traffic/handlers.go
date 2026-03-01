package traffic

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers contains HTTP handlers for the traffic gear.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// TrafficPage renders the traffic analysis page.
func (h *Handlers) TrafficPage(w http.ResponseWriter, r *http.Request) {
	// Check permission - traffic uses metrics permission
	if !h.deps.Auth.HasPermission(r, "metrics", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view traffic data", http.StatusForbidden)
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

	// Render the traffic page
	component := pages.TrafficPage(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render traffic page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
