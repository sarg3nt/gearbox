package os_updates

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Handlers contains HTTP handlers for the OS updates gear.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// OSUpdatesPage renders the OS updates management page.
func (h *Handlers) OSUpdatesPage(w http.ResponseWriter, r *http.Request) {
	// Check permission - OS updates requires system management permission
	if !h.deps.Auth.HasPermission(r, "system", "manage") {
		http.Error(w, "Forbidden: insufficient permissions to manage system updates", http.StatusForbidden)
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

	// Build page data
	data := pages.OSUpdatesPageData{
		User:     user,
		BoxID: "", // Will be populated by client-side JS
		Servers:  servers,
		// Other fields will be loaded via HTMX
		Config:           nil,
		UpdateStatus:     nil,
		Packages:         nil,
		RebootStatus:     nil,
		UnattendedConfig: nil,
		PipxStatus:       nil,
		Snapshots:        nil,
		History:          nil,
		CanConfigure:     true, // Based on permission check above
		CanAction:        true, // Based on permission check above
	}

	// Render the OS updates page
	component := pages.OSUpdatesPage(data)
	if err := component.Render(r.Context(), w); err != nil {
		h.deps.Logger.Error("failed to render OS updates page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Note: Additional API handlers for installing updates, checking for updates, etc.
// can be added here when the backend functionality is implemented.
