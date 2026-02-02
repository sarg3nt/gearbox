package database

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// generateUUID generates a new UUID v4 for user IDs.
// This is a local helper to avoid import cycles with the auth package.
func generateUUID() string {
	return uuid.New().String()
}

// initUserSchema creates the user-related database tables.
func (d *DB) initUserSchema() error {
	schema := `
	-- Users table
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY, -- UUID for security
		email TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		first_name TEXT NOT NULL DEFAULT '',
		last_name TEXT NOT NULL DEFAULT '',
		phone_number TEXT DEFAULT '',
		role TEXT NOT NULL DEFAULT 'readonly',
		status TEXT NOT NULL DEFAULT 'pending',
		must_change_password INTEGER NOT NULL DEFAULT 0,
		password_changed_at DATETIME,
		last_login_at DATETIME,
		failed_login_attempts INTEGER NOT NULL DEFAULT 0,
		locked_until DATETIME,
		password_reset_token TEXT,
		password_reset_expires DATETIME,
		-- Session security fields (OWASP 2026 compliance)
		session_token TEXT, -- Cryptographically random 128-bit token
		session_created_at DATETIME,
		session_last_activity DATETIME,
		session_ip TEXT,
		session_user_agent TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		approved_by TEXT, -- UUID
		approved_at DATETIME,
		FOREIGN KEY (approved_by) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
	CREATE INDEX IF NOT EXISTS idx_users_reset_token ON users(password_reset_token);
	CREATE INDEX IF NOT EXISTS idx_users_session_token ON users(session_token);

	-- Account requests table (for pending registrations)
	CREATE TABLE IF NOT EXISTS account_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		phone_number TEXT DEFAULT '',
		reason TEXT DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		reviewed_by TEXT, -- UUID
		reviewed_at DATETIME,
		deny_reason TEXT DEFAULT '',
		FOREIGN KEY (reviewed_by) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_account_requests_email ON account_requests(email);
	CREATE INDEX IF NOT EXISTS idx_account_requests_status ON account_requests(status);

	-- Passkeys table for WebAuthn credentials
	CREATE TABLE IF NOT EXISTS passkeys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL, -- UUID
		credential_id BLOB NOT NULL UNIQUE,
		public_key BLOB NOT NULL,
		name TEXT NOT NULL DEFAULT 'Passkey',
		aaguid BLOB,
		sign_count INTEGER NOT NULL DEFAULT 0,
		backup_eligible INTEGER NOT NULL DEFAULT 0,
		backup_state INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	-- Add backup_eligible and backup_state columns if they don't exist (migration)
	-- SQLite doesn't support ADD COLUMN IF NOT EXISTS, so we ignore errors


	CREATE INDEX IF NOT EXISTS idx_passkeys_user_id ON passkeys(user_id);
	CREATE INDEX IF NOT EXISTS idx_passkeys_credential_id ON passkeys(credential_id);

	-- SMTP settings table
	CREATE TABLE IF NOT EXISTS smtp_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 587,
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		from_address TEXT NOT NULL DEFAULT '',
		from_name TEXT NOT NULL DEFAULT 'Gearbox',
		use_tls INTEGER NOT NULL DEFAULT 0,
		use_starttls INTEGER NOT NULL DEFAULT 1,
		enabled INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT, -- UUID
		test_email_sent_at DATETIME,
		FOREIGN KEY (updated_by) REFERENCES users(id)
	);

	-- Audit log table
	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT, -- UUID
		action TEXT NOT NULL,
		details TEXT DEFAULT '',
		ip_address TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);

	-- WebAuthn sessions table for temporary challenge storage
	CREATE TABLE IF NOT EXISTS webauthn_sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER,
		session_data BLOB NOT NULL,
		session_type TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_user_id ON webauthn_sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_expires ON webauthn_sessions(expires_at);

	-- Insert default SMTP settings row if not exists
	INSERT OR IGNORE INTO smtp_settings (id) VALUES (1);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add backup_eligible and backup_state columns if they don't exist
	// SQLite doesn't support ADD COLUMN IF NOT EXISTS, so we check and add manually
	d.migratePasskeysTable()

	return nil
}

// migratePasskeysTable adds missing columns to the passkeys table.
func (d *DB) migratePasskeysTable() {
	// Check if backup_eligible column exists by trying to select it
	_, err := d.db.Exec(`SELECT backup_eligible FROM passkeys LIMIT 1`)
	if err != nil {
		// Column doesn't exist, add it
		// Default to true for backup_eligible since most modern authenticators support backup
		_, _ = d.db.Exec(`ALTER TABLE passkeys ADD COLUMN backup_eligible INTEGER NOT NULL DEFAULT 1`)
		_, _ = d.db.Exec(`ALTER TABLE passkeys ADD COLUMN backup_state INTEGER NOT NULL DEFAULT 0`)
	} else {
		// Columns exist - update any passkeys that have backup_eligible = 0
		// Most modern authenticators (iCloud Keychain, 1Password, etc.) support backup
		// This fixes passkeys created before we started storing the flags
		_, _ = d.db.Exec(`UPDATE passkeys SET backup_eligible = 1 WHERE backup_eligible = 0`)
	}
}

// CreateUser creates a new user in the database.
func (d *DB) CreateUser(user *models.User) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO users (
			email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.Email, user.PasswordHash, user.FirstName, user.LastName, user.PhoneNumber,
		user.Role, user.Status, user.MustChangePassword, time.Now(), time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create user: %w", err)
	}

	return result.LastInsertId()
}

// GetUserByID retrieves a user by ID.
func (d *DB) GetUserByID(id string) (*models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.getUserByIDLocked(id)
}

func (d *DB) getUserByIDLocked(id string) (*models.User, error) {
	user := &models.User{}
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users WHERE id = ?`, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.PhoneNumber,
		&user.Role, &user.Status, &user.MustChangePassword, &user.PasswordChangedAt, &user.LastLoginAt,
		&user.FailedLoginAttempts, &user.LockedUntil, &user.PasswordResetToken, &user.PasswordResetExpires,
		&user.CreatedAt, &user.UpdatedAt, &user.ApprovedBy, &user.ApprovedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetUserByEmail retrieves a user by email.
func (d *DB) GetUserByEmail(email string) (*models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.getUserByEmailLocked(email)
}

func (d *DB) getUserByEmailLocked(email string) (*models.User, error) {
	user := &models.User{}
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users WHERE email = ? COLLATE NOCASE`, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.PhoneNumber,
		&user.Role, &user.Status, &user.MustChangePassword, &user.PasswordChangedAt, &user.LastLoginAt,
		&user.FailedLoginAttempts, &user.LockedUntil, &user.PasswordResetToken, &user.PasswordResetExpires,
		&user.CreatedAt, &user.UpdatedAt, &user.ApprovedBy, &user.ApprovedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetUserByResetToken retrieves a user by password reset token.
func (d *DB) GetUserByResetToken(token string) (*models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	user := &models.User{}
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users WHERE password_reset_token = ? AND password_reset_expires > ?`,
		token, time.Now()).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.PhoneNumber,
		&user.Role, &user.Status, &user.MustChangePassword, &user.PasswordChangedAt, &user.LastLoginAt,
		&user.FailedLoginAttempts, &user.LockedUntil, &user.PasswordResetToken, &user.PasswordResetExpires,
		&user.CreatedAt, &user.UpdatedAt, &user.ApprovedBy, &user.ApprovedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by reset token: %w", err)
	}
	return user, nil
}

