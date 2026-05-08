package home

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Handlers serves the Home dashboard pages and APIs.
type Handlers struct {
	deps gear.Dependencies
}

// NewHandlers builds a Handlers using the gear dependencies.
func NewHandlers(deps gear.Dependencies) *Handlers {
	return &Handlers{deps: deps}
}

// IndexPage renders the active dashboard for the current user.
// Phase 1 ships a placeholder; later phases replace this with the
// gridstack-driven tile board.
func (h *Handlers) IndexPage(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	IndexPlaceholder(user).Render(r.Context(), w) //nolint:errcheck
}

// getUser returns the user from the auth context or a placeholder.
func (h *Handlers) getUser(r *http.Request) *models.User {
	if h.deps.Auth == nil {
		return &models.User{Email: "unknown"}
	}
	u := h.deps.Auth.GetUser(r)
	if u == nil {
		return &models.User{Email: "unknown"}
	}
	return &models.User{ID: u.ID, Email: u.Email}
}
