package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

const (
	sessionName        = "gearbox-session"
	sessionUserIDKey   = "user_id"
	sessionTokenKey    = "session_token" // OWASP 2026: Server-side session validation
	sessionLoginKey    = "login_time"
	csrfTokenKey       = "csrf_token"
	MaxFailedAttempts  = 5
	LockoutDuration    = 15 * time.Minute
)

// Manager handles authentication and session management.
type Manager struct {
	db           *database.DB
	sessionStore *sessions.CookieStore
	timeout      time.Duration
	logger       *slog.Logger
}

// NewManager creates a new authentication manager.
func NewManager(db *database.DB, sessionSecret string, timeout time.Duration, logger *slog.Logger) (*Manager, error) {
	if len(sessionSecret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 characters")
	}

	// Create cookie store
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   int(timeout.Seconds()),
		HttpOnly: true,
		Secure:   true, // Default to secure; SetSecure(false) only for non-TLS dev environments
		SameSite: http.SameSiteStrictMode,
	}

	return &Manager{
		db:           db,
		sessionStore: store,
		timeout:      timeout,
		logger:       logger,
	}, nil
}

// SetSecure enables secure cookies (for HTTPS).
func (m *Manager) SetSecure(secure bool) {
	m.sessionStore.Options.Secure = secure
}

// Login authenticates a user and creates a session.
func (m *Manager) Login(w http.ResponseWriter, r *http.Request, email, password string) (*models.User, error) {
	// Get user by email
	user, err := m.db.GetUserByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	if user == nil {
		// Don't reveal that the user doesn't exist
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if account is locked
	if user.IsLocked() {
		return nil, fmt.Errorf("account is temporarily locked due to too many failed attempts")
	}

	// Check if account is active
	if user.Status != models.UserStatusActive {
		if user.Status == models.UserStatusPending {
			return nil, fmt.Errorf("account is pending approval")
		}
		return nil, fmt.Errorf("account is disabled")
	}

	// Validate password
	if !CheckPassword(password, user.PasswordHash) {
		// Record failed attempt
		if err := m.db.RecordLoginAttempt(user.ID, false); err != nil {
			m.logger.Error("failed to record login attempt", "error", err, "user_id", user.ID)
		}

		// Log the attempt
		m.logAudit(r, &user.ID, models.AuditActionLoginFailed, "")

		return nil, fmt.Errorf("invalid credentials")
	}

	// Record successful login
	if err := m.db.RecordLoginAttempt(user.ID, true); err != nil {
		m.logger.Error("failed to record login", "error", err, "user_id", user.ID)
	}

	// OWASP 2026: Generate cryptographically secure session token
	sessionToken, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	// Store session token in database for server-side validation
	// This prevents session fixation and allows immediate invalidation
	ip := r.RemoteAddr
	userAgent := r.UserAgent()
	if err := m.db.SetUserSessionToken(user.ID, sessionToken, ip, userAgent); err != nil {
		m.logger.Error("failed to store session token", "error", err, "user_id", user.ID)
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Create session cookie
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Generate CSRF token
	csrfToken, err := GenerateCSRFToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	// Store user ID and session token in cookie
	session.Values[sessionUserIDKey] = user.ID
	session.Values[sessionTokenKey] = sessionToken // CRITICAL: Validated on every request
	session.Values[sessionLoginKey] = time.Now().Unix()
	session.Values[csrfTokenKey] = csrfToken

	// Save session
	if err := session.Save(r, w); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Log the successful login
	m.logAudit(r, &user.ID, models.AuditActionLogin, "")

	m.logger.Info("user logged in", "user_id", user.ID, "ip", ip)

	return user, nil
}

// Logout destroys the user session.
// OWASP 2026: Invalidates server-side session token to prevent replay attacks.
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	// Get current user for audit log
	user, _ := m.GetUser(r)

	// CRITICAL SECURITY: Clear session token from database
	// This invalidates the session server-side immediately
	if user != nil {
		if err := m.db.ClearUserSessionToken(user.ID); err != nil {
			m.logger.Error("failed to clear session token", "error", err, "user_id", user.ID)
		}
		m.logAudit(r, &user.ID, models.AuditActionLogout, "")
		m.logger.Info("user logged out", "user_id", user.ID)
	}

	// Clear client-side cookie
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return err
	}

	session.Values = make(map[interface{}]interface{})
	session.Options.MaxAge = -1

	return session.Save(r, w)
}