// UpdateUser updates a user in the database.
func (d *DB) UpdateUser(user *models.User) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE users SET
			email = ?, password_hash = ?, first_name = ?, last_name = ?, phone_number = ?,
			role = ?, status = ?, must_change_password = ?, password_changed_at = ?,
			last_login_at = ?, failed_login_attempts = ?, locked_until = ?,
			password_reset_token = ?, password_reset_expires = ?,
			updated_at = ?, approved_by = ?, approved_at = ?
		WHERE id = ?`,
		user.Email, user.PasswordHash, user.FirstName, user.LastName, user.PhoneNumber,
		user.Role, user.Status, user.MustChangePassword, user.PasswordChangedAt,
		user.LastLoginAt, user.FailedLoginAttempts, user.LockedUntil,
		user.PasswordResetToken, user.PasswordResetExpires,
		time.Now(), user.ApprovedBy, user.ApprovedAt, user.ID,
	)
	return err
}

// UpdateUserProfile updates only the profile fields (name, phone) for a user.
func (d *DB) UpdateUserProfile(userID string, firstName string, lastName string, phoneNumber string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE users
		SET first_name = ?, last_name = ?, phone_number = ?, updated_at = ?
		WHERE id = ?`,
		firstName, lastName, phoneNumber, time.Now(), userID,
	)
	return err
}

