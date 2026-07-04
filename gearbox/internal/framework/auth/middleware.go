package auth

import (
	"context"
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/models"
	webcoreauth "github.com/sarg3nt/webcore/core/auth"
)

type contextKey string

const userContextKey contextKey = "user"
const integrationStatusContextKey contextKey = "integrationStatus"
const integrationOrderContextKey contextKey = "integrationOrder"
const userPermissionsContextKey contextKey = "userPermissions"
const selectedBoxContextKey contextKey = "selectedBox"
const allBoxesContextKey contextKey = "allBoxes"

// SidebarIntegration represents an integration for sidebar rendering with order information.
type SidebarIntegration struct {
	Name      string
	Enabled   bool
	SortOrder int
}

// RequireAuth is middleware that requires authentication. It delegates to
// webcore's RequireAuth (which owns the session validation, the /login
// redirect with return URL, and the dev-only loopback bypass in `-tags dev`
// builds), then re-maps the authenticated user from webcore's context slot
// into gearbox's as a concrete *models.User for the 60+ GetUserFromContext
// call sites.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	remap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wcUser, ok := webcoreauth.GetUserFromContext(r.Context()); ok {
			if user := unwrapUser(wcUser); user != nil {
				r = r.WithContext(context.WithValue(r.Context(), userContextKey, user))
			}
		}
		next.ServeHTTP(w, r)
	})
	return m.wc.RequireAuth(remap)
}

// RequirePasswordChange is middleware that enforces password change before
// allowing any other action. MUST be placed after RequireAuth in the chain
// (it reads the webcore context RequireAuth populates). Delegates to webcore
// with gearbox's historical allowlist: the account-setup page, /logout, and
// /static/* (always allowed by webcore).
func (m *Manager) RequirePasswordChange(next http.Handler) http.Handler {
	return m.wc.RequirePasswordChange("/settings/complete-account-setup", "/logout")(next)
}

// RequireCSRF is middleware that validates CSRF tokens for state-changing
// requests (POST/PUT/DELETE/PATCH).
func (m *Manager) RequireCSRF(next http.Handler) http.Handler {
	return m.wc.RequireCSRF(next)
}

// RequireAdmin is middleware that requires admin role.
func (m *Manager) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if !user.IsAdmin() {
			http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GetUserFromContext retrieves the user from the request context.
func GetUserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok
}

// SetGearStatus adds integration status to the context.
func SetGearStatus(ctx context.Context, status map[string]bool) context.Context {
	return context.WithValue(ctx, integrationStatusContextKey, status)
}

// GetGearStatusFromContext retrieves integration status from the context.
func GetGearStatusFromContext(ctx context.Context) (map[string]bool, bool) {
	status, ok := ctx.Value(integrationStatusContextKey).(map[string]bool)
	return status, ok
}

// IsGearEnabledFromContext checks if a specific integration is enabled from context.
// Returns true if the integration is enabled or if the status is not in context (fail open).
func IsGearEnabledFromContext(ctx context.Context, name string) bool {
	status, ok := GetGearStatusFromContext(ctx)
	if !ok {
		// If no status in context, assume enabled (fail open)
		return true
	}
	enabled, exists := status[name]
	if !exists {
		// If integration not in map, assume enabled
		return true
	}
	return enabled
}

// SetIntegrationOrder adds integration order list to the context.
func SetIntegrationOrder(ctx context.Context, integrations []SidebarIntegration) context.Context {
	return context.WithValue(ctx, integrationOrderContextKey, integrations)
}

// GetGearOrderFromContext retrieves ordered integrations from the context.
func GetGearOrderFromContext(ctx context.Context) ([]SidebarIntegration, bool) {
	integrations, ok := ctx.Value(integrationOrderContextKey).([]SidebarIntegration)
	return integrations, ok
}

// SetUserPermissions adds user permissions to the context.
func SetUserPermissions(ctx context.Context, perms *models.UserPermissions) context.Context {
	return context.WithValue(ctx, userPermissionsContextKey, perms)
}

// GetUserPermissionsFromContext retrieves user permissions from the context.
func GetUserPermissionsFromContext(ctx context.Context) (*models.UserPermissions, bool) {
	perms, ok := ctx.Value(userPermissionsContextKey).(*models.UserPermissions)
	return perms, ok
}

// HasPermissionFromContext checks if the user has a specific permission from the context.
func HasPermissionFromContext(ctx context.Context, component models.Component, permission models.Permission) bool {
	perms, ok := GetUserPermissionsFromContext(ctx)
	if !ok || perms == nil {
		return false
	}
	return perms.HasPermission(component, permission)
}

// SetSelectedBox stores the active box context (the box the user is currently
// "in") on the request context. A nil value is a valid signal meaning "no box
// is currently selected" — i.e. the user is on a box-agnostic page such as
// the Bx fleet view, Home dashboard, or Settings.
func SetSelectedBox(ctx context.Context, box *models.BoxConfig) context.Context {
	return context.WithValue(ctx, selectedBoxContextKey, box)
}

// GetSelectedBoxFromContext retrieves the active box, if any. The second
// return value reports whether a box is actually selected — `nil, false`
// means box-agnostic context.
func GetSelectedBoxFromContext(ctx context.Context) (*models.BoxConfig, bool) {
	box, ok := ctx.Value(selectedBoxContextKey).(*models.BoxConfig)
	if !ok || box == nil {
		return nil, false
	}
	return box, true
}

// SetAllBoxes stores the full enabled-box roster on the request context so
// templates (the header chip, the switcher palette) can render it without
// re-querying the database per page.
func SetAllBoxes(ctx context.Context, boxes []models.BoxConfig) context.Context {
	return context.WithValue(ctx, allBoxesContextKey, boxes)
}

// GetAllBoxesFromContext retrieves the enabled-box roster. Returns an empty
// slice if not present (first-run, or middleware not wired) — callers should
// treat that as "no boxes configured."
func GetAllBoxesFromContext(ctx context.Context) []models.BoxConfig {
	boxes, _ := ctx.Value(allBoxesContextKey).([]models.BoxConfig)
	return boxes
}
