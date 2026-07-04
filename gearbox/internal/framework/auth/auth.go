// Package auth is gearbox's authentication layer. Since the webcore adoption
// it is a thin wrapper: the security-critical mechanics (DB-validated session
// tokens, constant-time login, CSRF, password change/reset, the dev loopback
// bypass) live in github.com/sarg3nt/webcore/core/auth, driven through the
// adapters in adapter.go. Gearbox-specific concerns — RBAC/permissions, the
// gear/box request context, WebAuthn (kept local for passkey-ID
// compatibility), and audit-log storage — remain here.
package auth

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	webcoreauth "github.com/sarg3nt/webcore/core/auth"
)

const (
	sessionName       = "gearbox-session"
	MaxFailedAttempts = 5
	LockoutDuration   = 15 * time.Minute

	// devBypassEnvVar gates the dev-only loopback auto-login (webcore
	// implements the bypass; it only exists in `-tags dev` builds).
	devBypassEnvVar = "GEARBOX_DEV_AUTO_LOGIN"
	// devBypassEmail is the seeded account the bypass logs in as.
	devBypassEmail = "dev"
)

// Manager handles authentication and session management by delegating to a
// webcore auth.Manager. The public API is unchanged from the pre-webcore
// implementation; sessions issued by the old code remain valid (same cookie
// name, same session keys, same secret).
type Manager struct {
	wc     *webcoreauth.Manager
	db     *database.DB
	logger *slog.Logger
}

// NewManager creates a new authentication manager.
// absoluteTimeout=0 disables the hard TTL.
func NewManager(db *database.DB, sessionSecret string, timeout, absoluteTimeout time.Duration, logger *slog.Logger) (*Manager, error) {
	wc, err := webcoreauth.NewManager(webcoreauth.ManagerConfig{
		Store:           userStore{db: db},
		SessionSecret:   sessionSecret,
		Timeout:         timeout,
		AbsoluteTimeout: absoluteTimeout,
		Secure:          true, // default secure; SetSecure(false) only for non-TLS dev
		SessionName:     sessionName,
		Audit:           auditLogger{db: db, logger: logger},
		Logger:          logger,
		LoginPath:       "/login",
		DevBypassEnvVar: devBypassEnvVar,
		DevBypassEmail:  devBypassEmail,
	})
	if err != nil {
		return nil, err
	}
	return &Manager{wc: wc, db: db, logger: logger}, nil
}

// SetSecure enables secure cookies (for HTTPS).
func (m *Manager) SetSecure(secure bool) { m.wc.SetSecure(secure) }

// Login authenticates a user and creates a session.
func (m *Manager) Login(w http.ResponseWriter, r *http.Request, email, password string) (*models.User, error) {
	u, err := m.wc.Login(w, r, email, password)
	if err != nil {
		return nil, err
	}
	return unwrapUser(u), nil
}

// Logout destroys the user session and invalidates the server-side token.
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	return m.wc.Logout(w, r)
}

// GetUser retrieves the authenticated user, validating the server-side
// session token and both timeouts.
func (m *Manager) GetUser(r *http.Request) (*models.User, error) {
	u, err := m.wc.GetUser(r)
	if err != nil {
		return nil, err
	}
	user := unwrapUser(u)
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return user, nil
}

// GetCSRFToken retrieves the CSRF token from the session.
func (m *Manager) GetCSRFToken(r *http.Request) (string, error) { return m.wc.GetCSRFToken(r) }

// ValidateCSRFToken validates a CSRF token from the request (header or form)
// in constant time.
func (m *Manager) ValidateCSRFToken(r *http.Request) error { return m.wc.ValidateCSRFToken(r) }

// IsAuthenticated checks if the request has a valid session.
func (m *Manager) IsAuthenticated(r *http.Request) bool { return m.wc.IsAuthenticated(r) }

// ExtendSession refreshes the sliding window (never the absolute TTL).
func (m *Manager) ExtendSession(w http.ResponseWriter, r *http.Request) error {
	return m.wc.ExtendSession(w, r)
}

