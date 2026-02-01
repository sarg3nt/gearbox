package database

import (
	"database/sql"
	"fmt"
	"time"
)

// ConfigType represents the type of configuration (haproxy or firewall).
type ConfigType string

const (
	ConfigTypeHAProxy   ConfigType = "haproxy"
	ConfigTypeFirewall  ConfigType = "firewall"
)

// ChangeType represents the type of configuration change.
type ChangeType string

const (
	ChangeTypeManual   ChangeType = "manual"
	ChangeTypeGitSync  ChangeType = "git_sync"
	ChangeTypeRestore  ChangeType = "restore"
)

// BoxGitConfig stores git configuration for a monitored box.
type BoxGitConfig struct {
	ID                  int64      `json:"id"`
	HAProxyBoxID        int64      `json:"haproxy_box_id"`
	ConfigType          ConfigType `json:"config_type"`
	GitRepoURL          string     `json:"git_repo_url"`
	GitBranch           string     `json:"git_branch"`
	GitFilePath         string     `json:"git_file_path"`
	GitPATEncrypted     []byte     `json:"-"` // Never expose in JSON
	AutoApplyEnabled    bool       `json:"auto_apply_enabled"`
	SyncIntervalMinutes int        `json:"sync_interval_minutes"`
	LastSyncAt          *time.Time `json:"last_sync_at,omitempty"`
	LastSyncCommit      string     `json:"last_sync_commit,omitempty"`
	LastSyncStatus      string     `json:"last_sync_status,omitempty"`
	LastSyncError       string     `json:"last_sync_error,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ConfigChange represents a configuration change audit record.
type ConfigChange struct {
	ID           int64      `json:"id"`
	HAProxyBoxID int64      `json:"haproxy_box_id"`
	ConfigType   ConfigType `json:"config_type"`
	ChangeType   ChangeType `json:"change_type"`
	PreviousSHA256 string     `json:"previous_sha256,omitempty"`
	NewSHA256      string     `json:"new_sha256,omitempty"`
	ChangedBy      *string    `json:"changed_by,omitempty"` // UUID
	ChangedByName  string     `json:"changed_by_name,omitempty"` // Populated on read
	ChangeReason   string     `json:"change_reason,omitempty"`
	GitCommitSHA   string     `json:"git_commit_sha,omitempty"`
	BackupPath     string     `json:"backup_path,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// initConfigSchema creates the configuration management tables.
func (d *DB) initConfigSchema() error {
	schema := `
	-- Git configuration per monitored box
	CREATE TABLE IF NOT EXISTS box_git_config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		haproxy_box_id INTEGER NOT NULL,
		config_type TEXT NOT NULL,
		git_repo_url TEXT,
		git_branch TEXT NOT NULL DEFAULT 'main',
		git_file_path TEXT,
		git_pat_encrypted BLOB,
		auto_apply_enabled INTEGER NOT NULL DEFAULT 0,
		sync_interval_minutes INTEGER NOT NULL DEFAULT 60,
		last_sync_at DATETIME,
		last_sync_commit TEXT,
		last_sync_status TEXT,
		last_sync_error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (haproxy_box_id) REFERENCES boxes(id) ON DELETE CASCADE,
		UNIQUE(haproxy_box_id, config_type)
	);

	CREATE INDEX IF NOT EXISTS idx_box_git_config_box
		ON box_git_config(haproxy_box_id);

	-- Config change history (audit trail)
	CREATE TABLE IF NOT EXISTS config_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		haproxy_box_id INTEGER NOT NULL,
		config_type TEXT NOT NULL,
		change_type TEXT NOT NULL,
		previous_sha256 TEXT,
		new_sha256 TEXT,
		changed_by INTEGER,
		change_reason TEXT,
		git_commit_sha TEXT,
		backup_path TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (haproxy_box_id) REFERENCES boxes(id) ON DELETE CASCADE,
		FOREIGN KEY (changed_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_config_changes_box_type
		ON config_changes(haproxy_box_id, config_type, created_at);
	`

	_, err := d.db.Exec(schema)
	return err
}

// GetBoxGitConfig retrieves the git configuration for a box and config type.
func (d *DB) GetBoxGitConfig(haproxyBoxID int64, configType ConfigType) (*BoxGitConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var cfg BoxGitConfig
	var lastSyncAt sql.NullString
	var lastSyncCommit, lastSyncStatus, lastSyncError sql.NullString
	var gitRepoURL, gitFilePath sql.NullString

	err := d.db.QueryRow(`
		SELECT id, haproxy_box_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes,
			last_sync_at, last_sync_commit, last_sync_status, last_sync_error,
			created_at, updated_at
		FROM box_git_config
		WHERE haproxy_box_id = ? AND config_type = ?`,
		haproxyBoxID, configType,
	).Scan(
		&cfg.ID, &cfg.HAProxyBoxID, &cfg.ConfigType, &gitRepoURL, &cfg.GitBranch, &gitFilePath,
		&cfg.GitPATEncrypted, &cfg.AutoApplyEnabled, &cfg.SyncIntervalMinutes,
		&lastSyncAt, &lastSyncCommit, &lastSyncStatus, &lastSyncError,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get box git config: %w", err)
	}

	if gitRepoURL.Valid {
		cfg.GitRepoURL = gitRepoURL.String
	}
	if gitFilePath.Valid {
		cfg.GitFilePath = gitFilePath.String
	}
	if lastSyncAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastSyncAt.String)
		cfg.LastSyncAt = &t
	}
	if lastSyncCommit.Valid {
		cfg.LastSyncCommit = lastSyncCommit.String
	}
	if lastSyncStatus.Valid {
		cfg.LastSyncStatus = lastSyncStatus.String
	}
	if lastSyncError.Valid {
		cfg.LastSyncError = lastSyncError.String
	}

	return &cfg, nil
}

