package database

import (
	"database/sql"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// initFirewallSchema creates the firewall-related tables.
func (d *DB) initFirewallSchema() error {
	schema := `
	-- Blocked IPs tracks IPs that have been manually or automatically blocked
	CREATE TABLE IF NOT EXISTS blocked_ips (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		ip_address TEXT NOT NULL,
		reason TEXT NOT NULL,
		blocked_at DATETIME NOT NULL,
		blocked_by INTEGER,
		unblocked_at DATETIME,
		unblocked_by INTEGER,
		expires_at DATETIME,
		auto_blocked INTEGER NOT NULL DEFAULT 0,
		country TEXT,
		country_code TEXT,
		threat_score INTEGER DEFAULT 0,
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(server_id, ip_address, blocked_at),
		FOREIGN KEY (blocked_by) REFERENCES users(id),
		FOREIGN KEY (unblocked_by) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_blocked_ips_server
		ON blocked_ips(server_id);
	CREATE INDEX IF NOT EXISTS idx_blocked_ips_active
		ON blocked_ips(server_id, unblocked_at) WHERE unblocked_at IS NULL;
	CREATE INDEX IF NOT EXISTS idx_blocked_ips_ip
		ON blocked_ips(ip_address);
	`

	_, err := d.db.Exec(schema)
	return err
}

// BlockIP records an IP block in the database.
func (d *DB) BlockIP(block *models.BlockedIP) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO blocked_ips (
			server_id, ip_address, reason, blocked_at, blocked_by,
			expires_at, auto_blocked, country, country_code, threat_score, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		block.ServerID, block.IPAddress, block.Reason, block.BlockedAt, block.BlockedBy,
		block.ExpiresAt, block.AutoBlocked, block.Country, block.CountryCode,
		block.ThreatScore, block.Notes,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UnblockIP marks an IP as unblocked.
func (d *DB) UnblockIP(serverID, ipAddress string, userID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE blocked_ips SET
			unblocked_at = CURRENT_TIMESTAMP,
			unblocked_by = ?
		WHERE server_id = ? AND ip_address = ? AND unblocked_at IS NULL`,
		userID, serverID, ipAddress,
	)
	return err
}

// GetBlockedIPs returns currently blocked IPs for a server.
func (d *DB) GetBlockedIPs(serverID string) ([]models.BlockedIP, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT b.id, b.server_id, b.ip_address, b.reason, b.blocked_at, b.blocked_by,
			b.unblocked_at, b.unblocked_by, b.expires_at, b.auto_blocked,
			b.country, b.country_code, b.threat_score, b.notes,
			COALESCE(u.username, '') as blocked_by_username
		FROM blocked_ips b
		LEFT JOIN users u ON b.blocked_by = u.id
		WHERE b.server_id = ? AND b.unblocked_at IS NULL
			AND (b.expires_at IS NULL OR b.expires_at > datetime('now'))
		ORDER BY b.blocked_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanBlockedIPs(rows)
}

// GetAllBlockedIPs returns all blocked IPs (including unblocked/expired) for a server.
func (d *DB) GetAllBlockedIPs(serverID string, limit int) ([]models.BlockedIP, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT b.id, b.server_id, b.ip_address, b.reason, b.blocked_at, b.blocked_by,
			b.unblocked_at, b.unblocked_by, b.expires_at, b.auto_blocked,
			b.country, b.country_code, b.threat_score, b.notes,
			COALESCE(u.username, '') as blocked_by_username
		FROM blocked_ips b
		LEFT JOIN users u ON b.blocked_by = u.id
		WHERE b.server_id = ?
		ORDER BY b.blocked_at DESC
		LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanBlockedIPs(rows)
}

// IsIPBlocked checks if an IP is currently blocked.
func (d *DB) IsIPBlocked(serverID, ipAddress string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM blocked_ips
		WHERE server_id = ? AND ip_address = ? AND unblocked_at IS NULL
			AND (expires_at IS NULL OR expires_at > datetime('now'))`,
		serverID, ipAddress,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetBlockedIPByAddress returns the block record for a specific IP.
func (d *DB) GetBlockedIPByAddress(serverID, ipAddress string) (*models.BlockedIP, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`
		SELECT b.id, b.server_id, b.ip_address, b.reason, b.blocked_at, b.blocked_by,
			b.unblocked_at, b.unblocked_by, b.expires_at, b.auto_blocked,
			b.country, b.country_code, b.threat_score, b.notes,
			COALESCE(u.username, '') as blocked_by_username
		FROM blocked_ips b
		LEFT JOIN users u ON b.blocked_by = u.id
		WHERE b.server_id = ? AND b.ip_address = ? AND b.unblocked_at IS NULL
			AND (b.expires_at IS NULL OR b.expires_at > datetime('now'))
		ORDER BY b.blocked_at DESC
		LIMIT 1`, serverID, ipAddress)

	var b models.BlockedIP
	var blockedBy, unblockedBy sql.NullString
	var unblockedAt, expiresAt sql.NullTime
	var country, countryCode, notes sql.NullString

	err := row.Scan(
		&b.ID, &b.ServerID, &b.IPAddress, &b.Reason, &b.BlockedAt, &blockedBy,
		&unblockedAt, &unblockedBy, &expiresAt, &b.AutoBlocked,
		&country, &countryCode, &b.ThreatScore, &notes, &b.BlockedByUsername,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if blockedBy.Valid {
		b.BlockedBy = &blockedBy.String
	}
	if unblockedAt.Valid {
		b.UnblockedAt = &unblockedAt.Time
	}
	if unblockedBy.Valid {
		b.UnblockedBy = &unblockedBy.String
	}
	if expiresAt.Valid {
		b.ExpiresAt = &expiresAt.Time
	}
	if country.Valid {
		b.Country = country.String
	}
	if countryCode.Valid {
		b.CountryCode = countryCode.String
	}
	if notes.Valid {
		b.Notes = notes.String
	}

	return &b, nil
}

// scanBlockedIPs scans rows into BlockedIP structs.
func (d *DB) scanBlockedIPs(rows *sql.Rows) ([]models.BlockedIP, error) {
	var blocks []models.BlockedIP
	for rows.Next() {
		var b models.BlockedIP
		var blockedBy, unblockedBy sql.NullString
		var unblockedAt, expiresAt sql.NullTime
		var country, countryCode, notes sql.NullString

		err := rows.Scan(
			&b.ID, &b.ServerID, &b.IPAddress, &b.Reason, &b.BlockedAt, &blockedBy,
			&unblockedAt, &unblockedBy, &expiresAt, &b.AutoBlocked,
			&country, &countryCode, &b.ThreatScore, &notes, &b.BlockedByUsername,
		)
		if err != nil {
			return nil, err
		}

		if blockedBy.Valid {
			b.BlockedBy = &blockedBy.String
		}
		if unblockedAt.Valid {
			b.UnblockedAt = &unblockedAt.Time
		}
		if unblockedBy.Valid {
			b.UnblockedBy = &unblockedBy.String
		}
		if expiresAt.Valid {
			b.ExpiresAt = &expiresAt.Time
		}
		if country.Valid {
			b.Country = country.String
		}
		if countryCode.Valid {
			b.CountryCode = countryCode.String
		}
		if notes.Valid {
			b.Notes = notes.String
		}

		blocks = append(blocks, b)
	}
	return blocks, nil
}

// GetBlockedIPCount returns the count of currently blocked IPs.
func (d *DB) GetBlockedIPCount(serverID string) (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM blocked_ips
		WHERE server_id = ? AND unblocked_at IS NULL
			AND (expires_at IS NULL OR expires_at > datetime('now'))`,
		serverID,
	).Scan(&count)
	return count, err
}

// CleanupExpiredBlocks removes expired block records.
func (d *DB) CleanupExpiredBlocks() (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Mark expired blocks as unblocked
	result, err := d.db.Exec(`
		UPDATE blocked_ips SET
			unblocked_at = expires_at
		WHERE unblocked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= datetime('now')`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetRecentBlockActivity returns recent block/unblock activity.
func (d *DB) GetRecentBlockActivity(serverID string, limit int) ([]models.BlockedIP, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT b.id, b.server_id, b.ip_address, b.reason, b.blocked_at, b.blocked_by,
			b.unblocked_at, b.unblocked_by, b.expires_at, b.auto_blocked,
			b.country, b.country_code, b.threat_score, b.notes,
			COALESCE(u.username, '') as blocked_by_username
		FROM blocked_ips b
		LEFT JOIN users u ON b.blocked_by = u.id
		WHERE b.server_id = ?
		ORDER BY COALESCE(b.unblocked_at, b.blocked_at) DESC
		LIMIT ?`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanBlockedIPs(rows)
}

// GetAutoBlockedIPs returns IPs that were automatically blocked.
func (d *DB) GetAutoBlockedIPs(serverID string) ([]models.BlockedIP, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT b.id, b.server_id, b.ip_address, b.reason, b.blocked_at, b.blocked_by,
			b.unblocked_at, b.unblocked_by, b.expires_at, b.auto_blocked,
			b.country, b.country_code, b.threat_score, b.notes,
			'' as blocked_by_username
		FROM blocked_ips b
		WHERE b.server_id = ? AND b.auto_blocked = 1 AND b.unblocked_at IS NULL
			AND (b.expires_at IS NULL OR b.expires_at > datetime('now'))
		ORDER BY b.blocked_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanBlockedIPs(rows)
}