// GetUser retrieves the authenticated user from the session.
// OWASP 2026: Validates session token stored in database to prevent session fixation.
func (m *Manager) GetUser(r *http.Request) (*models.User, error) {
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		m.logger.Debug("failed to get session", "error", err)
		return nil, err
	}

	// Check if user ID is in session (now string/UUID)
	userID, ok := session.Values[sessionUserIDKey].(string)
	if !ok || userID == "" {
		return nil, fmt.Errorf("not authenticated")
	}

	// CRITICAL SECURITY: Validate session token from cookie against database
	sessionToken, ok := session.Values[sessionTokenKey].(string)
	if !ok || sessionToken == "" {
		m.logger.Warn("no session token in cookie", "user_id", userID)
		return nil, fmt.Errorf("invalid session: missing token")
	}

	// Check if session has expired (time-based)
	loginTime, ok := session.Values[sessionLoginKey].(int64)
	if !ok {
		return nil, fmt.Errorf("invalid session")
	}

	if time.Since(time.Unix(loginTime, 0)) > m.timeout {
		m.logger.Debug("session expired", "user_id", userID)
		return nil, fmt.Errorf("session expired")
	}

	// Get user from database
	user, err := m.db.GetUserByID(userID)
	if err != nil {
		m.logger.Error("failed to get user from DB", "error", err, "user_id", userID)
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if user == nil {
		m.logger.Warn("user not found", "user_id", userID)
		return nil, fmt.Errorf("user not found")
	}

	// CRITICAL SECURITY: Validate session token against database
	// This prevents old cookies from working after DB wipe or logout
	valid, err := m.db.ValidateSessionToken(userID, sessionToken)
	if err != nil {
		m.logger.Error("session token validation error", "error", err, "user_id", userID)
		return nil, fmt.Errorf("session validation failed")
	}

	if !valid {
		m.logger.Warn("session token invalid in DB", "user_id", userID)
		return nil, fmt.Errorf("session invalid: please log in again")
	}

	// Verify user is still active
	if user.Status != models.UserStatusActive {
		m.logger.Warn("user not active", "user_id", userID, "status", user.Status)
		return nil, fmt.Errorf("account is no longer active")
	}

	return user, nil
}

// GetCSRFToken retrieves the CSRF token from the session.
func (m *Manager) GetCSRFToken(r *http.Request) (string, error) {
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return "", err
	}

	token, ok := session.Values[csrfTokenKey].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("no CSRF token in session")
	}

	return token, nil
}

// ValidateCSRFToken validates a CSRF token from the request.
func (m *Manager) ValidateCSRFToken(r *http.Request) error {
	// Get token from session
	sessionToken, err := m.GetCSRFToken(r)
	if err != nil {
		return fmt.Errorf("failed to get session CSRF token: %w", err)
	}

	// Get token from request (header or form)
	requestToken := r.Header.Get("X-CSRF-Token")
	if requestToken == "" {
		requestToken = r.FormValue("csrf_token")
	}

	if requestToken == "" {
		return fmt.Errorf("no CSRF token in request")
	}

	if requestToken != sessionToken {
		return fmt.Errorf("CSRF token mismatch")
	}

	return nil
}

// IsAuthenticated checks if the request has a valid session.
func (m *Manager) IsAuthenticated(r *http.Request) bool {
	_, err := m.GetUser(r)
	return err == nil
}

// ExtendSession refreshes the session login time to prevent timeout.
func (m *Manager) ExtendSession(w http.ResponseWriter, r *http.Request) error {
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return err
	}

	// Only extend if user is logged in (userID is now string/UUID, not int64)
	userID, ok := session.Values[sessionUserIDKey].(string)
	if !ok || userID == "" {
		return fmt.Errorf("not authenticated")
	}

	// Update login time to now
	session.Values[sessionLoginKey] = time.Now().Unix()

	return session.Save(r, w)
}

// GetSessionExpirationTime returns the time when the current session will expire.
func (m *Manager) GetSessionExpirationTime(r *http.Request) (time.Time, error) {
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return time.Time{}, err
	}

	// Check if user is logged in (userID is now string/UUID, not int64)
	userID, ok := session.Values[sessionUserIDKey].(string)
	if !ok || userID == "" {
		return time.Time{}, fmt.Errorf("not authenticated")
	}

	// Get login time
	loginTime, ok := session.Values[sessionLoginKey].(int64)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid session")
	}

	// Calculate expiration time
	expiresAt := time.Unix(loginTime, 0).Add(m.timeout)
	return expiresAt, nil
}

// ChangePassword changes a user's password.
func (m *Manager) ChangePassword(r *http.Request, userID string, currentPassword, newPassword string) error {
	// Get user
	user, err := m.db.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	// Verify current password
	if !CheckPassword(currentPassword, user.PasswordHash) {
		return fmt.Errorf("current password is incorrect")
	}

	// Validate new password
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Check that new password is different from current
	if CheckPassword(newPassword, user.PasswordHash) {
		return fmt.Errorf("new password must be different from current password")
	}

	// Hash new password
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password
	if err := m.db.UpdateUserPassword(userID, hash, false); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// OWASP 2026: Invalidate session on password change (force re-authentication)
	if err := m.db.ClearUserSessionToken(userID); err != nil {
		m.logger.Error("failed to clear session after password change", "error", err)
	}

	// Log the change
	m.logAudit(r, &userID, models.AuditActionPasswordChange, "")

	return nil
}

