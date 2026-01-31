package models

import (
	"time"
)

// UserRole represents the role of a user.
type UserRole string

const (
	RoleAdmin    UserRole = "admin"
	RoleReadOnly UserRole = "readonly"
)

// UserStatus represents the status of a user account.
type UserStatus string

const (
	UserStatusPending  UserStatus = "pending"  // Account request submitted, awaiting approval
	UserStatusActive   UserStatus = "active"   // Account approved and active
	UserStatusDisabled UserStatus = "disabled" // Account disabled by admin
)

// User represents a user in the system.
type User struct {
	ID                   string     `json:"id"` // UUID
	Email                string     `json:"email"`
	PasswordHash         string     `json:"-"` // Never expose in JSON
	FirstName            string     `json:"first_name"`
	LastName             string     `json:"last_name"`
	PhoneNumber          string     `json:"phone_number,omitempty"`
	Role                 UserRole   `json:"role"`
	Status               UserStatus `json:"status"`
	MustChangePassword   bool       `json:"must_change_password"`
	PasswordChangedAt    *time.Time `json:"password_changed_at,omitempty"`
	LastLoginAt          *time.Time `json:"last_login_at,omitempty"`
	FailedLoginAttempts  int        `json:"failed_login_attempts"`
	LockedUntil          *time.Time `json:"locked_until,omitempty"`
	PasswordResetToken   *string    `json:"-"` // Never expose in JSON
	PasswordResetExpires *time.Time `json:"-"` // Never expose in JSON
	// Session security fields (OWASP 2026 compliance)
	SessionToken        *string    `json:"-"` // Never expose in JSON
	SessionCreatedAt    *time.Time `json:"-"` // Never expose in JSON
	SessionLastActivity *time.Time `json:"-"` // Never expose in JSON
	SessionIP           *string    `json:"-"` // Never expose in JSON
	SessionUserAgent    *string    `json:"-"` // Never expose in JSON
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	ApprovedBy          *string    `json:"approved_by,omitempty"` // UUID
	ApprovedAt          *time.Time `json:"approved_at,omitempty"`
}

// DisplayName returns the user's display name.
func (u *User) DisplayName() string {
	if u.FirstName != "" && u.LastName != "" {
		return u.FirstName + " " + u.LastName
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return u.Email
}

// Initials returns the user's initials for avatar display.
func (u *User) Initials() string {
	if u.FirstName != "" && u.LastName != "" {
		return string(u.FirstName[0]) + string(u.LastName[0])
	}
	if u.FirstName != "" {
		return string(u.FirstName[0])
	}
	if u.Email != "" {
		return string(u.Email[0])
	}
	return "?"
}

// IsAdmin returns true if the user has admin role.
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

// IsLocked returns true if the user account is currently locked.
func (u *User) IsLocked() bool {
	if u.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*u.LockedUntil)
}

// CanLogin returns true if the user can attempt to log in.
func (u *User) CanLogin() bool {
	return u.Status == UserStatusActive && !u.IsLocked()
}

// AccountRequest represents a pending account request.
type AccountRequest struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	FirstName   string    `json:"first_name"`
	LastName    string    `json:"last_name"`
	PhoneNumber string    `json:"phone_number,omitempty"`
	Reason      string    `json:"reason,omitempty"` // Why they need access
	Status      string    `json:"status"`           // pending, approved, denied
	CreatedAt   time.Time `json:"created_at"`
	ReviewedBy  *int64    `json:"reviewed_by,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	DenyReason  string    `json:"deny_reason,omitempty"`
}

// Passkey represents a WebAuthn passkey credential.
type Passkey struct {
	ID              int64  `json:"id"`
	UserID          string `json:"user_id"` // UUID
	CredentialID    []byte     `json:"-"` // Raw credential ID
	CredentialIDHex string     `json:"credential_id"` // Hex-encoded for display
	PublicKey       []byte     `json:"-"` // Raw public key
	Name            string     `json:"name"` // User-friendly name like "MacBook Pro"
	AAGUID          []byte     `json:"-"`    // Authenticator Attestation GUID
	SignCount       uint32     `json:"sign_count"`
	BackupEligible  bool       `json:"backup_eligible"`  // BE flag - credential can be backed up
	BackupState     bool       `json:"backup_state"`     // BS flag - credential is backed up
	CreatedAt       time.Time  `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
}

// SessionInfo represents information about the current session.
type SessionInfo struct {
	User      *User
	LoginTime time.Time
	CSRFToken string
}

// SMTPSettings represents email server configuration.
type SMTPSettings struct {
	ID              int64      `json:"id"`
	Host            string     `json:"host"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	Password        string     `json:"-"` // Never expose in JSON
	FromAddress     string     `json:"from_address"`
	FromName        string     `json:"from_name"`
	UseTLS          bool       `json:"use_tls"`
	UseStartTLS     bool       `json:"use_starttls"`
	Enabled         bool       `json:"enabled"`
	UpdatedAt       time.Time  `json:"updated_at"`
	UpdatedBy       string     `json:"updated_by"` // UUID
	TestEmailSentAt *time.Time `json:"test_email_sent_at,omitempty"`
}

// AuditLog represents a security audit log entry.
type AuditLog struct {
	ID        int64     `json:"id"`
	UserID    *string   `json:"user_id,omitempty"` // UUID
	Action    string    `json:"action"`
	Details   string    `json:"details,omitempty"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// Common audit actions
const (
	AuditActionLogin              = "login"
	AuditActionLoginFailed        = "login_failed"
	AuditActionLogout             = "logout"
	AuditActionPasswordChange     = "password_change"
	AuditActionPasswordReset      = "password_reset"
	AuditActionAccountCreated     = "account_created"
	AuditActionAccountApproved    = "account_approved"
	AuditActionAccountDenied      = "account_denied"
	AuditActionAccountDisabled    = "account_disabled"
	AuditActionAccountEnabled     = "account_enabled"
	AuditActionProfileUpdated     = "profile_updated"
	AuditActionPasskeyAdded       = "passkey_added"
	AuditActionPasskeyRemoved     = "passkey_removed"
	AuditActionSMTPSettingsChanged = "smtp_settings_changed"
)
