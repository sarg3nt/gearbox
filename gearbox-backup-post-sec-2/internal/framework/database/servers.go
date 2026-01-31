package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// ServerDB represents a monitored server configuration in the database.
type ServerDB struct {
	ID              int64
	ServerID        string
	Name            string
	Location        string
	Notes           string
	AgentURL        string
	APIKeyEncrypted []byte
	Enabled         bool
	AutoDiscovery   bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       *string // UUID
}

// UsesAgentAPI returns true if this server has valid Agent API configuration.
// All servers use Agent API - this validates the configuration is complete.
func (s *ServerDB) UsesAgentAPI() bool {
	return s.AgentURL != "" && len(s.APIKeyEncrypted) > 0
}

// CreateServer inserts a new server configuration into the database.
func (d *DB) CreateServer(server *ServerDB) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		INSERT INTO servers (
			server_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, created_by, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`

	result, err := d.db.Exec(query,
		server.ServerID,
		server.Name,
		server.Location,
		server.Notes,
		server.AgentURL,
		server.APIKeyEncrypted,
		server.Enabled,
		server.AutoDiscovery,
		server.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert server: %w", err)
	}

	serverID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	server.ID = serverID

	return nil
}

// GetServers retrieves all server configurations from the database.
func (d *DB) GetServers() ([]*ServerDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, created_at, updated_at, created_by
		FROM servers
		ORDER BY name ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var servers []*ServerDB
	for rows.Next() {
		server := &ServerDB{}
		err := rows.Scan(
			&server.ID,
			&server.ServerID,
			&server.Name,
			&server.Location,
			&server.Notes,
			&server.AgentURL,
			&server.APIKeyEncrypted,
			&server.Enabled,
			&server.AutoDiscovery,
			&server.CreatedAt,
			&server.UpdatedAt,
			&server.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}

		servers = append(servers, server)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating servers: %w", err)
	}

	return servers, nil
}

// GetEnabledServers retrieves only enabled server configurations.
func (d *DB) GetEnabledServers() ([]*ServerDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, created_at, updated_at, created_by
		FROM servers
		WHERE enabled = 1
		ORDER BY name ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled servers: %w", err)
	}
	defer rows.Close()

	var servers []*ServerDB
	for rows.Next() {
		server := &ServerDB{}
		err := rows.Scan(
			&server.ID,
			&server.ServerID,
			&server.Name,
			&server.Location,
			&server.Notes,
			&server.AgentURL,
			&server.APIKeyEncrypted,
			&server.Enabled,
			&server.AutoDiscovery,
			&server.CreatedAt,
			&server.UpdatedAt,
			&server.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}

		servers = append(servers, server)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled servers: %w", err)
	}

	return servers, nil
}

// GetServerByID retrieves a server by its database ID.
func (d *DB) GetServerByID(id int64) (*ServerDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, created_at, updated_at, created_by
		FROM servers
		WHERE id = ?
	`

	server := &ServerDB{}
	err := d.db.QueryRow(query, id).Scan(
		&server.ID,
		&server.ServerID,
		&server.Name,
		&server.Location,
		&server.Notes,
		&server.AgentURL,
		&server.APIKeyEncrypted,
		&server.Enabled,
		&server.AutoDiscovery,
		&server.CreatedAt,
		&server.UpdatedAt,
		&server.CreatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %w", err)
	}

	return server, nil
}

// GetServerByServerID retrieves a server by its server_id field.
func (d *DB) GetServerByServerID(serverID string) (*ServerDB, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, name, location, notes, agent_url, api_key_encrypted,
			enabled, auto_discovery, created_at, updated_at, created_by
		FROM servers
		WHERE server_id = ?
	`

	server := &ServerDB{}
	err := d.db.QueryRow(query, serverID).Scan(
		&server.ID,
		&server.ServerID,
		&server.Name,
		&server.Location,
		&server.Notes,
		&server.AgentURL,
		&server.APIKeyEncrypted,
		&server.Enabled,
		&server.AutoDiscovery,
		&server.CreatedAt,
		&server.UpdatedAt,
		&server.CreatedBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %w", err)
	}

	return server, nil
}

// UpdateServer updates an existing server configuration.
func (d *DB) UpdateServer(server *ServerDB) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		UPDATE servers SET
			server_id = ?,
			name = ?,
			location = ?,
			notes = ?,
			agent_url = ?,
			api_key_encrypted = ?,
			enabled = ?,
			auto_discovery = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := d.db.Exec(query,
		server.ServerID,
		server.Name,
		server.Location,
		server.Notes,
		server.AgentURL,
		server.APIKeyEncrypted,
		server.Enabled,
		server.AutoDiscovery,
		server.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update server: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("server not found")
	}

	return nil
}

// DeleteServer deletes a server configuration from the database.
func (d *DB) DeleteServer(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM servers WHERE id = ?`
	result, err := d.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("server not found")
	}

	return nil
}

// SetServerEnabled enables or disables a server.
func (d *DB) SetServerEnabled(id int64, enabled bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
		UPDATE servers
		SET enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	result, err := d.db.Exec(query, enabled, id)
	if err != nil {
		return fmt.Errorf("failed to update server enabled status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("server not found")
	}

	return nil
}

// CountServers returns the total count of servers.
func (d *DB) CountServers() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count servers: %w", err)
	}
	return count, nil
}

// CountEnabledServers returns the count of enabled servers.
func (d *DB) CountEnabledServers() (int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM servers WHERE enabled = 1").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count enabled servers: %w", err)
	}
	return count, nil
}

// ToServerConfig converts a ServerDB to a models.ServerConfig.
// This requires decryption of the API key, which should be done by the caller.
func (s *ServerDB) ToServerConfig(apiKey string) models.ServerConfig {
	return models.ServerConfig{
		ID:       s.ServerID,
		Name:     s.Name,
		AgentURL: s.AgentURL,
		APIKey:   apiKey,
	}
}
