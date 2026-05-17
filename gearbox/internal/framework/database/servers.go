package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// BoxDB represents a monitored box configuration in the database.
type BoxDB struct {
	ID              int64
	BoxID           string
	Name            string
	Location        string
	Notes           string
	AgentURL        string
	APIKeyEncrypted []byte
	Enabled         bool
	AutoDiscovery   bool
	SkipTLSVerify   bool
	// ConsoleEnabled is the per-box opt-in for the remote console
	// feature (see #89). Default 0 — operator flips it on per box
	// from the box settings UI. This is the sole gate on the
	// dashboard's console proxy path; flipping it off revokes
	// access immediately.
	ConsoleEnabled bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      *string // UUID
}

// UsesAgentAPI returns true if this box has valid Agent API configuration.
// All boxes use Agent API - this validates the configuration is complete.
func (b *BoxDB) UsesAgentAPI() bool {
	return b.AgentURL != "" && len(b.APIKeyEncrypted) > 0
}

// CreateBox inserts a new box configuration into the database.
func (d *DB) CreateBox(box *BoxDB) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		INSERT INTO boxes (
			box_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, skip_tls_verify, console_enabled, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`

	result, err := d.db.Exec(query,
		box.BoxID,
		box.Name,
		box.Location,
		box.Notes,
		box.AgentURL,
		box.APIKeyEncrypted,
		box.Enabled,
		box.AutoDiscovery,
		box.SkipTLSVerify,
		box.ConsoleEnabled,
		box.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert box: %w", err)
	}

	boxID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	box.ID = boxID

	return nil
}

// GetBoxes retrieves all box configurations from the database.
func (d *DB) GetBoxes() ([]*BoxDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, box_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, skip_tls_verify, console_enabled, created_at, updated_at, created_by
		FROM boxes
		ORDER BY name ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query boxes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boxes []*BoxDB
	for rows.Next() {
		box := &BoxDB{}
		err := rows.Scan(
			&box.ID,
			&box.BoxID,
			&box.Name,
			&box.Location,
			&box.Notes,
			&box.AgentURL,
			&box.APIKeyEncrypted,
			&box.Enabled,
			&box.AutoDiscovery,
			&box.SkipTLSVerify,
			&box.ConsoleEnabled,
			&box.CreatedAt,
			&box.UpdatedAt,
			&box.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan box: %w", err)
		}

		boxes = append(boxes, box)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating boxes: %w", err)
	}

	return boxes, nil
}

// GetEnabledBoxes retrieves only enabled box configurations.
func (d *DB) GetEnabledBoxes() ([]*BoxDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, box_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, skip_tls_verify, console_enabled, created_at, updated_at, created_by
		FROM boxes
		WHERE enabled = 1
		ORDER BY name ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled boxes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boxes []*BoxDB
	for rows.Next() {
		box := &BoxDB{}
		err := rows.Scan(
			&box.ID,
			&box.BoxID,
			&box.Name,
			&box.Location,
			&box.Notes,
			&box.AgentURL,
			&box.APIKeyEncrypted,
			&box.Enabled,
			&box.AutoDiscovery,
			&box.SkipTLSVerify,
			&box.ConsoleEnabled,
			&box.CreatedAt,
			&box.UpdatedAt,
			&box.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan box: %w", err)
		}

		boxes = append(boxes, box)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled boxes: %w", err)
	}

	return boxes, nil
}

// GetBoxByID retrieves a box by its database ID.
func (d *DB) GetBoxByID(id int64) (*BoxDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, box_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, skip_tls_verify, console_enabled, created_at, updated_at, created_by
		FROM boxes
		WHERE id = ?
	`

	box := &BoxDB{}
	err := d.db.QueryRow(query, id).Scan(
		&box.ID,
		&box.BoxID,
		&box.Name,
		&box.Location,
		&box.Notes,
		&box.AgentURL,
		&box.APIKeyEncrypted,
		&box.Enabled,
		&box.AutoDiscovery,
		&box.SkipTLSVerify,
		&box.ConsoleEnabled,
		&box.CreatedAt,
		&box.UpdatedAt,
		&box.CreatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get box: %w", err)
	}

	return box, nil
}

// GetBoxByBoxID retrieves a box by its box_id field.
func (d *DB) GetBoxByBoxID(boxID string) (*BoxDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, box_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, skip_tls_verify, console_enabled, created_at, updated_at, created_by
		FROM boxes
		WHERE box_id = ?
	`

	box := &BoxDB{}
	err := d.db.QueryRow(query, boxID).Scan(
		&box.ID,
		&box.BoxID,
		&box.Name,
		&box.Location,
		&box.Notes,
		&box.AgentURL,
		&box.APIKeyEncrypted,
		&box.Enabled,
		&box.AutoDiscovery,
		&box.SkipTLSVerify,
		&box.ConsoleEnabled,
		&box.CreatedAt,
		&box.UpdatedAt,
		&box.CreatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get box: %w", err)
	}

	return box, nil
}

// UpdateBox updates an existing box configuration.
func (d *DB) UpdateBox(box *BoxDB) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		UPDATE boxes SET
			box_id = ?,
			name = ?,
			location = ?,
			notes = ?,
			agent_url = ?,
			api_key_encrypted = ?,
			enabled = ?,
			auto_discovery = ?,
			skip_tls_verify = ?,
			console_enabled = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := d.db.Exec(query,
		box.BoxID,
		box.Name,
		box.Location,
		box.Notes,
		box.AgentURL,
		box.APIKeyEncrypted,
		box.Enabled,
		box.AutoDiscovery,
		box.SkipTLSVerify,
		box.ConsoleEnabled,
		box.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update box: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("box not found")
	}

	return nil
}

// DeleteBox deletes a box configuration from the database, along with
// any rotation-keyring entries that reference it.
//
// The schema declares ON DELETE CASCADE on box_agent_keys.box_id, but
// this codebase doesn't set PRAGMA foreign_keys=ON (enabling it is a
// broader change that risks regressing on legacy rows elsewhere). To
// avoid leaving orphaned rows that hold encrypted secrets, the
// dependent table is wiped explicitly inside the same transaction.
func (d *DB) DeleteBox(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM box_agent_keys WHERE box_id = ?`, id); err != nil {
		return fmt.Errorf("delete dependent keys: %w", err)
	}

	result, err := tx.Exec(`DELETE FROM boxes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete box: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("box not found")
	}

	return tx.Commit()
}

// SetBoxEnabled enables or disables a box.
func (d *DB) SetBoxEnabled(id int64, enabled bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		UPDATE boxes
		SET enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := d.db.Exec(query, enabled, id)
	if err != nil {
		return fmt.Errorf("failed to update box enabled status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("box not found")
	}

	return nil
}

// CountBoxes returns the total count of boxes.
func (d *DB) CountBoxes() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM boxes").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count boxes: %w", err)
	}
	return count, nil
}

// CountEnabledBoxes returns the count of enabled boxes.
func (d *DB) CountEnabledBoxes() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM boxes WHERE enabled = 1").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count enabled boxes: %w", err)
	}
	return count, nil
}

// ToBoxConfig converts a BoxDB to a models.BoxConfig.
// This requires decryption of the API key, which should be done by the caller.
func (b *BoxDB) ToBoxConfig(apiKey string) models.BoxConfig {
	return models.BoxConfig{
		ID:            b.BoxID,
		Name:          b.Name,
		AgentURL:      b.AgentURL,
		APIKey:        apiKey,
		SkipTLSVerify: b.SkipTLSVerify,
	}
}
