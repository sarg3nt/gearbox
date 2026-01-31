package database

import (
	"database/sql"
	"fmt"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// initPermissionsSchema creates the permissions-related tables.
func (d *DB) initPermissionsSchema() error {
	schema := `
	-- Permission grants: track individual permissions granted to users
	CREATE TABLE IF NOT EXISTS permission_grants (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		component TEXT NOT NULL,
		permission TEXT NOT NULL,
		granted_by INTEGER NOT NULL,
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		server_id TEXT DEFAULT '',
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE CASCADE,
		UNIQUE(user_id, component, permission, server_id)
	);

	CREATE INDEX IF NOT EXISTS idx_permission_grants_user
		ON permission_grants(user_id);
	CREATE INDEX IF NOT EXISTS idx_permission_grants_component
		ON permission_grants(component);
	`

	_, err := d.db.Exec(schema)
	return err
}

// GrantPermission grants a permission to a user for a component.
func (d *DB) GrantPermission(userID int64, component models.Component, permission models.Permission, grantedBy int64, serverID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Convert nil serverID to empty string for consistency with schema
	serverIDStr := ""
	if serverID != nil {
		serverIDStr = *serverID
	}

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO permission_grants (user_id, component, permission, granted_by, server_id)
		VALUES (?, ?, ?, ?, ?)`,
		userID, component, permission, grantedBy, serverIDStr,
	)
	return err
}

// RevokePermission removes a permission from a user for a component.
func (d *DB) RevokePermission(userID int64, component models.Component, permission models.Permission, serverID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Convert nil serverID to empty string for consistency with schema
	serverIDStr := ""
	if serverID != nil {
		serverIDStr = *serverID
	}

	_, err := d.db.Exec(`
		DELETE FROM permission_grants
		WHERE user_id = ? AND component = ? AND permission = ? AND server_id = ?`,
		userID, component, permission, serverIDStr,
	)
	return err
}

// GetUserPermissions retrieves all permissions for a user.
func (d *DB) GetUserPermissions(userID string) (*models.UserPermissions, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get user to check if admin
	user, err := d.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Admins have all permissions
	if user.IsAdmin() {
		return &models.UserPermissions{
			UserID:      userID,
			IsAdmin:     true,
			Permissions: make(map[models.Component][]models.Permission),
		}, nil
	}

	// Query user's granted permissions
	rows, err := d.db.Query(`
		SELECT component, permission
		FROM permission_grants
		WHERE user_id = ?
		ORDER BY component, permission`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms := &models.UserPermissions{
		UserID:      userID,
		IsAdmin:     false,
		Permissions: make(map[models.Component][]models.Permission),
	}

	for rows.Next() {
		var component models.Component
		var permission models.Permission

		if err := rows.Scan(&component, &permission); err != nil {
			return nil, err
		}

		perms.Permissions[component] = append(perms.Permissions[component], permission)
	}

	return perms, nil
}

// GetUserPermissionGrants retrieves all permission grants for a user with metadata.
func (d *DB) GetUserPermissionGrants(userID int64) ([]models.PermissionGrant, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, user_id, component, permission, granted_by, granted_at, server_id
		FROM permission_grants
		WHERE user_id = ?
		ORDER BY component, permission`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grants []models.PermissionGrant
	for rows.Next() {
		var g models.PermissionGrant
		var serverID sql.NullString

		err := rows.Scan(&g.ID, &g.UserID, &g.Component, &g.Permission, &g.GrantedBy, &g.GrantedAt, &serverID)
		if err != nil {
			return nil, err
		}

		if serverID.Valid {
			g.ServerID = &serverID.String
		}

		grants = append(grants, g)
	}

	return grants, nil
}

// SetUserPermissions sets all permissions for a user (replaces existing).
func (d *DB) SetUserPermissions(userID string, permissions map[models.Component][]models.Permission, grantedBy string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete existing permissions
	_, err = tx.Exec(`DELETE FROM permission_grants WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}

	// Insert new permissions
	stmt, err := tx.Prepare(`
		INSERT INTO permission_grants (user_id, component, permission, granted_by)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for component, perms := range permissions {
		for _, perm := range perms {
			_, err := stmt.Exec(userID, component, perm, grantedBy)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// CopyPermissionsFromTemplate applies a permission template to a user.
func (d *DB) CopyPermissionsFromTemplate(userID string, templateName string, grantedBy string) error {
	templates := models.GetPermissionTemplates()

	var template *models.PermissionTemplate
	for _, t := range templates {
		if t.Name == templateName {
			template = &t
			break
		}
	}

	if template == nil {
		return fmt.Errorf("template not found: %s", templateName)
	}

	return d.SetUserPermissions(userID, template.Permissions, grantedBy)
}

// GetAllUserPermissions retrieves permissions for all users (for admin management).
func (d *DB) GetAllUserPermissions() (map[string]*models.UserPermissions, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get all users
	users, err := d.GetAllUsers()
	if err != nil {
		return nil, err
	}

	result := make(map[string]*models.UserPermissions)

	// Get permissions for each user
	for _, user := range users {
		perms, err := d.GetUserPermissions(user.ID)
		if err != nil {
			d.logger.Warn("failed to get permissions for user", "user_id", user.ID, "error", err)
			continue
		}
		result[user.ID] = perms
	}

	return result, nil
}

// HasPermission checks if a user has a specific permission for a component.
func (d *DB) HasPermission(userID string, component models.Component, permission models.Permission) (bool, error) {
	perms, err := d.GetUserPermissions(userID)
	if err != nil {
		return false, err
	}

	return perms.HasPermission(component, permission), nil
}

// GetUsersWithPermission returns all users who have a specific permission.
func (d *DB) GetUsersWithPermission(component models.Component, permission models.Permission) ([]models.User, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get all admins (they have all permissions)
	admins, err := d.db.Query(`
		SELECT id, email, password_hash, first_name, last_name, phone_number, role, status,
			must_change_password, password_changed_at, last_login_at, failed_login_attempts,
			locked_until, password_reset_token, password_reset_expires, created_at, updated_at,
			approved_by, approved_at
		FROM users
		WHERE role = ? AND status = ?`,
		models.RoleAdmin, models.UserStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer admins.Close()

	var users []models.User
	for admins.Next() {
		var u models.User
		err := scanUser(admins, &u)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	// Get users with explicit permission grant
	rows, err := d.db.Query(`
		SELECT DISTINCT u.id, u.email, u.password_hash, u.first_name, u.last_name, u.phone_number,
			u.role, u.status, u.must_change_password, u.password_changed_at, u.last_login_at,
			u.failed_login_attempts, u.locked_until, u.password_reset_token, u.password_reset_expires,
			u.created_at, u.updated_at, u.approved_by, u.approved_at
		FROM users u
		INNER JOIN permission_grants pg ON u.id = pg.user_id
		WHERE pg.component = ? AND pg.permission = ? AND u.status = ?`,
		component, permission, models.UserStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var u models.User
		err := scanUser(rows, &u)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	return users, nil
}

// Helper function to scan user from rows (avoiding duplication).
func scanUser(rows interface{ Scan(...interface{}) error }, u *models.User) error {
	var passwordChangedAt, lastLoginAt, lockedUntil, approvedAt sql.NullTime
	var passwordResetToken, passwordResetExpires, phoneNumber, approvedBy sql.NullString

	err := rows.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &phoneNumber,
		&u.Role, &u.Status, &u.MustChangePassword, &passwordChangedAt, &lastLoginAt,
		&u.FailedLoginAttempts, &lockedUntil, &passwordResetToken, &passwordResetExpires,
		&u.CreatedAt, &u.UpdatedAt, &approvedBy, &approvedAt,
	)
	if err != nil {
		return err
	}

	if phoneNumber.Valid {
		u.PhoneNumber = phoneNumber.String
	}
	if passwordChangedAt.Valid {
		u.PasswordChangedAt = &passwordChangedAt.Time
	}
	if lastLoginAt.Valid {
		u.LastLoginAt = &lastLoginAt.Time
	}
	if lockedUntil.Valid {
		u.LockedUntil = &lockedUntil.Time
	}
	if passwordResetToken.Valid {
		u.PasswordResetToken = &passwordResetToken.String
	}
	if approvedBy.Valid {
		u.ApprovedBy = &approvedBy.String
	}
	if approvedAt.Valid {
		u.ApprovedAt = &approvedAt.Time
	}

	return nil
}
