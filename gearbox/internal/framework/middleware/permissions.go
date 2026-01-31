package middleware

import (
	"log/slog"
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// PermissionMiddleware creates a middleware that checks for specific permissions.
func PermissionMiddleware(authManager *auth.Manager, component models.Component, permission models.Permission, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from session
			user, err := authManager.GetUser(r)
			if err != nil {
				logger.Warn("permission check failed - not authenticated", "error", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Get user permissions
			perms, err := authManager.GetUserPermissions(r)
			if err != nil {
				logger.Error("failed to get user permissions", "user_id", user.ID, "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Check if user has the required permission
			if !perms.HasPermission(component, permission) {
				logger.Warn("permission denied",
					"user_id", user.ID,
					"email", user.Email,
					"permission", permission,
					"component", component)
				http.Error(w, "Forbidden - insufficient permissions", http.StatusForbidden)
				return
			}

			// User has permission, continue
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermissions creates a middleware that checks for multiple permissions (user must have ALL).
func RequirePermissions(authManager *auth.Manager, checks []struct {
	Component  models.Component
	Permission models.Permission
}, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from session
			user, err := authManager.GetUser(r)
			if err != nil {
				logger.Warn("permission check failed - not authenticated", "error", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Get user permissions
			perms, err := authManager.GetUserPermissions(r)
			if err != nil {
				logger.Error("failed to get user permissions", "user_id", user.ID, "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Check each required permission
			for _, check := range checks {
				if !perms.HasPermission(check.Component, check.Permission) {
					logger.Warn("permission denied",
						"user_id", user.ID,
						"email", user.Email,
						"permission", check.Permission,
						"component", check.Component)
					http.Error(w, "Forbidden - insufficient permissions", http.StatusForbidden)
					return
				}
			}

			// User has all required permissions, continue
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission creates a middleware that checks for multiple permissions (user must have AT LEAST ONE).
func RequireAnyPermission(authManager *auth.Manager, checks []struct {
	Component  models.Component
	Permission models.Permission
}, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from session
			user, err := authManager.GetUser(r)
			if err != nil {
				logger.Warn("permission check failed - not authenticated", "error", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Get user permissions
			perms, err := authManager.GetUserPermissions(r)
			if err != nil {
				logger.Error("failed to get user permissions", "user_id", user.ID, "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Check if user has at least one of the required permissions
			hasAnyPermission := false
			for _, check := range checks {
				if perms.HasPermission(check.Component, check.Permission) {
					hasAnyPermission = true
					break
				}
			}

			if !hasAnyPermission {
				logger.Warn("permission denied - none of the required permissions",
					"user_id", user.ID,
					"email", user.Email)
				http.Error(w, "Forbidden - insufficient permissions", http.StatusForbidden)
				return
			}

			// User has at least one required permission, continue
			next.ServeHTTP(w, r)
		})
	}
}

// AdminOnly creates a middleware that only allows admin users.
func AdminOnly(authManager *auth.Manager, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from session
			user, err := authManager.GetUser(r)
			if err != nil {
				logger.Warn("admin check failed - not authenticated", "error", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if user is admin
			if !user.IsAdmin() {
				logger.Warn("admin access denied", "user_id", user.ID, "email", user.Email)
				http.Error(w, "Forbidden - admin only", http.StatusForbidden)
				return
			}

			// User is admin, continue
			next.ServeHTTP(w, r)
		})
	}
}