// UpdateUserPassword updates only the password for a user.
func (d *DB) UpdateUserPassword(userID string, passwordHash string, mustChange bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE users SET
			password_hash = ?, must_change_password = ?, password_changed_at = ?,
			password_reset_token = NULL, password_reset_expires = NULL, updated_at = ?
		WHERE id = ?`,
		passwordHash, mustChange, now, now, userID,
	)
	return err
}

// UpdateUserPasswordAndEmail updates the password and email for a user (used during initial setup).
func (d *DB) UpdateUserPasswordAndEmail(userID string, passwordHash string, email string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE users SET
			password_hash = ?, email = ?, must_change_password = 0, password_changed_at = ?,
			password_reset_token = NULL, password_reset_expires = NULL, updated_at = ?
		WHERE id = ?`,
		passwordHash, email, now, now, userID,
	)
	return err
}

// DeleteUser permanently removes a user and their associated data.
func (d *DB) DeleteUser(userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Delete user's passkeys first (foreign key relationship)
	_, err := d.db.Exec(`DELETE FROM passkeys WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user passkeys: %w", err)
	}

	// Delete user's WebAuthn sessions
	_, err = d.db.Exec(`DELETE FROM webauthn_sessions WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user webauthn sessions: %w", err)
	}

	// Delete the user
	result, err := d.db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// SetPasswordResetToken sets a password reset token for a user.
func (d *DB) SetPasswordResetToken(userID string, token string, expiresAt time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE users SET password_reset_token = ?, password_reset_expires = ?, updated_at = ?
		WHERE id = ?`,
		token, expiresAt, time.Now(), userID,
	)
	return err
}

// ClearPasswordResetToken clears the password reset token for a user.
func (d *DB) ClearPasswordResetToken(userID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE users SET password_reset_token = NULL, password_reset_expires = NULL, updated_at = ?
		WHERE id = ?`,
		time.Now(), userID,
	)
	return err
}

// RecordLoginAttempt updates login attempt tracking.
func (d *DB) RecordLoginAttempt(userID string, success bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if success {
		now := time.Now()
		_, err := d.db.Exec(`
			UPDATE users SET
				last_login_at = ?, failed_login_attempts = 0, locked_until = NULL, updated_at = ?
			WHERE id = ?`,
			now, now, userID,
		)
		return err
	}

	// Failed attempt - increment counter and potentially lock
	_, err := d.db.Exec(`
		UPDATE users SET
			failed_login_attempts = failed_login_attempts + 1,
			locked_until = CASE
				WHEN failed_login_attempts >= 4 THEN datetime('now', '+15 minutes')
				ELSE locked_until
			END,
			updated_at = datetime('now')
		WHERE id = ?`,
		userID,
	)
	return err
}

