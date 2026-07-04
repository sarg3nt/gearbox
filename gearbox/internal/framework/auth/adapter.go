package auth

import (
	"errors"
	"net/http"
	"time"

	webcoreauth "github.com/sarg3nt/webcore/core/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// authUser adapts *models.User to webcore's auth.AuthUser interface. An
// adapter struct (rather than methods on models.User) is required because
// User.ID is a field and the interface wants an ID() method — Go forbids a
// field and method sharing a name.
type authUser struct{ u *models.User }

func (a authUser) ID() string               { return a.u.ID }
func (a authUser) Email() string            { return a.u.Email }
func (a authUser) PasswordHash() string     { return a.u.PasswordHash }
func (a authUser) IsLocked() bool           { return a.u.IsLocked() }
func (a authUser) MustChangePassword() bool { return a.u.MustChangePassword }

// StatusError preserves gearbox's historical login messages for non-active
// accounts. These are user-facing and disclosed pre-password-check by design
// (see the webcore AuthUser contract's enumeration note).
func (a authUser) StatusError() error {
	switch a.u.Status {
	case models.UserStatusActive:
		return nil
	case models.UserStatusPending:
		return errors.New("account is pending approval")
	default:
		return errors.New("account is disabled")
	}
}

// unwrapUser recovers the concrete *models.User from a webcore AuthUser
// produced by our store. Returns nil for a nil interface.
func unwrapUser(u webcoreauth.AuthUser) *models.User {
	if u == nil {
		return nil
	}
	if a, ok := u.(authUser); ok {
		return a.u
	}
	return nil
}

// userStore implements webcore's auth.UserStore over gearbox's database.
// Lookups return the untyped-nil interface on not-found, per the contract.
type userStore struct{ db *database.DB }

func (s userStore) GetUserByEmail(email string) (webcoreauth.AuthUser, error) {
	u, err := s.db.GetUserByEmail(email)
	if err != nil || u == nil {
		return nil, err
	}
	return authUser{u}, nil
}

func (s userStore) GetUserByID(id string) (webcoreauth.AuthUser, error) {
	u, err := s.db.GetUserByID(id)
	if err != nil || u == nil {
		return nil, err
	}
	return authUser{u}, nil
}

func (s userStore) GetUserByResetToken(token string) (webcoreauth.AuthUser, error) {
	u, err := s.db.GetUserByResetToken(token)
	if err != nil || u == nil {
		return nil, err
	}
	return authUser{u}, nil
}

func (s userStore) RecordLoginAttempt(id string, success bool) error {
	return s.db.RecordLoginAttempt(id, success)
}

func (s userStore) SetSessionToken(id, token, ip, userAgent string) error {
	return s.db.SetUserSessionToken(id, token, ip, userAgent)
}

func (s userStore) ValidateSessionToken(id, token string) (bool, error) {
	return s.db.ValidateSessionToken(id, token)
}

func (s userStore) ClearSessionToken(id string) error {
	return s.db.ClearUserSessionToken(id)
}

func (s userStore) UpdatePassword(id, hash string, mustChange bool) error {
	return s.db.UpdateUserPassword(id, hash, mustChange)
}

func (s userStore) SetPasswordResetToken(id, token string, expiresAt time.Time) error {
	return s.db.SetPasswordResetToken(id, token, expiresAt)
}

// auditLogger implements webcore's auth.AuditLogger over gearbox's audit_logs
// table. webcore's Action* strings are identical to gearbox's AuditAction*
// values ("login", "login_failed", …), so no mapping is needed.
type auditLogger struct {
	db     *database.DB
	logger interface {
		Error(msg string, args ...any)
	}
}

func (a auditLogger) LogAudit(r *http.Request, userID *string, action, details string) {
	log := &models.AuditLog{
		UserID:    userID,
		Action:    action,
		Details:   details,
		IPAddress: getClientIP(r),
		UserAgent: r.UserAgent(),
	}
	if err := a.db.CreateAuditLog(log); err != nil {
		a.logger.Error("failed to create audit log", "error", err, "action", action, "details", details)
	}
}

// getClientIP extracts the client IP, honoring reverse-proxy headers.
func getClientIP(r *http.Request) string {
	return webcoreauth.ClientIP(r)
}
