// Package services provides adapter interfaces for core framework services.
// These adapters allow plugins to use framework interfaces while the actual
// implementations remain in their current locations (internal/auth, internal/events, etc.).
package services

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// AuthAdapter wraps the auth.Manager to implement plugin.AuthChecker.
type AuthAdapter struct {
	manager *auth.Manager
}

// NewAuthAdapter creates a new AuthAdapter wrapping an existing auth.Manager.
func NewAuthAdapter(manager *auth.Manager) *AuthAdapter {
	return &AuthAdapter{manager: manager}
}

// IsAuthenticated checks if the request has a valid session.
func (a *AuthAdapter) IsAuthenticated(r *http.Request) bool {
	return a.manager.IsAuthenticated(r)
}

// GetUser retrieves the authenticated user from the request.
func (a *AuthAdapter) GetUser(r *http.Request) *plugin.User {
	user, err := a.manager.GetUser(r)
	if err != nil || user == nil {
		return nil
	}

	return &plugin.User{
		ID:        user.ID,
		Email:     user.Email,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		IsAdmin:   user.IsAdmin(),
	}
}

// HasPermission checks if the current user has a specific permission.
func (a *AuthAdapter) HasPermission(r *http.Request, component, action string) bool {
	return a.manager.HasPermission(r, models.Component(component), models.Permission(action))
}

// GetCSRFToken retrieves the CSRF token from the session.
func (a *AuthAdapter) GetCSRFToken(r *http.Request) (string, error) {
	return a.manager.GetCSRFToken(r)
}

// ValidateCSRFToken validates the CSRF token in the request.
func (a *AuthAdapter) ValidateCSRFToken(r *http.Request) error {
	return a.manager.ValidateCSRFToken(r)
}

// GetUserPermissions retrieves the user's permissions from the request.
// This is needed for pages that need to pass permissions to templates.
// Returns models.UserPermissions for backward compatibility with templates.
func (a *AuthAdapter) GetUserPermissions(r *http.Request) (*models.UserPermissions, error) {
	return a.manager.GetUserPermissions(r)
}

// GetPluginUserPermissions implements plugin.FullAuthChecker.
// Returns the user's permissions in the plugin-defined format.
func (a *AuthAdapter) GetPluginUserPermissions(r *http.Request) (*plugin.UserPermissions, error) {
	modelPerms, err := a.manager.GetUserPermissions(r)
	if err != nil {
		return nil, err
	}
	if modelPerms == nil {
		return &plugin.UserPermissions{Permissions: make(map[string]map[string]bool)}, nil
	}

	// Convert models.UserPermissions to plugin.UserPermissions
	// models uses map[Component][]Permission, plugin uses map[string]map[string]bool
	perms := &plugin.UserPermissions{
		Permissions: make(map[string]map[string]bool),
	}

	// Handle admin users - they have all permissions
	if modelPerms.IsAdmin {
		perms.Permissions["*"] = map[string]bool{"*": true}
		return perms, nil
	}

	for component, actions := range modelPerms.Permissions {
		perms.Permissions[string(component)] = make(map[string]bool)
		for _, action := range actions {
			perms.Permissions[string(component)][string(action)] = true
		}
	}
	return perms, nil
}

// GetFullUser implements plugin.FullAuthChecker.
// Returns the complete user model from the request context.
func (a *AuthAdapter) GetFullUser(r *http.Request) *plugin.FullUser {
	user, err := a.manager.GetUser(r)
	if err != nil || user == nil {
		return nil
	}

	return &plugin.FullUser{
		ID:                 user.ID,
		Email:              user.Email,
		FirstName:          user.FirstName,
		LastName:           user.LastName,
		Role:               string(user.Role),
		MustChangePassword: user.MustChangePassword,
	}
}

// Ensure AuthAdapter implements plugin.AuthChecker and plugin.FullAuthChecker.
var (
	_ plugin.AuthChecker     = (*AuthAdapter)(nil)
	_ plugin.FullAuthChecker = (*AuthAdapter)(nil)
)