// GetAllUsers retrieves all users.
func (d *DB) GetAllUsers() ([]*models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.PhoneNumber,
			&user.Role, &user.Status, &user.MustChangePassword, &user.PasswordChangedAt, &user.LastLoginAt,
			&user.FailedLoginAttempts, &user.LockedUntil, &user.PasswordResetToken, &user.PasswordResetExpires,
			&user.CreatedAt, &user.UpdatedAt, &user.ApprovedBy, &user.ApprovedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// GetActiveUsers retrieves all active users.
func (d *DB) GetActiveUsers() ([]*models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users WHERE status = 'active' ORDER BY last_name, first_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		err := rows.Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName, &user.PhoneNumber,
			&user.Role, &user.Status, &user.MustChangePassword, &user.PasswordChangedAt, &user.LastLoginAt,
			&user.FailedLoginAttempts, &user.LockedUntil, &user.PasswordResetToken, &user.PasswordResetExpires,
			&user.CreatedAt, &user.UpdatedAt, &user.ApprovedBy, &user.ApprovedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// CountUsers returns the count of users by status.
func (d *DB) CountUsers() (total, active, pending int, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	err = d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total)
	if err != nil {
		return
	}
	err = d.db.QueryRow(`SELECT COUNT(*) FROM users WHERE status = 'active'`).Scan(&active)
	if err != nil {
		return
	}
	err = d.db.QueryRow(`SELECT COUNT(*) FROM users WHERE status = 'pending'`).Scan(&pending)
	return
}

// Account Request Methods

// CreateAccountRequest creates a new account request.
func (d *DB) CreateAccountRequest(req *models.AccountRequest, passwordHash string) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if email already exists in users or pending requests
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ? COLLATE NOCASE`, req.Email).Scan(&count)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, fmt.Errorf("email already registered")
	}

	err = d.db.QueryRow(`SELECT COUNT(*) FROM account_requests WHERE email = ? COLLATE NOCASE AND status = 'pending'`, req.Email).Scan(&count)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, fmt.Errorf("account request already pending")
	}

	result, err := d.db.Exec(`
		INSERT INTO account_requests (email, password_hash, first_name, last_name, phone_number, reason, status)
		VALUES (?, ?, ?, ?, ?, ?, 'pending')`,
		req.Email, passwordHash, req.FirstName, req.LastName, req.PhoneNumber, req.Reason,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// GetAccountRequest retrieves an account request by ID.
func (d *DB) GetAccountRequest(id int64) (*models.AccountRequest, string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	req := &models.AccountRequest{}
	var passwordHash string
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number, reason, status,
			created_at, reviewed_by, reviewed_at, deny_reason
		FROM account_requests WHERE id = ?`, id).Scan(
		&req.ID, &req.Email, &passwordHash, &req.FirstName, &req.LastName, &req.PhoneNumber, &req.Reason, &req.Status,
		&req.CreatedAt, &req.ReviewedBy, &req.ReviewedAt, &req.DenyReason,
	)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return req, passwordHash, nil
}