// SetPassword sets a user's password (used during forced password change).
func (m *Manager) SetPassword(r *http.Request, userID string, newPassword string) error {
	// Validate new password
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Hash new password
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password
	if err := m.db.UpdateUserPassword(userID, hash, false); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Log the change
	m.logAudit(r, &userID, models.AuditActionPasswordChange, "forced password change")

	return nil
}

// SetPasswordAndEmail sets a user's password and email (used during initial admin setup).
func (m *Manager) SetPasswordAndEmail(r *http.Request, userID string, newPassword, newEmail string) error {
	// Validate new password
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Hash new password
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password and email
	if err := m.db.UpdateUserPasswordAndEmail(userID, hash, newEmail); err != nil {
		return fmt.Errorf("failed to update password and email: %w", err)
	}

	// Log the change
	m.logAudit(r, &userID, models.AuditActionPasswordChange, "initial account setup with email update")

	return nil
}

// RequestPasswordReset initiates a password reset.
func (m *Manager) RequestPasswordReset(email string) (string, *models.User, error) {
	user, err := m.db.GetUserByEmail(email)
	if err != nil {
		return "", nil, err
	}
	if user == nil {
		// Don't reveal that the user doesn't exist
		return "", nil, nil
	}

	// Generate reset token
	token, err := GenerateSecureToken(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate reset token: %w", err)
	}

	// Set expiry (1 hour)
	expiresAt := time.Now().Add(1 * time.Hour)

	// Save token
	if err := m.db.SetPasswordResetToken(user.ID, token, expiresAt); err != nil {
		return "", nil, fmt.Errorf("failed to save reset token: %w", err)
	}

	return token, user, nil
}

// ResetPassword completes a password reset.
func (m *Manager) ResetPassword(r *http.Request, token, newPassword string) error {
	// Get user by token
	user, err := m.db.GetUserByResetToken(token)
	if err != nil {
		return fmt.Errorf("database error: %w", err)
	}
	if user == nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// Validate new password
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Hash new password
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update password and clear token
	if err := m.db.UpdateUserPassword(user.ID, hash, false); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Log the reset
	m.logAudit(r, &user.ID, models.AuditActionPasswordReset, "via reset link")

	return nil
}

// GetDB returns the database instance.
func (m *Manager) GetDB() *database.DB {
	return m.db
}

// GetUserPermissions retrieves permissions for the current user.
func (m *Manager) GetUserPermissions(r *http.Request) (*models.UserPermissions, error) {
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

// RequirePermission returns an error if the user doesn't have the required permission.
func (m *Manager) RequirePermission(r *http.Request, component models.Component, permission models.Permission) error {
	if !m.HasPermission(r, component, permission) {
		return fmt.Errorf("permission denied: %s on %s", permission, component)
	}
	return nil
}

// CreateSessionForUser creates a session for a user without password validation.
// This is used for passkey authentication where the user has already been verified.
func (m *Manager) CreateSessionForUser(w http.ResponseWriter, r *http.Request, user *models.User) error {
	// Generate cryptographically secure session token
	sessionToken, err := GenerateSessionToken()
	if err != nil {
		return fmt.Errorf("failed to generate session token: %w", err)
	}

	// Store session token in database for server-side validation
	ip := r.RemoteAddr
	userAgent := r.UserAgent()
	if err := m.db.SetUserSessionToken(user.ID, sessionToken, ip, userAgent); err != nil {
		m.logger.Error("failed to store session token", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Create session cookie
	session, err := m.sessionStore.Get(r, sessionName)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Generate CSRF token
	csrfToken, err := GenerateCSRFToken()
	if err != nil {
		return fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	// Store user ID and session token in cookie
	session.Values[sessionUserIDKey] = user.ID
	session.Values[sessionTokenKey] = sessionToken // CRITICAL: Validated on every request
	session.Values[sessionLoginKey] = time.Now().Unix()
	session.Values[csrfTokenKey] = csrfToken

	// Save session
	if err := session.Save(r, w); err != nil {
		m.logger.Error("failed to save session cookie", "error", err)
		return fmt.Errorf("failed to save session: %w", err)
	}

	m.logger.Info("session created for user", "user_id", user.ID, "ip", ip)
	return nil
}

// LogAudit creates an audit log entry.
func (m *Manager) LogAudit(r *http.Request, userID *string, action, details string) {
	m.logAudit(r, userID, action, details)
}

func (m *Manager) logAudit(r *http.Request, userID *string, action, details string) {
	log := &models.AuditLog{
		UserID:    userID,
		Action:    action,
		Details:   details,
		IPAddress: getClientIP(r),
		UserAgent: r.UserAgent(),
	}

	if err := m.db.CreateAuditLog(log); err != nil {
		m.logger.Error("failed to create audit log",
			"error", err,
			"action", action,
			"details", details)
	}
}

// generateToken generates a random token for CSRF protection.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for reverse proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the chain
		ips := splitAndTrim(xff, ",")
		if len(ips) > 0 {
			return ips[0]
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

func splitAndTrim(s, sep string) []string {
	parts := make([]string, 0)
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimString(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