// GetSessionExpirationTime returns when the current session's sliding window
// expires.
func (m *Manager) GetSessionExpirationTime(r *http.Request) (time.Time, error) {
	return m.wc.GetSessionExpirationTime(r)
}

// ChangePassword changes a user's password after verifying the current one,
// invalidating the session server-side.
func (m *Manager) ChangePassword(r *http.Request, userID, currentPassword, newPassword string) error {
	return m.wc.ChangePassword(r, userID, currentPassword, newPassword)
}

// SetPassword sets a user's password (used during forced password change).
func (m *Manager) SetPassword(r *http.Request, userID, newPassword string) error {
	return m.wc.SetPassword(r, userID, newPassword)
}

// SetPasswordAndEmail sets a user's password and email (initial admin setup).
// Gearbox-specific: webcore has no combined update, and splitting it would
// leave a window where one write lands without the other.
func (m *Manager) SetPasswordAndEmail(r *http.Request, userID, newPassword, newEmail string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	if err := m.db.UpdateUserPasswordAndEmail(userID, hash, newEmail); err != nil {
		return fmt.Errorf("failed to update password and email: %w", err)
	}
	m.LogAudit(r, &userID, models.AuditActionPasswordChange, "initial account setup with email update")
	return nil
}

// RequestPasswordReset initiates a password reset. Returns ("", nil, nil)
// for unknown emails so callers can't reveal account existence.
func (m *Manager) RequestPasswordReset(email string) (string, *models.User, error) {
	token, u, err := m.wc.RequestPasswordReset(email)
	if err != nil || u == nil {
		return "", nil, err
	}
	return token, unwrapUser(u), nil
}

// ResetPassword completes a password reset via token.
func (m *Manager) ResetPassword(r *http.Request, token, newPassword string) error {
	return m.wc.ResetPassword(r, token, newPassword)
}

// CreateSessionForUser creates a session without password validation (used
// for passkey login where the user is already verified).
func (m *Manager) CreateSessionForUser(w http.ResponseWriter, r *http.Request, user *models.User) error {
	return m.wc.CreateSessionForUser(w, r, authUser{user})
}

// GetDB returns the database instance.
func (m *Manager) GetDB() *database.DB { return m.db }

// GetUserPermissions retrieves permissions for the current user. Prefers the
// user placed in the request context by RequireAuth; falls back to the
// session lookup for callers without the middleware in front of them (SSE
// setup, tests).
func (m *Manager) GetUserPermissions(r *http.Request) (*models.UserPermissions, error) {
	if user, ok := GetUserFromContext(r.Context()); ok && user != nil {
		return m.db.GetUserPermissions(user.ID)
	}
	user, err := m.GetUser(r)
	if err != nil {
		return nil, err
	}
	return m.db.GetUserPermissions(user.ID)
}

// HasPermission checks if the current user has a specific permission.
func (m *Manager) HasPermission(r *http.Request, component models.Component, permission models.Permission) bool {
	perms, err := m.GetUserPermissions(r)
	if err != nil {
		m.logger.Debug("GetUserPermissions error", "error", err)
		return false
	}
	result := perms.HasPermission(component, permission)
	m.logger.Debug("permission check",
		"user_id", perms.UserID,
		"component", component,
		"permission", permission,
		"is_admin", perms.IsAdmin,
		"result", result)
	return result
}

// RequirePermission returns an error if the user doesn't have the required
// permission.
func (m *Manager) RequirePermission(r *http.Request, component models.Component, permission models.Permission) error {
	if !m.HasPermission(r, component, permission) {
		return fmt.Errorf("permission denied: %s on %s", permission, component)
	}
	return nil
}

// LogAudit creates an audit log entry.
func (m *Manager) LogAudit(r *http.Request, userID *string, action, details string) {
	auditLogger{db: m.db, logger: m.logger}.LogAudit(r, userID, action, details)
}