// GetPendingAccountRequests retrieves all pending account requests.
func (d *DB) GetPendingAccountRequests() ([]*models.AccountRequest, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, email, first_name, last_name, phone_number, reason, status, created_at
		FROM account_requests WHERE status = 'pending' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var requests []*models.AccountRequest
	for rows.Next() {
		req := &models.AccountRequest{}
		err := rows.Scan(
			&req.ID, &req.Email, &req.FirstName, &req.LastName, &req.PhoneNumber, &req.Reason, &req.Status, &req.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, nil
}

// ApproveAccountRequest approves an account request and creates the user.
func (d *DB) ApproveAccountRequest(requestID int64, approverID string) (*models.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get the request
	req := &models.AccountRequest{}
	var passwordHash string
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number, status
		FROM account_requests WHERE id = ?`, requestID).Scan(
		&req.ID, &req.Email, &passwordHash, &req.FirstName, &req.LastName, &req.PhoneNumber, &req.Status,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("request not found")
	}
	if err != nil {
		return nil, err
	}

	if req.Status != "pending" {
		return nil, fmt.Errorf("request already processed")
	}

	// Check if email already exists
	var count int
	err = d.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ? COLLATE NOCASE`, req.Email).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, fmt.Errorf("email already registered")
	}

	// Start transaction
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Create the user
	now := time.Now()

	// Generate UUID for new user
	userID := generateUUID()

	_, err = tx.Exec(`
		INSERT INTO users (
			id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, created_at, updated_at,
			approved_by, approved_at
		) VALUES (?, ?, ?, ?, ?, ?, 'readonly', 'active', 0, ?, ?, ?, ?)`,
		userID, req.Email, passwordHash, req.FirstName, req.LastName, req.PhoneNumber,
		now, now, approverID, now,
	)
	if err != nil {
		return nil, err
	}

	// Update request status
	_, err = tx.Exec(`
		UPDATE account_requests SET status = 'approved', reviewed_by = ?, reviewed_at = ?
		WHERE id = ?`,
		approverID, now, requestID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Return the created user
	return &models.User{
		ID:          userID,
		Email:       req.Email,
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		PhoneNumber: req.PhoneNumber,
		Role:        models.RoleReadOnly,
		Status:      models.UserStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		ApprovedBy:  &approverID,
		ApprovedAt:  &now,
	}, nil
}

// DenyAccountRequest denies an account request.
func (d *DB) DenyAccountRequest(requestID int64, denierID string, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	result, err := d.db.Exec(`
		UPDATE account_requests SET status = 'denied', reviewed_by = ?, reviewed_at = ?, deny_reason = ?
		WHERE id = ? AND status = 'pending'`,
		denierID, now, reason, requestID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("request not found or already processed")
	}

	return nil
}

// Passkey Methods

// CreatePasskey creates a new passkey for a user.
func (d *DB) CreatePasskey(passkey *models.Passkey) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO passkeys (user_id, credential_id, public_key, name, aaguid, sign_count, backup_eligible, backup_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		passkey.UserID, passkey.CredentialID, passkey.PublicKey, passkey.Name, passkey.AAGUID, passkey.SignCount,
		passkey.BackupEligible, passkey.BackupState,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// GetPasskeyByCredentialID retrieves a passkey by credential ID.
func (d *DB) GetPasskeyByCredentialID(credentialID []byte) (*models.Passkey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	passkey := &models.Passkey{}
	err := d.db.QueryRow(`
		SELECT id, user_id, credential_id, public_key, name, aaguid, sign_count, backup_eligible, backup_state, created_at, last_used_at
		FROM passkeys WHERE credential_id = ?`, credentialID).Scan(
		&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey, &passkey.Name,
		&passkey.AAGUID, &passkey.SignCount, &passkey.BackupEligible, &passkey.BackupState, &passkey.CreatedAt, &passkey.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	passkey.CredentialIDHex = hex.EncodeToString(passkey.CredentialID)
	return passkey, nil
}

// GetUserPasskeys retrieves all passkeys for a user.
func (d *DB) GetUserPasskeys(userID string) ([]*models.Passkey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, user_id, credential_id, public_key, name, aaguid, sign_count, backup_eligible, backup_state, created_at, last_used_at
		FROM passkeys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var passkeys []*models.Passkey
	for rows.Next() {
		passkey := &models.Passkey{}
		err := rows.Scan(
			&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey, &passkey.Name,
			&passkey.AAGUID, &passkey.SignCount, &passkey.BackupEligible, &passkey.BackupState, &passkey.CreatedAt, &passkey.LastUsedAt,
		)
		if err != nil {
			return nil, err
		}
		passkey.CredentialIDHex = hex.EncodeToString(passkey.CredentialID)
		passkeys = append(passkeys, passkey)
	}
	return passkeys, nil
}

// UpdatePasskeySignCount updates the sign count after successful authentication.
func (d *DB) UpdatePasskeySignCount(id int64, signCount uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE passkeys SET sign_count = ?, last_used_at = ? WHERE id = ?`,
		signCount, time.Now(), id,
	)
	return err
}

// UpdatePasskeyAfterLogin updates the sign count and backup flags after successful authentication.
func (d *DB) UpdatePasskeyAfterLogin(id int64, signCount uint32, backupEligible, backupState bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE passkeys SET sign_count = ?, backup_eligible = ?, backup_state = ?, last_used_at = ? WHERE id = ?`,
		signCount, backupEligible, backupState, time.Now(), id,
	)
	return err
}

// DeletePasskey deletes a passkey.
func (d *DB) DeletePasskey(id int64, userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("passkey not found")
	}

	return nil
}

// SMTP Settings Methods

// GetSMTPSettings retrieves the SMTP settings.
func (d *DB) GetSMTPSettings() (*models.SMTPSettings, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	settings := &models.SMTPSettings{}
	err := d.db.QueryRow(`
		SELECT id, host, port, username, password, from_address, from_name,
			use_tls, use_starttls, enabled, updated_at, updated_by, test_email_sent_at
		FROM smtp_settings WHERE id = 1`).Scan(
		&settings.ID, &settings.Host, &settings.Port, &settings.Username, &settings.Password,
		&settings.FromAddress, &settings.FromName, &settings.UseTLS, &settings.UseStartTLS,
		&settings.Enabled, &settings.UpdatedAt, &settings.UpdatedBy, &settings.TestEmailSentAt,
	)
	if err == sql.ErrNoRows {
		return &models.SMTPSettings{ID: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	return settings, nil
}

// UpdateSMTPSettings updates the SMTP settings.
func (d *DB) UpdateSMTPSettings(settings *models.SMTPSettings, updaterID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE smtp_settings SET
			host = ?, port = ?, username = ?, password = ?, from_address = ?, from_name = ?,
			use_tls = ?, use_starttls = ?, enabled = ?, updated_at = ?, updated_by = ?
		WHERE id = 1`,
		settings.Host, settings.Port, settings.Username, settings.Password, settings.FromAddress, settings.FromName,
		settings.UseTLS, settings.UseStartTLS, settings.Enabled, time.Now(), updaterID,
	)
	return err
}

// UpdateSMTPTestEmailTime updates the test email sent timestamp.
func (d *DB) UpdateSMTPTestEmailTime() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE smtp_settings SET test_email_sent_at = ? WHERE id = 1`, time.Now())
	return err
}

// Audit Log Methods

// CreateAuditLog creates a new audit log entry.
func (d *DB) CreateAuditLog(log *models.AuditLog) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO audit_logs (user_id, action, details, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?)`,
		log.UserID, log.Action, log.Details, log.IPAddress, log.UserAgent,
	)
	return err
}

// GetRecentAuditLogs retrieves recent audit logs.
func (d *DB) GetRecentAuditLogs(limit int) ([]*models.AuditLog, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, user_id, action, details, ip_address, user_agent, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*models.AuditLog
	for rows.Next() {
		log := &models.AuditLog{}
		err := rows.Scan(
			&log.ID, &log.UserID, &log.Action, &log.Details, &log.IPAddress, &log.UserAgent, &log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// GetUserAuditLogs retrieves audit logs for a specific user.
func (d *DB) GetUserAuditLogs(userID int64, limit int) ([]*models.AuditLog, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, user_id, action, details, ip_address, user_agent, created_at
		FROM audit_logs WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*models.AuditLog
	for rows.Next() {
		log := &models.AuditLog{}
		err := rows.Scan(
			&log.ID, &log.UserID, &log.Action, &log.Details, &log.IPAddress, &log.UserAgent, &log.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// Helper to generate secure random tokens
func GenerateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EnsureAdminExists ensures the admin user exists, creating if necessary.
func (d *DB) EnsureAdminExists(passwordHash string, mustChangePassword bool) (*models.User, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if any admin user already exists (by role, not email)
	// This allows the admin to change their email without creating a duplicate
	admin := &models.User{}
	err := d.db.QueryRow(`
		SELECT id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, last_login_at,
			failed_login_attempts, locked_until, password_reset_token, password_reset_expires,
			created_at, updated_at, approved_by, approved_at
		FROM users WHERE role = 'admin' AND status = 'active' LIMIT 1`).Scan(
		&admin.ID, &admin.Email, &admin.PasswordHash, &admin.FirstName, &admin.LastName, &admin.PhoneNumber,
		&admin.Role, &admin.Status, &admin.MustChangePassword, &admin.PasswordChangedAt, &admin.LastLoginAt,
		&admin.FailedLoginAttempts, &admin.LockedUntil, &admin.PasswordResetToken, &admin.PasswordResetExpires,
		&admin.CreatedAt, &admin.UpdatedAt, &admin.ApprovedBy, &admin.ApprovedAt,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}

	if err != sql.ErrNoRows {
		// Admin exists
		return admin, false, nil
	}

	// Create admin user
	now := time.Now()

	// Generate UUID for admin user
	id := generateUUID()

	_, err = d.db.Exec(`
		INSERT INTO users (
			id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, created_at, updated_at
		) VALUES (?, 'admin', ?, 'System', 'Administrator', '', 'admin', 'active', ?, ?, ?, ?)`,
		id, passwordHash, mustChangePassword, now, now, now,
	)
	if err != nil {
		return nil, false, fmt.Errorf("failed to create admin user: %w", err)
	}

	admin = &models.User{
		ID:                 id,
		Email:              "admin",
		PasswordHash:       passwordHash,
		FirstName:          "System",
		LastName:           "Administrator",
		Role:               models.RoleAdmin,
		Status:             models.UserStatusActive,
		MustChangePassword: mustChangePassword,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	return admin, true, nil
}

// GetAdminPasswordHash returns the current admin password hash.
func (d *DB) GetAdminPasswordHash() (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var hash string
	err := d.db.QueryRow(`SELECT password_hash FROM users WHERE role = 'admin' AND status = 'active' LIMIT 1`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// ResetAdminPassword resets the admin password (updates all active admin users).
func (d *DB) ResetAdminPassword(passwordHash string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE users SET password_hash = ?, must_change_password = 1, password_changed_at = ?, updated_at = ?
		WHERE role = 'admin' AND status = 'active'`,
		passwordHash, now, now,
	)
	return err
}

