package containers

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
)

// Handlers contains HTTP handlers for the containers gear.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// ContainersPage renders the containers management page.
func (h *Handlers) ContainersPage(w http.ResponseWriter, r *http.Request) {
	if !h.deps.Auth.HasPermission(r, "containers", "view") {
		http.Error(w, "Forbidden: insufficient permissions to view containers", http.StatusForbidden)
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	serverAdapter, ok := h.deps.Servers.(*services.ServerAdapter)
	if !ok {
		h.deps.Logger.Error("containers: failed to get server adapter - unexpected type")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	servers := serverAdapter.GetEnabledServersAsModels()

	if len(servers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	component := ContainersPage(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("containers: failed to render page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
