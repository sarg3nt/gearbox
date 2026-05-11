package database

import (
	"fmt"
)

// LogSourceSetting represents an enabled log source for a monitored box.
type LogSourceSetting struct {
	ID          int64
	HAProxyID   int64  // FK to boxes.id (field name retained for legacy callers)
	LogName     string // e.g., "haproxy", "system", "fail2ban"
	DisplayName string // User-friendly display name
}

// GetEnabledLogSources returns the list of enabled log source names for a server.
// If no explicit settings exist, returns nil (caller should use defaults).
func (d *DB) GetEnabledLogSources(haproxyID int64) ([]LogSourceSetting, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, box_id, log_name, display_name
		FROM log_source_settings
		WHERE box_id = ?
		ORDER BY display_name ASC
	`

	rows, err := d.db.Query(query, haproxyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query log sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sources []LogSourceSetting
	for rows.Next() {
		var s LogSourceSetting
		if err := rows.Scan(&s.ID, &s.HAProxyID, &s.LogName, &s.DisplayName); err != nil {
			return nil, fmt.Errorf("failed to scan log source: %w", err)
		}
		sources = append(sources, s)
	}

	return sources, rows.Err()
}

// GetEnabledLogSourcesByServerID returns enabled log sources for a server by its server_id.
// Returns empty slice (not error) if the server has no HAProxy configuration or no log sources.
func (d *DB) GetEnabledLogSourcesByServerID(serverID string) ([]LogSourceSetting, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// INNER JOIN to boxes ensures the box exists; zero rows if it doesn't or has no log sources.
	query := `
		SELECT ls.id, ls.box_id, ls.log_name, ls.display_name
		FROM log_source_settings ls
		JOIN boxes b ON ls.box_id = b.id
		WHERE b.box_id = ?
		ORDER BY ls.display_name ASC
	`

	rows, err := d.db.Query(query, serverID)
	if err != nil {
		d.logger.Warn("failed to query log sources by server ID", "serverID", serverID, "error", err)
		return nil, nil
	}
	defer func() { _ = rows.Close() }()

	var sources []LogSourceSetting
	for rows.Next() {
		var s LogSourceSetting
		if err := rows.Scan(&s.ID, &s.HAProxyID, &s.LogName, &s.DisplayName); err != nil {
			return nil, fmt.Errorf("failed to scan log source: %w", err)
		}
		sources = append(sources, s)
	}

	return sources, rows.Err()
}

// HasLogSourceSettings checks if a server has any explicit log source settings.
func (d *DB) HasLogSourceSettings(haproxyID int64) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(*) FROM log_source_settings WHERE box_id = ?",
		haproxyID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check log source settings: %w", err)
	}

	return count > 0, nil
}

// SetEnabledLogSourcesByServerID replaces all enabled log sources for a server by its server_id.
func (d *DB) SetEnabledLogSourcesByServerID(serverID string, sources []LogSourceSettingInput) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// First, look up the boxes.id row for this box_id string.
	var haproxyID int64
	err := d.db.QueryRow("SELECT id FROM boxes WHERE box_id = ?", serverID).Scan(&haproxyID)
	if err != nil {
		return fmt.Errorf("failed to find server with ID %s: %w", serverID, err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete existing settings
	if _, err := tx.Exec("DELETE FROM log_source_settings WHERE box_id = ?", haproxyID); err != nil {
		return fmt.Errorf("failed to delete existing settings: %w", err)
	}

	// Insert new settings
	if len(sources) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO log_source_settings (box_id, log_name, display_name)
			VALUES (?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare insert statement: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for _, source := range sources {
			if _, err := stmt.Exec(haproxyID, source.LogName, source.DisplayName); err != nil {
				return fmt.Errorf("failed to insert log source %s: %w", source.LogName, err)
			}
		}
	}

	return tx.Commit()
}

// LogSourceSettingInput is used for creating/updating log source settings.
type LogSourceSettingInput struct {
	LogName     string
	DisplayName string
}

// SetEnabledLogSources replaces all enabled log sources for a server.
// This is an all-or-nothing operation for simplicity.
func (d *DB) SetEnabledLogSources(haproxyID int64, sources []LogSourceSetting) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete existing settings
	if _, err := tx.Exec("DELETE FROM log_source_settings WHERE box_id = ?", haproxyID); err != nil {
		return fmt.Errorf("failed to delete existing settings: %w", err)
	}

	// Insert new settings
	if len(sources) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO log_source_settings (box_id, log_name, display_name)
			VALUES (?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare insert statement: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for _, source := range sources {
			if _, err := stmt.Exec(haproxyID, source.LogName, source.DisplayName); err != nil {
				return fmt.Errorf("failed to insert log source %s: %w", source.LogName, err)
			}
		}
	}

	return tx.Commit()
}

// AddLogSource adds a single log source to a server's enabled list.
func (d *DB) AddLogSource(haproxyID int64, logName, displayName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT OR IGNORE INTO log_source_settings (box_id, log_name, display_name)
		VALUES (?, ?, ?)
	`, haproxyID, logName, displayName)
	if err != nil {
		return fmt.Errorf("failed to add log source: %w", err)
	}

	return nil
}

// RemoveLogSource removes a single log source from a server's enabled list.
func (d *DB) RemoveLogSource(haproxyID int64, logName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		DELETE FROM log_source_settings
		WHERE box_id = ? AND log_name = ?
	`, haproxyID, logName)
	if err != nil {
		return fmt.Errorf("failed to remove log source: %w", err)
	}

	return nil
}