// WebAuthn Session Methods

// WebAuthnSessionType indicates the type of WebAuthn session.
type WebAuthnSessionType string

const (
	WebAuthnSessionRegistration WebAuthnSessionType = "registration"
	WebAuthnSessionLogin        WebAuthnSessionType = "login"
)

// CreateWebAuthnSession stores a WebAuthn session for later retrieval.
func (d *DB) CreateWebAuthnSession(sessionID string, userID *string, sessionData []byte, sessionType WebAuthnSessionType, expiresAt time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO webauthn_sessions (id, user_id, session_data, session_type, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		sessionID, userID, sessionData, string(sessionType), expiresAt,
	)
	return err
}

// GetWebAuthnSession retrieves a WebAuthn session by ID.
func (d *DB) GetWebAuthnSession(sessionID string) ([]byte, *string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var sessionData []byte
	var userID *string
	var expiresAt time.Time

	err := d.db.QueryRow(`
		SELECT session_data, user_id, expires_at FROM webauthn_sessions WHERE id = ?`,
		sessionID).Scan(&sessionData, &userID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	// Check if session has expired
	if time.Now().After(expiresAt) {
		return nil, nil, nil
	}

	return sessionData, userID, nil
}

// DeleteWebAuthnSession removes a WebAuthn session.
func (d *DB) DeleteWebAuthnSession(sessionID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM webauthn_sessions WHERE id = ?`, sessionID)
	return err
}

// CleanupExpiredWebAuthnSessions removes expired WebAuthn sessions.
func (d *DB) CleanupExpiredWebAuthnSessions() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`DELETE FROM webauthn_sessions WHERE expires_at < ?`, time.Now())
	return err
}

// GetPasskeyByID retrieves a passkey by its ID.
func (d *DB) GetPasskeyByID(id int64) (*models.Passkey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	passkey := &models.Passkey{}
	err := d.db.QueryRow(`
		SELECT id, user_id, credential_id, public_key, name, aaguid, sign_count, backup_eligible, backup_state, created_at, last_used_at
		FROM passkeys WHERE id = ?`, id).Scan(
		&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey, &passkey.Name,
		&passkey.AAGUID, &passkey.SignCount, &passkey.BackupEligible, &passkey.BackupState, &passkey.CreatedAt, &passkey.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	passkey.CredentialIDHex = hex.EncodeToString(passkey.CredentialID)
	return passkey, nil
}

// GetUserByPasskeyCredentialID retrieves a user by their passkey credential ID.
func (d *DB) GetUserByPasskeyCredentialID(credentialID []byte) (*models.User, *models.Passkey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	passkey := &models.Passkey{}
	err := d.db.QueryRow(`
		SELECT id, user_id, credential_id, public_key, name, aaguid, sign_count, backup_eligible, backup_state, created_at, last_used_at
		FROM passkeys WHERE credential_id = ?`, credentialID).Scan(
		&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey, &passkey.Name,
		&passkey.AAGUID, &passkey.SignCount, &passkey.BackupEligible, &passkey.BackupState, &passkey.CreatedAt, &passkey.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	passkey.CredentialIDHex = hex.EncodeToString(passkey.CredentialID)

	// Get the user
	user, err := d.getUserByIDLocked(passkey.UserID)
	if err != nil {
		return nil, nil, err
	}

	return user, passkey, nil
}

// ============================================================================
// Session Token Management (OWASP 2026 Security)
// ============================================================================

// SetUserSessionToken sets the session token and metadata for a user.
// This is called on login to establish a server-side session validation token.
func (d *DB) SetUserSessionToken(userID, sessionToken, ip, userAgent string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE users 
		SET session_token = ?,
		    session_created_at = ?,
		    session_last_activity = ?,
		    session_ip = ?,
		    session_user_agent = ?
		WHERE id = ?`,
		sessionToken, now, now, ip, userAgent, userID)
	
	return err
}

