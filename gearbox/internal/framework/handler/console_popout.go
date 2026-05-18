package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// ConsolePopoutPage renders a chromeless full-window console session for
// the given box. Reached via the popout button in the in-page console
// manager (window.open → /console/popout/{boxID}). Auth + per-box opt-in
// match APIConsoleCapabilities so the popout can't outrun those gates.
func (h *Handler) ConsolePopoutPage(w http.ResponseWriter, r *http.Request) {
	if _, err := h.authManager.GetUser(r); err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !h.authManager.HasPermission(r, models.ComponentBoxConsole, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Box ID is required", http.StatusBadRequest)
		return
	}
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		http.NotFound(w, r)
		return
	}
	if !server.ConsoleEnabled {
		http.Error(w, "Console is not enabled for this box", http.StatusNotFound)
		return
	}

	component := pages.ConsolePopoutPage(boxID, server.Name)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render console popout page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