// SaveBoxGitConfig saves or updates the git configuration for a box.
func (d *DB) SaveBoxGitConfig(cfg *BoxGitConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO box_git_config (
			haproxy_box_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(haproxy_box_id, config_type) DO UPDATE SET
			git_repo_url = excluded.git_repo_url,
			git_branch = excluded.git_branch,
			git_file_path = excluded.git_file_path,
			git_pat_encrypted = excluded.git_pat_encrypted,
			auto_apply_enabled = excluded.auto_apply_enabled,
			sync_interval_minutes = excluded.sync_interval_minutes,
			updated_at = excluded.updated_at`,
		cfg.HAProxyBoxID, cfg.ConfigType, cfg.GitRepoURL, cfg.GitBranch, cfg.GitFilePath,
		cfg.GitPATEncrypted, cfg.AutoApplyEnabled, cfg.SyncIntervalMinutes, time.Now(),
	)
	return err
}

// UpdateGitSyncStatus updates the last sync status for a git configuration.
func (d *DB) UpdateGitSyncStatus(haproxyBoxID int64, configType ConfigType, status, commitSHA, errorMsg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE box_git_config
		SET last_sync_at = ?, last_sync_status = ?, last_sync_commit = ?, last_sync_error = ?, updated_at = ?
		WHERE haproxy_box_id = ? AND config_type = ?`,
		time.Now(), status, commitSHA, errorMsg, time.Now(),
		haproxyBoxID, configType,
	)
	return err
}

// DeleteBoxGitConfig deletes the git configuration for a box and config type.
func (d *DB) DeleteBoxGitConfig(haproxyBoxID int64, configType ConfigType) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		DELETE FROM box_git_config
		WHERE haproxy_box_id = ? AND config_type = ?`,
		haproxyBoxID, configType,
	)
	return err
}

// GetAllGitConfigsForBox retrieves all git configurations for a box.
func (d *DB) GetAllGitConfigsForBox(haproxyBoxID int64) ([]BoxGitConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, haproxy_box_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes,
			last_sync_at, last_sync_commit, last_sync_status, last_sync_error,
			created_at, updated_at
		FROM box_git_config
		WHERE haproxy_box_id = ?
		ORDER BY config_type`,
		haproxyBoxID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get git configs: %w", err)
	}
	defer rows.Close()

	var configs []BoxGitConfig
	for rows.Next() {
		var cfg BoxGitConfig
		var lastSyncAt sql.NullString
		var lastSyncCommit, lastSyncStatus, lastSyncError sql.NullString
		var gitRepoURL, gitFilePath sql.NullString

		err := rows.Scan(
			&cfg.ID, &cfg.HAProxyBoxID, &cfg.ConfigType, &gitRepoURL, &cfg.GitBranch, &gitFilePath,
			&cfg.GitPATEncrypted, &cfg.AutoApplyEnabled, &cfg.SyncIntervalMinutes,
			&lastSyncAt, &lastSyncCommit, &lastSyncStatus, &lastSyncError,
			&cfg.CreatedAt, &cfg.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan git config: %w", err)
		}

		if gitRepoURL.Valid {
			cfg.GitRepoURL = gitRepoURL.String
		}
		if gitFilePath.Valid {
			cfg.GitFilePath = gitFilePath.String
		}
		if lastSyncAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastSyncAt.String)
			cfg.LastSyncAt = &t
		}
		if lastSyncCommit.Valid {
			cfg.LastSyncCommit = lastSyncCommit.String
		}
		if lastSyncStatus.Valid {
			cfg.LastSyncStatus = lastSyncStatus.String
		}
		if lastSyncError.Valid {
			cfg.LastSyncError = lastSyncError.String
		}

		configs = append(configs, cfg)
	}

	return configs, nil
}