// sessionActivityDebounce is the minimum interval between session_last_activity DB writes per user.
// This prevents write lock contention on the main mutex during rapid page refreshes.
const sessionActivityDebounce = 5 * time.Minute

// ValidateSessionToken checks if the provided session token matches the user's stored token.
// Returns true if valid, false otherwise. Updates last activity time in a debounced manner
// to avoid write lock contention that causes backend lockups during rapid requests.
func (d *DB) ValidateSessionToken(userID, sessionToken string) (bool, error) {
	// Use read lock for the validation check (most common path)
	d.mu.RLock()
	var storedToken sql.NullString
	err := d.db.QueryRow(`
		SELECT session_token
		FROM users
		WHERE id = ?`, userID).Scan(&storedToken)
	d.mu.RUnlock()

	if err != nil {
		return false, err
	}

	// No token set = not valid
	if !storedToken.Valid || storedToken.String == "" {
		return false, nil
	}

	// Token mismatch = not valid
	if storedToken.String != sessionToken {
		return false, nil
	}

	// Debounce session_last_activity updates to avoid write lock contention.
	// Go's sync.RWMutex is write-preferring: a pending Lock() blocks new RLock() callers.
	// Without debouncing, every auth check spawns a goroutine that calls Lock(), which
	// blocks all subsequent RLock() calls (including new auth checks), causing cascading lockups.
	d.updateSessionActivityDebounced(userID)

	return true, nil
}

