//go:build dev

// Dev-only seed helper for the loopback auto-login bypass (issue #83).
// Compiled in only when the binary is built with `-tags dev`. Production
// binaries do not contain this function at all.

package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// EnsureDevUserExists creates the seeded `dev` user used by the dev
// auto-login bypass if it doesn't already exist. The password hash is
// expected to be the package-level dummyPasswordHash from auth, so the
// form-login path can never authenticate as this user — only the
// loopback bypass can.
//
// Returns (created, error) — `created` is true only on the first call
// that actually inserted a row.
func (d *DB) EnsureDevUserExists(email, passwordHash string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var existingID string
	err := d.db.QueryRow(`SELECT id FROM users WHERE email = ? LIMIT 1`, email).Scan(&existingID)
	if err == nil {
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}

	now := time.Now()
	id := generateUUID()
	_, err = d.db.Exec(`
		INSERT INTO users (
			id, email, password_hash, first_name, last_name, phone_number,
			role, status, must_change_password, password_changed_at, created_at, updated_at
		) VALUES (?, ?, ?, 'Dev', 'User', '', ?, ?, 0, ?, ?, ?)`,
		id, email, passwordHash, models.RoleAdmin, models.UserStatusActive, now, now, now,
	)
	if err != nil {
		return false, fmt.Errorf("failed to create dev user: %w", err)
	}
	return true, nil
}
