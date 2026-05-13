package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/sarg3nt/gearbox/internal/framework/models"
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

// RequireAuth is middleware that requires authentication.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := m.GetUser(r)
		if err != nil {
			// Not authenticated, redirect to login with return URL
			returnURL := r.URL.Path
			if r.URL.RawQuery != "" {
				returnURL += "?" + r.URL.RawQuery
			}

			// Construct redirect URL with return parameter
			// Note: Don't show "session expired" message here - on first visit there's no session
			// Messages are handled by specific handlers (logout, etc.)
			redirectURL := "/login"
			if returnURL != "/" && returnURL != "" {
				redirectURL += "?return=" + url.QueryEscape(returnURL)
			}

			http.Redirect(w, r, redirectURL, http.StatusSeeOther)
			return
		}

		// Add user to request context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequirePasswordChange is middleware that enforces password change before allowing any other action.
// This MUST be placed after RequireAuth in the middleware chain.
func (m *Manager) RequirePasswordChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := GetUserFromContext(r.Context())
		if !ok {
			// Should not happen if RequireAuth is before this middleware
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Allow access to account setup, logout, and static assets
		allowedPaths := map[string]bool{
			"/settings/complete-account-setup": true,
			"/logout":                          true,
			"/static/":                         true, // Allow static assets
		}

		// Check if the current path is allowed
		for allowedPath := range allowedPaths {
			if r.URL.Path == allowedPath || (allowedPath == "/static/" && len(r.URL.Path) > 8 && r.URL.Path[:8] == "/static/") {
				next.ServeHTTP(w, r)
				return
			}
		}

		// If user must change password, redirect to account setup page
		if user.MustChangePassword {
			// Only redirect if not already on the account setup page
			if r.URL.Path != "/settings/complete-account-setup" {
				http.Redirect(w, r, "/settings/complete-account-setup", http.StatusSeeOther)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// RequireCSRF is middleware that validates CSRF tokens for POST requests.
func (m *Manager) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check CSRF for state-changing methods
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" || r.Method == "PATCH" {
			if err := m.ValidateCSRFToken(r); err != nil {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
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
