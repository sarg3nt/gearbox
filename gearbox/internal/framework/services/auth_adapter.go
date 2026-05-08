// Package services provides adapter interfaces for core framework services.
// These adapters allow plugins to use framework interfaces while the actual
// implementations remain in their current locations (internal/auth, internal/events, etc.).
package services

import (
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
)

// AuthAdapter wraps the auth.Manager to implement gear.AuthChecker.
type AuthAdapter struct {
	manager   *auth.Manager
	encryptor *crypto.Encryptor
}

// NewAuthAdapter creates a new AuthAdapter wrapping an existing auth.Manager.
// The encryptor is optional — pass nil if no encryption is configured.
func NewAuthAdapter(manager *auth.Manager) *AuthAdapter {
	return &AuthAdapter{manager: manager}
}

// SetEncryptor wires the secrets encryptor onto the adapter. Gears that
// store API keys / passwords downcast Auth to *AuthAdapter and call
// GetEncryptor() to encrypt at rest.
func (a *AuthAdapter) SetEncryptor(e *crypto.Encryptor) {
	a.encryptor = e
}

// GetDB returns the underlying typed database handle. Gears that need typed
// access (rather than raw *sql.DB) downcast Auth to *AuthAdapter and call this.
func (a *AuthAdapter) GetDB() *database.DB {
	return a.manager.GetDB()
}

// GetEncryptor returns the secrets encryptor, or nil when none is configured.
func (a *AuthAdapter) GetEncryptor() *crypto.Encryptor {
	return a.encryptor
}

// IsAuthenticated checks if the request has a valid session.
func (a *AuthAdapter) IsAuthenticated(r *http.Request) bool {
	return a.manager.IsAuthenticated(r)
}

// GetUser retrieves the authenticated user from the request.
func (a *AuthAdapter) GetUser(r *http.Request) *gear.User {
	user, err := a.manager.GetUser(r)
	if err != nil || user == nil {
		return nil
	}

	return &gear.User{
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

// GetGearUserPermissions implements gear.FullAuthChecker.
// Returns the user's permissions in the plugin-defined format.
func (a *AuthAdapter) GetGearUserPermissions(r *http.Request) (*gear.UserPermissions, error) {
	modelPerms, err := a.manager.GetUserPermissions(r)
	if err != nil {
		return nil, err
	}
	if modelPerms == nil {
		return &gear.UserPermissions{Permissions: make(map[string]map[string]bool)}, nil
	}

	// Convert models.UserPermissions to gear.UserPermissions
	// models uses map[Component][]Permission, plugin uses map[string]map[string]bool
	perms := &gear.UserPermissions{
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

// GetFullUser implements gear.FullAuthChecker.
// Returns the complete user model from the request context.
func (a *AuthAdapter) GetFullUser(r *http.Request) *gear.FullUser {
	user, err := a.manager.GetUser(r)
	if err != nil || user == nil {
		return nil
	}

	return &gear.FullUser{
		ID:                 user.ID,
		Email:              user.Email,
		FirstName:          user.FirstName,
		LastName:           user.LastName,
		Role:               string(user.Role),
		MustChangePassword: user.MustChangePassword,
	}
}

// Ensure AuthAdapter implements gear.AuthChecker and gear.FullAuthChecker.
var (
	_ gear.AuthChecker     = (*AuthAdapter)(nil)
	_ gear.FullAuthChecker = (*AuthAdapter)(nil)
)
