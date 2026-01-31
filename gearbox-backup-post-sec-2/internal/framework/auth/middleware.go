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

			// Construct redirect URL with return parameter and message
			redirectURL := "/login?message=Session+expired.+Please+log+in+again."
			if returnURL != "/" && returnURL != "" {
				redirectURL += "&return=" + url.QueryEscape(returnURL)
			}

			http.Redirect(w, r, redirectURL, http.StatusSeeOther)
			return
		}

		// Add user to request context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
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

// SetPluginStatus adds integration status to the context.
func SetPluginStatus(ctx context.Context, status map[string]bool) context.Context {
	return context.WithValue(ctx, integrationStatusContextKey, status)
}

// GetPluginStatusFromContext retrieves integration status from the context.
func GetPluginStatusFromContext(ctx context.Context) (map[string]bool, bool) {
	status, ok := ctx.Value(integrationStatusContextKey).(map[string]bool)
	return status, ok
}

// IsPluginEnabledFromContext checks if a specific integration is enabled from context.
// Returns true if the integration is enabled or if the status is not in context (fail open).
func IsPluginEnabledFromContext(ctx context.Context, name string) bool {
	status, ok := GetPluginStatusFromContext(ctx)
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

// GetPluginOrderFromContext retrieves ordered integrations from the context.
func GetPluginOrderFromContext(ctx context.Context) ([]SidebarIntegration, bool) {
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