// RecordConfigChange records a configuration change in the audit log.
func (d *DB) RecordConfigChange(change *ConfigChange) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO config_changes (
			haproxy_box_id, config_type, change_type,
			previous_sha256, new_sha256, changed_by, change_reason,
			git_commit_sha, backup_path, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		change.HAProxyBoxID, change.ConfigType, change.ChangeType,
		change.PreviousSHA256, change.NewSHA256, change.ChangedBy, change.ChangeReason,
		change.GitCommitSHA, change.BackupPath, time.Now(),
	)
	return err
}

// GetConfigChanges retrieves configuration change history for a box.
func (d *DB) GetConfigChanges(haproxyBoxID int64, configType ConfigType, limit int) ([]ConfigChange, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT cc.id, cc.haproxy_box_id, cc.config_type, cc.change_type,
			cc.previous_sha256, cc.new_sha256, cc.changed_by, cc.change_reason,
			cc.git_commit_sha, cc.backup_path, cc.created_at,
			COALESCE(u.username, '')
		FROM config_changes cc
		LEFT JOIN users u ON cc.changed_by = u.id
		WHERE cc.haproxy_box_id = ? AND cc.config_type = ?
		ORDER BY cc.created_at DESC
		LIMIT ?`,
		haproxyBoxID, configType, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get config changes: %w", err)
	}
	defer rows.Close()

	var changes []ConfigChange
	for rows.Next() {
		var c ConfigChange
		var prevSHA, newSHA, reason, gitCommit, backupPath sql.NullString

		err := rows.Scan(
			&c.ID, &c.HAProxyBoxID, &c.ConfigType, &c.ChangeType,
			&prevSHA, &newSHA, &c.ChangedBy, &reason,
			&gitCommit, &backupPath, &c.CreatedAt, &c.ChangedByName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan config change: %w", err)
		}

		if prevSHA.Valid {
			c.PreviousSHA256 = prevSHA.String
		}
		if newSHA.Valid {
			c.NewSHA256 = newSHA.String
		}
		if reason.Valid {
			c.ChangeReason = reason.String
		}
		if gitCommit.Valid {
			c.GitCommitSHA = gitCommit.String
		}
		if backupPath.Valid {
			c.BackupPath = backupPath.String
		}

		changes = append(changes, c)
	}

	return changes, nil
}

// GetRecentConfigChanges retrieves all recent configuration changes across all boxes.
func (d *DB) GetRecentConfigChanges(limit int) ([]ConfigChange, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT cc.id, cc.haproxy_box_id, cc.config_type, cc.change_type,
			cc.previous_sha256, cc.new_sha256, cc.changed_by, cc.change_reason,
			cc.git_commit_sha, cc.backup_path, cc.created_at,
			COALESCE(u.username, '')
		FROM config_changes cc
		LEFT JOIN users u ON cc.changed_by = u.id
		ORDER BY cc.created_at DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent config changes: %w", err)
	}
	defer rows.Close()

	var changes []ConfigChange
	for rows.Next() {
		var c ConfigChange
		var prevSHA, newSHA, reason, gitCommit, backupPath sql.NullString

		err := rows.Scan(
			&c.ID, &c.HAProxyBoxID, &c.ConfigType, &c.ChangeType,
			&prevSHA, &newSHA, &c.ChangedBy, &reason,
			&gitCommit, &backupPath, &c.CreatedAt, &c.ChangedByName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan config change: %w", err)
		}

		if prevSHA.Valid {
			c.PreviousSHA256 = prevSHA.String
		}
		if newSHA.Valid {
			c.NewSHA256 = newSHA.String
		}
		if reason.Valid {
			c.ChangeReason = reason.String
		}
		if gitCommit.Valid {
			c.GitCommitSHA = gitCommit.String
		}
		if backupPath.Valid {
			c.BackupPath = backupPath.String
		}

		changes = append(changes, c)
	}

	return changes, nil
}
