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

// ServerGitConfig stores git configuration for a HAProxy server.
type ServerGitConfig struct {
	ID                  int64      `json:"id"`
	HAProxyServerID     int64      `json:"haproxy_server_id"`
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
	ID              int64      `json:"id"`
	HAProxyServerID int64      `json:"haproxy_server_id"`
	ConfigType      ConfigType `json:"config_type"`
	ChangeType      ChangeType `json:"change_type"`
	PreviousSHA256  string     `json:"previous_sha256,omitempty"`
	NewSHA256       string     `json:"new_sha256,omitempty"`
	ChangedBy       *string    `json:"changed_by,omitempty"` // UUID
	ChangedByName   string     `json:"changed_by_name,omitempty"` // Populated on read
	ChangeReason    string     `json:"change_reason,omitempty"`
	GitCommitSHA    string     `json:"git_commit_sha,omitempty"`
	BackupPath      string     `json:"backup_path,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// initConfigSchema creates the configuration management tables.
func (d *DB) initConfigSchema() error {
	schema := `
	-- Git configuration per HAProxy server
	CREATE TABLE IF NOT EXISTS server_git_config (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		haproxy_server_id INTEGER NOT NULL,
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
		FOREIGN KEY (haproxy_server_id) REFERENCES haproxy_servers(id) ON DELETE CASCADE,
		UNIQUE(haproxy_server_id, config_type)
	);

	CREATE INDEX IF NOT EXISTS idx_server_git_config_server
		ON server_git_config(haproxy_server_id);

	-- Config change history (audit trail)
	CREATE TABLE IF NOT EXISTS config_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		haproxy_server_id INTEGER NOT NULL,
		config_type TEXT NOT NULL,
		change_type TEXT NOT NULL,
		previous_sha256 TEXT,
		new_sha256 TEXT,
		changed_by INTEGER,
		change_reason TEXT,
		git_commit_sha TEXT,
		backup_path TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (haproxy_server_id) REFERENCES haproxy_servers(id) ON DELETE CASCADE,
		FOREIGN KEY (changed_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_config_changes_server_type
		ON config_changes(haproxy_server_id, config_type, created_at);
	`

	_, err := d.db.Exec(schema)
	return err
}

// GetServerGitConfig retrieves the git configuration for a server and config type.
func (d *DB) GetServerGitConfig(haproxyServerID int64, configType ConfigType) (*ServerGitConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var cfg ServerGitConfig
	var lastSyncAt sql.NullString
	var lastSyncCommit, lastSyncStatus, lastSyncError sql.NullString
	var gitRepoURL, gitFilePath sql.NullString

	err := d.db.QueryRow(`
		SELECT id, haproxy_server_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes,
			last_sync_at, last_sync_commit, last_sync_status, last_sync_error,
			created_at, updated_at
		FROM server_git_config
		WHERE haproxy_server_id = ? AND config_type = ?`,
		haproxyServerID, configType,
	).Scan(
		&cfg.ID, &cfg.HAProxyServerID, &cfg.ConfigType, &gitRepoURL, &cfg.GitBranch, &gitFilePath,
		&cfg.GitPATEncrypted, &cfg.AutoApplyEnabled, &cfg.SyncIntervalMinutes,
		&lastSyncAt, &lastSyncCommit, &lastSyncStatus, &lastSyncError,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get server git config: %w", err)
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

// SaveServerGitConfig saves or updates the git configuration for a server.
func (d *DB) SaveServerGitConfig(cfg *ServerGitConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO server_git_config (
			haproxy_server_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(haproxy_server_id, config_type) DO UPDATE SET
			git_repo_url = excluded.git_repo_url,
			git_branch = excluded.git_branch,
			git_file_path = excluded.git_file_path,
			git_pat_encrypted = excluded.git_pat_encrypted,
			auto_apply_enabled = excluded.auto_apply_enabled,
			sync_interval_minutes = excluded.sync_interval_minutes,
			updated_at = excluded.updated_at`,
		cfg.HAProxyServerID, cfg.ConfigType, cfg.GitRepoURL, cfg.GitBranch, cfg.GitFilePath,
		cfg.GitPATEncrypted, cfg.AutoApplyEnabled, cfg.SyncIntervalMinutes, time.Now(),
	)
	return err
}