// updateSessionActivityDebounced updates session_last_activity at most once per sessionActivityDebounce
// interval per user. This uses a separate lightweight mutex (not the main DB mutex) to check
// timing, and only acquires the main write lock when an actual DB write is needed.
func (d *DB) updateSessionActivityDebounced(userID string) {
	now := time.Now()

	d.sessionActivityMu.Lock()
	lastUpdate, exists := d.sessionActivityTimes[userID]
	if exists && now.Sub(lastUpdate) < sessionActivityDebounce {
		d.sessionActivityMu.Unlock()
		return // Skip — updated recently
	}
	d.sessionActivityTimes[userID] = now
	d.sessionActivityMu.Unlock()

	// Perform the actual DB write in a goroutine so it doesn't block the auth response.
	// This is safe because the debounce check above ensures at most one write per user
	// per interval, preventing goroutine pileup.
	go func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if _, err := d.db.Exec(`
			UPDATE users
			SET session_last_activity = ?
			WHERE id = ?`, now, userID); err != nil {
			d.logger.Warn("failed to update session activity", "userID", userID, "error", err)
		}
	}()
}

// ClearUserSessionToken invalidates a user's session token.
// Called on logout or password change to force re-authentication.
func (d *DB) ClearUserSessionToken(userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean up the debounce tracking entry for this user
	d.sessionActivityMu.Lock()
	delete(d.sessionActivityTimes, userID)
	d.sessionActivityMu.Unlock()

	_, err := d.db.Exec(`
		UPDATE users
		SET session_token = NULL,
		    session_created_at = NULL,
		    session_last_activity = NULL,
		    session_ip = NULL,
		    session_user_agent = NULL
		WHERE id = ?`, userID)

	return err
}

// ClearAllUserSessions invalidates all sessions for a user.
// Used for "logout all devices" functionality.
func (d *DB) ClearAllUserSessions(userID string) error {
	return d.ClearUserSessionToken(userID)
}

// GetUserSessionInfo retrieves session metadata for security auditing.
func (d *DB) GetUserSessionInfo(userID string) (sessionIP, sessionUserAgent string, sessionCreatedAt, sessionLastActivity *time.Time, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	err = d.db.QueryRow(`
		SELECT session_ip, session_user_agent, session_created_at, session_last_activity
		FROM users 
		WHERE id = ?`, userID).Scan(&sessionIP, &sessionUserAgent, &sessionCreatedAt, &sessionLastActivity)
	
	return
}
