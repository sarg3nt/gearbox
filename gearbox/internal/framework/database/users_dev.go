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

// EnsureDevUserExists ensures the seeded `dev` user exists for the dev
// auto-login bypass AND that its password hash is the caller-supplied
// `passwordHash` (the package-level dummyPasswordHash from auth). The
// hash is rewritten unconditionally on every call so a row left over
// from a prior manual setup — where a developer may have set a real
// bcrypt-able password for testing — cannot be authenticated via the
// form-login path. Only the loopback bypass can authenticate as `dev`.
//
// Returns (created, error) — `created` is true only on the call that
// inserted the row; subsequent calls return false but still rewrite the
// hash, status, and must_change_password fields back to the safe
// defaults. Other fields (role, names) are not overwritten so a
// developer can still adjust the dev user's display info without losing
// it on next startup.
func (d *DB) EnsureDevUserExists(email, passwordHash string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var existingID string
	err := d.db.QueryRow(`SELECT id FROM users WHERE email = ? LIMIT 1`, email).Scan(&existingID)
	switch {
	case err == nil:
		// Row exists. Force the hash + status + must_change_password back to
		// the safe defaults so the form-login path remains unable to
		// authenticate as this user even if the row was tampered with.
		_, err := d.db.Exec(`
			UPDATE users
			SET password_hash = ?,
			    status = ?,
			    must_change_password = 0,
			    updated_at = ?
			WHERE id = ?`,
			passwordHash, models.UserStatusActive, time.Now(), existingID,
		)
		if err != nil {
			return false, fmt.Errorf("failed to reset dev user safety fields: %w", err)
		}
		return false, nil
	case err != sql.ErrNoRows:
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
