package database

import (
	"database/sql"
	"fmt"
	"time"
)

// BoxAgentKey is one accepted API key for a box. The N-row keyring
// replaces the single-key model (issue #72): the controller installs a
// new key as secondary, flips it to primary, then removes the old one
// after an overlap window long enough to survive a partial rotation.
//
// The secret is encrypted-at-rest with the dashboard's envelope
// encryptor; callers decrypt only when they need to make an outbound
// request to the agent.
type BoxAgentKey struct {
	ID              int64
	BoxID           int64
	KID             string
	SecretEncrypted []byte
	Role            string // "primary" or "secondary"
	CreatedAt       time.Time
	RetiredAt       sql.NullTime
	LastUsedAt      sql.NullTime
}

// GetBoxAgentKeys returns every entry for boxID, primary first.
func (d *DB) GetBoxAgentKeys(boxID int64) ([]*BoxAgentKey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	const q = `
		SELECT id, box_id, kid, secret_encrypted, role,
		       created_at, retired_at, last_used_at
		FROM box_agent_keys
		WHERE box_id = ?
		ORDER BY CASE role WHEN 'primary' THEN 0 ELSE 1 END, created_at ASC
	`
	rows, err := d.db.Query(q, boxID)
	if err != nil {
		return nil, fmt.Errorf("query box_agent_keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*BoxAgentKey
	for rows.Next() {
		k := &BoxAgentKey{}
		if err := rows.Scan(
			&k.ID, &k.BoxID, &k.KID, &k.SecretEncrypted, &k.Role,
			&k.CreatedAt, &k.RetiredAt, &k.LastUsedAt,
		); err != nil {
			return nil, fmt.Errorf("scan box_agent_key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetBoxPrimaryKey returns the box's primary key entry, or nil if none.
// Use this to pick which encrypted secret to decrypt for outbound
// requests.
func (d *DB) GetBoxPrimaryKey(boxID int64) (*BoxAgentKey, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	const q = `
		SELECT id, box_id, kid, secret_encrypted, role,
		       created_at, retired_at, last_used_at
		FROM box_agent_keys
		WHERE box_id = ? AND role = 'primary'
		LIMIT 1
	`
	k := &BoxAgentKey{}
	err := d.db.QueryRow(q, boxID).Scan(
		&k.ID, &k.BoxID, &k.KID, &k.SecretEncrypted, &k.Role,
		&k.CreatedAt, &k.RetiredAt, &k.LastUsedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query primary key: %w", err)
	}
	return k, nil
}

// InsertBoxAgentKey adds a new entry. Phase 2's rotator uses this
// to install the second key during the overlap window. Caller must set
// BoxID, KID, SecretEncrypted, Role; CreatedAt is set automatically.
func (d *DB) InsertBoxAgentKey(k *BoxAgentKey) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	const q = `
		INSERT INTO box_agent_keys (box_id, kid, secret_encrypted, role)
		VALUES (?, ?, ?, ?)
	`
	res, err := d.db.Exec(q, k.BoxID, k.KID, k.SecretEncrypted, k.Role)
	if err != nil {
		return fmt.Errorf("insert box_agent_key: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	k.ID = id
	return nil
}

// SetBoxPrimaryKey flips the named entry to primary and demotes any
// existing primary to secondary in a single transaction.
func (d *DB) SetBoxPrimaryKey(boxID int64, kid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Demote current primary.
	if _, err := tx.Exec(`
		UPDATE box_agent_keys SET role = 'secondary', retired_at = CURRENT_TIMESTAMP
		WHERE box_id = ? AND role = 'primary'
	`, boxID); err != nil {
		return fmt.Errorf("demote primary: %w", err)
	}

	// Promote target.
	res, err := tx.Exec(`
		UPDATE box_agent_keys SET role = 'primary', retired_at = NULL
		WHERE box_id = ? AND kid = ?
	`, boxID, kid)
	if err != nil {
		return fmt.Errorf("promote key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("key with kid=%q not found for box %d", kid, boxID)
	}

	return tx.Commit()
}

// DeleteBoxAgentKey removes the named entry. Refuses to delete the
// only remaining row — symmetric with agent-side keyring guard.
func (d *DB) DeleteBoxAgentKey(boxID int64, kid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM box_agent_keys WHERE box_id = ?`, boxID).Scan(&count); err != nil {
		return fmt.Errorf("count keys: %w", err)
	}
	if count <= 1 {
		return fmt.Errorf("cannot delete the only remaining key for box %d", boxID)
	}

	res, err := d.db.Exec(`DELETE FROM box_agent_keys WHERE box_id = ? AND kid = ?`, boxID, kid)
	if err != nil {
		return fmt.Errorf("delete key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("key with kid=%q not found for box %d", kid, boxID)
	}
	return nil
}

// TouchBoxAgentKeyLastUsed records last_used_at on the named entry.
// Best-effort; errors are logged by the caller but never block requests.
func (d *DB) TouchBoxAgentKeyLastUsed(boxID int64, kid string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.db.Exec(`
		UPDATE box_agent_keys SET last_used_at = CURRENT_TIMESTAMP
		WHERE box_id = ? AND kid = ?
	`, boxID, kid); err != nil {
		return fmt.Errorf("touch last_used_at: %w", err)
	}
	return nil
}