// UpdateGitSyncStatus updates the last sync status for a git configuration.
func (d *DB) UpdateGitSyncStatus(haproxyServerID int64, configType ConfigType, status, commitSHA, errorMsg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE server_git_config
		SET last_sync_at = ?, last_sync_status = ?, last_sync_commit = ?, last_sync_error = ?, updated_at = ?
		WHERE haproxy_server_id = ? AND config_type = ?`,
		time.Now(), status, commitSHA, errorMsg, time.Now(),
		haproxyServerID, configType,
	)
	return err
}

// DeleteServerGitConfig deletes the git configuration for a server and config type.
func (d *DB) DeleteServerGitConfig(haproxyServerID int64, configType ConfigType) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		DELETE FROM server_git_config
		WHERE haproxy_server_id = ? AND config_type = ?`,
		haproxyServerID, configType,
	)
	return err
}

// GetAllGitConfigsForServer retrieves all git configurations for a server.
func (d *DB) GetAllGitConfigsForServer(haproxyServerID int64) ([]ServerGitConfig, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, haproxy_server_id, config_type, git_repo_url, git_branch, git_file_path,
			git_pat_encrypted, auto_apply_enabled, sync_interval_minutes,
			last_sync_at, last_sync_commit, last_sync_status, last_sync_error,
			created_at, updated_at
		FROM server_git_config
		WHERE haproxy_server_id = ?
		ORDER BY config_type`,
		haproxyServerID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get git configs: %w", err)
	}
	defer rows.Close()

	var configs []ServerGitConfig
	for rows.Next() {
		var cfg ServerGitConfig
		var lastSyncAt sql.NullString
		var lastSyncCommit, lastSyncStatus, lastSyncError sql.NullString
		var gitRepoURL, gitFilePath sql.NullString

		err := rows.Scan(
			&cfg.ID, &cfg.HAProxyServerID, &cfg.ConfigType, &gitRepoURL, &cfg.GitBranch, &gitFilePath,
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
			haproxy_server_id, config_type, change_type,
			previous_sha256, new_sha256, changed_by, change_reason,
			git_commit_sha, backup_path, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		change.HAProxyServerID, change.ConfigType, change.ChangeType,
		change.PreviousSHA256, change.NewSHA256, change.ChangedBy, change.ChangeReason,
		change.GitCommitSHA, change.BackupPath, time.Now(),
	)
	return err
}

// GetConfigChanges retrieves configuration change history for a server.
func (d *DB) GetConfigChanges(haproxyServerID int64, configType ConfigType, limit int) ([]ConfigChange, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT cc.id, cc.haproxy_server_id, cc.config_type, cc.change_type,
			cc.previous_sha256, cc.new_sha256, cc.changed_by, cc.change_reason,
			cc.git_commit_sha, cc.backup_path, cc.created_at,
			COALESCE(u.username, '')
		FROM config_changes cc
		LEFT JOIN users u ON cc.changed_by = u.id
		WHERE cc.haproxy_server_id = ? AND cc.config_type = ?
		ORDER BY cc.created_at DESC
		LIMIT ?`,
		haproxyServerID, configType, limit,
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
			&c.ID, &c.HAProxyServerID, &c.ConfigType, &c.ChangeType,
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

// GetRecentConfigChanges retrieves all recent configuration changes across all servers.
func (d *DB) GetRecentConfigChanges(limit int) ([]ConfigChange, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT cc.id, cc.haproxy_server_id, cc.config_type, cc.change_type,
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
			&c.ID, &c.HAProxyServerID, &c.ConfigType, &c.ChangeType,
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
