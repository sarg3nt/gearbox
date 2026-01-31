package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PluginName constants for known plugins.
const (
	PluginHAProxy      = "haproxy"
	PluginLogs         = "logs"
	PluginServices     = "services"
	PluginCertificates = "certificates"
	PluginCertbot      = "certbot"
	PluginMetrics      = "metrics"
	PluginTraffic      = "traffic"
	PluginAlerts       = "alerts"
	PluginOSUpdates    = "os_updates"
)

// CertbotRenewalMethod represents how certbot renewal is configured.
type CertbotRenewalMethod string

const (
	CertbotMethodNone    CertbotRenewalMethod = "none"
	CertbotMethodSystemd CertbotRenewalMethod = "systemd"
	CertbotMethodCron    CertbotRenewalMethod = "cron"
)

// CertbotConfig holds certbot-specific configuration (used within CertificatesConfig).
type CertbotConfig struct {
	RenewalMethod CertbotRenewalMethod `json:"renewal_method"`
	ServiceName   string               `json:"service_name,omitempty"` // e.g., "certbot.timer" or custom timer name
	CronPath      string               `json:"cron_path,omitempty"`    // e.g., "/etc/cron.d/certbot"
}

// CertificatesConfig holds certificates integration configuration including certbot settings.
type CertificatesConfig struct {
	CertbotEnabled bool          `json:"certbot_enabled"` // Whether to show certbot renewal status
	Certbot        CertbotConfig `json:"certbot"`         // Certbot configuration
}

// ServicesConfig holds services integration configuration.
type ServicesConfig struct {
	MonitoredServices []string `json:"monitored_services"` // List of systemd service names to monitor
	ShowAll           bool     `json:"show_all"`           // If true, show all services (not just curated list)
}

// MetricsRetentionType represents how metrics retention is configured.
type MetricsRetentionType string

const (
	MetricsRetentionByDays MetricsRetentionType = "days"
	MetricsRetentionBySize MetricsRetentionType = "size"
)

// MetricsConfig holds metrics integration configuration.
type MetricsConfig struct {
	StoreHistory    bool                 `json:"store_history"`     // Whether to store historical metrics data
	RetentionType   MetricsRetentionType `json:"retention_type"`    // How retention is configured (days or size)
	RetentionDays   int                  `json:"retention_days"`    // Days to keep data (if retention_type is "days")
	RetentionSizeMB int                  `json:"retention_size_mb"` // Max size in MB (if retention_type is "size")
}

// BandwidthUnit represents the unit for bandwidth configuration.
type BandwidthUnit string

const (
	BandwidthUnitMbps BandwidthUnit = "mbps" // Megabits per second
	BandwidthUnitGbps BandwidthUnit = "gbps" // Gigabits per second
)

// TrafficConfig holds traffic integration configuration.
type TrafficConfig struct {
	// External (Internet) bandwidth settings - for traffic from public IPs
	OutboundBandwidth     int           `json:"outbound_bandwidth"`      // Total outbound bandwidth to internet
	OutboundBandwidthUnit BandwidthUnit `json:"outbound_bandwidth_unit"` // Unit: mbps or gbps
	InboundBandwidth      int           `json:"inbound_bandwidth"`       // Total inbound bandwidth from internet (optional, defaults to outbound)
	InboundBandwidthUnit  BandwidthUnit `json:"inbound_bandwidth_unit"`  // Unit: mbps or gbps
	// Internal (LAN) bandwidth settings - for traffic from private IPs (10.x, 192.168.x, 172.16-31.x)
	InternalBandwidth     int           `json:"internal_bandwidth"`      // Internal network bandwidth (0 = no limit/use request-based)
	InternalBandwidthUnit BandwidthUnit `json:"internal_bandwidth_unit"` // Unit: mbps or gbps
	// Data retention settings
	RetentionDays int `json:"retention_days"` // Days to keep historical traffic data (default: 7)
}

// GetOutboundBandwidthBps returns the outbound bandwidth in bits per second.
func (c *TrafficConfig) GetOutboundBandwidthBps() int64 {
	if c.OutboundBandwidth == 0 {
		return 0 // No limit configured
	}
	switch c.OutboundBandwidthUnit {
	case BandwidthUnitGbps:
		return int64(c.OutboundBandwidth) * 1_000_000_000
	default: // mbps
		return int64(c.OutboundBandwidth) * 1_000_000
	}
}

// GetInboundBandwidthBps returns the inbound bandwidth in bits per second.
// Falls back to outbound bandwidth if not set.
func (c *TrafficConfig) GetInboundBandwidthBps() int64 {
	if c.InboundBandwidth == 0 {
		return c.GetOutboundBandwidthBps() // Default to outbound
	}
	switch c.InboundBandwidthUnit {
	case BandwidthUnitGbps:
		return int64(c.InboundBandwidth) * 1_000_000_000
	default: // mbps
		return int64(c.InboundBandwidth) * 1_000_000
	}
}

// GetInternalBandwidthBps returns the internal network bandwidth in bits per second.
// Returns 0 if not configured (meaning no bandwidth-based visualization for internal traffic).
func (c *TrafficConfig) GetInternalBandwidthBps() int64 {
	if c.InternalBandwidth == 0 {
		return 0 // No limit - use request-based visualization
	}
	switch c.InternalBandwidthUnit {
	case BandwidthUnitGbps:
		return int64(c.InternalBandwidth) * 1_000_000_000
	default: // mbps
		return int64(c.InternalBandwidth) * 1_000_000
	}
}

// AlertsConfig holds alerts integration configuration.
type AlertsConfig struct {
	RetentionDays       int  `json:"retention_days"`        // Days to keep resolved alerts (default: 30)
	AutoResolveHours    int  `json:"auto_resolve_hours"`    // Hours after which acknowledged alerts auto-resolve (0 = disabled)
	SuppressAfterAck    bool `json:"suppress_after_ack"`    // Suppress new alerts for same rule after acknowledgement
	SuppressMinutes     int  `json:"suppress_minutes"`      // Minutes to suppress after acknowledgement (default: 60)
	EnableLiveStreaming bool `json:"enable_live_streaming"` // Enable real-time alert updates via SSE
	ShowRulesInAlerts   bool `json:"show_rules_in_alerts"`  // Show rules section in alerts page (legacy mode)
}

// OSUpdatesConfig holds OS updates integration configuration.
type OSUpdatesConfig struct {
	CheckFrequencyMinutes  int  `json:"check_frequency_minutes"`  // How often to check for updates (default: 60)
	AutoSecurityUpdates    bool `json:"auto_security_updates"`    // Enable automatic security updates via unattended-upgrades
	AutoReboot             bool `json:"auto_reboot"`              // Allow automatic reboot after updates if needed
	AlertOnAvailable       bool `json:"alert_on_available"`       // Create alert when updates are available
	AlertThreshold         int  `json:"alert_threshold"`          // Alert when N+ updates are pending (0 = any)
	SecurityAlertThreshold int  `json:"security_alert_threshold"` // Alert when N+ security updates pending (0 = any)
	CreateSnapshotBefore   bool `json:"create_snapshot_before"`   // Create snapshot before installing updates
	ShowPipx               bool `json:"show_pipx"`                // Show pipx package management section
	HistoryRetentionDays   int  `json:"history_retention_days"`   // Days to keep update history (default: 90)
}

// Plugin represents an integration configuration.
type Plugin struct {
	ID          int64           `json:"id"`
	ServerID    string          `json:"server_id"`    // HAProxy server this integration belongs to
	Name        string          `json:"name"`         // Unique identifier: logs, services, certificates, metrics
	DisplayName string          `json:"display_name"` // Human-readable name
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"`     // JSON config specific to integration type
	SortOrder   int             `json:"sort_order"` // Order for display in UI (lower = higher priority)
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	UpdatedBy   *int64          `json:"updated_by,omitempty"`
}

// DefaultPlugins returns the default integration configurations for a server.
// All plugins are disabled by default except for explicitly enabled ones.
// Note: Certbot is now part of Certificates configuration, not a separate integration.
func DefaultPlugins(serverID string) []Plugin {
	now := time.Now()
	return []Plugin{
		{
			ServerID:    serverID,
			Name:        "haproxy",
			DisplayName: "HAProxy",
			Description: "HAProxy reverse proxy monitoring with backend, frontend, and server statistics",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginMetrics,
			DisplayName: "Metrics",
			Description: "System resource monitoring: CPU, memory, disk, and network usage with historical data storage",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"store_history":true,"retention_type":"days","retention_days":7,"retention_size_mb":100}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginLogs,
			DisplayName: "Logs",
			Description: "View and search system and HAProxy logs",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginServices,
			DisplayName: "Services",
			Description: "Monitor systemd services status",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"monitored_services":["haproxy","gearbox-agent","nftables","fail2ban"],"show_all":false}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginCertificates,
			DisplayName: "Certificates",
			Description: "View and manage SSL/TLS certificates with optional certbot renewal monitoring",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"certbot_enabled":true,"certbot":{"renewal_method":"systemd","service_name":"certbot.timer"}}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginTraffic,
			DisplayName: "Traffic",
			Description: "Real-time traffic analysis with network visualization, geographic distribution, and per-IP metrics",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"outbound_bandwidth":1000,"outbound_bandwidth_unit":"mbps","inbound_bandwidth":0,"inbound_bandwidth_unit":"mbps","retention_days":7}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginAlerts,
			DisplayName: "Alerts",
			Description: "Real-time alerting with configurable rules for system metrics, backends, certificates, and services",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"retention_days":30,"auto_resolve_hours":0,"suppress_after_ack":true,"suppress_minutes":60,"enable_live_streaming":true,"show_rules_in_alerts":false}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ServerID:    serverID,
			Name:        PluginOSUpdates,
			DisplayName: "OS Updates",
			Description: "Monitor and manage system packages, security updates, and pipx packages with rollback support",
			Enabled:     false, // Disabled by default
			Config:      json.RawMessage(`{"check_frequency_minutes":60,"auto_security_updates":false,"auto_reboot":false,"alert_on_available":true,"alert_threshold":0,"security_alert_threshold":0,"create_snapshot_before":true,"show_pipx":true,"history_retention_days":90}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}

// initPluginsSchema creates the plugins table if it doesn't exist.
func (d *DB) initPluginsSchema() error {
	// Check if we need to migrate from old schema (without server_id)
	var tableExists int
	err := d.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='plugins'").Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check plugins table: %w", err)
	}

	if tableExists > 0 {
		// Table exists, check if it has server_id column
		var hasServerID int
		err := d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('plugins') WHERE name='server_id'").Scan(&hasServerID)
		if err != nil {
			return fmt.Errorf("failed to check server_id column: %w", err)
		}

		if hasServerID == 0 {
			// Old schema - need to migrate
			d.logger.Info("migrating plugins table to server-specific schema")

			// Drop the old table and recreate with new schema
			// (Plugins will be re-seeded per-server on first access)
			_, err := d.db.Exec("DROP TABLE IF EXISTS plugins")
			if err != nil {
				return fmt.Errorf("failed to drop old plugins table: %w", err)
			}

			// Also drop old index
			d.db.Exec("DROP INDEX IF EXISTS idx_plugins_name")

			d.logger.Info("old plugins table dropped, will be recreated with new schema")
		}
	}

	schema := `
	-- Plugins table for feature toggles and configuration (server-specific)
	CREATE TABLE IF NOT EXISTS plugins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		name TEXT NOT NULL,
		display_name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		config TEXT NOT NULL DEFAULT '{}',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by INTEGER,
		UNIQUE(server_id, name),
		FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_plugins_server_name ON plugins(server_id, name);
	CREATE INDEX IF NOT EXISTS idx_plugins_enabled ON plugins(enabled);
	`

	_, err = d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create plugins schema: %w", err)
	}

	// Run migrations for existing tables
	if err := d.migratePluginsSchema(); err != nil {
		return fmt.Errorf("failed to migrate plugins schema: %w", err)
	}

	// Note: Default plugins are now seeded per-server when first accessed via EnsureServerPlugins
	return nil
}

// migratePluginsSchema handles schema migrations for the plugins table.
func (d *DB) migratePluginsSchema() error {
	// Add sort_order column if it doesn't exist
	var hasSortOrder int
	err := d.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('plugins') WHERE name='sort_order'").Scan(&hasSortOrder)
	if err != nil {
		return fmt.Errorf("failed to check sort_order column: %w", err)
	}
	if hasSortOrder == 0 {
		_, err := d.db.Exec("ALTER TABLE plugins ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return fmt.Errorf("failed to add sort_order column: %w", err)
		}
		d.logger.Info("added sort_order column to plugins table")
	}

	// Migrate display names to shorter versions matching sidebar navigation
	displayNameUpdates := map[string]string{
		"metrics":      "Metrics",
		"logs":         "Logs",
		"services":     "Services",
		"certificates": "Certificates",
	}
	for name, displayName := range displayNameUpdates {
		_, err := d.db.Exec("UPDATE plugins SET display_name = ? WHERE name = ? AND display_name != ?", displayName, name, displayName)
		if err != nil {
			d.logger.Warn("failed to update display_name", "name", name, "error", err)
		}
	}

	return nil
}

// GetPlugins returns all plugins for a specific server.
func (d *DB) GetPlugins(serverID string) ([]Plugin, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
		SELECT id, server_id, name, display_name, description, enabled, config, sort_order, created_at, updated_at, updated_by
		FROM plugins
		WHERE server_id = ?
		ORDER BY sort_order ASC, display_name ASC
	`

	rows, err := d.db.Query(query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	var plugins []Plugin
	for rows.Next() {
		var i Plugin
		var configStr string
		if err := rows.Scan(&i.ID, &i.ServerID, &i.Name, &i.DisplayName, &i.Description, &i.Enabled, &configStr, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.UpdatedBy); err != nil {
			return nil, fmt.Errorf("failed to scan integration: %w", err)
		}
		i.Config = json.RawMessage(configStr)
		plugins = append(plugins, i)
	}

	return plugins, rows.Err()
}

// EnsureServerPlugins ensures that default plugins exist for a server.
// It also adds any new plugins that may have been added in newer versions.
// Additionally, it migrates legacy certbot integration into certificates config.
func (d *DB) EnsureServerPlugins(serverID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get existing plugins for this server
	existingNames := make(map[string]bool)
	rows, err := d.db.Query("SELECT name FROM plugins WHERE server_id = ?", serverID)
	if err != nil {
		return fmt.Errorf("failed to query existing plugins: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan integration name: %w", err)
		}
		existingNames[name] = true
	}

	// Migrate legacy certbot integration into certificates config
	if existingNames[PluginCertbot] {
		if err := d.migrateCertbotToCertificatesLocked(serverID); err != nil {
			d.logger.Warn("failed to migrate certbot to certificates", "server_id", serverID, "error", err)
		}
		// Remove certbot from existing names so we don't try to add it
		delete(existingNames, PluginCertbot)
	}

	// Add any missing default plugins
	defaults := DefaultPlugins(serverID)
	addedCount := 0
	for i, intg := range defaults {
		if !existingNames[intg.Name] {
			intg.Config = defaults[i].Config // Ensure we use the config from defaults slice
			if err := d.createPluginWithSortOrderLocked(&intg, i); err != nil {
				return fmt.Errorf("failed to seed integration %s for server %s: %w", intg.Name, serverID, err)
			}
			addedCount++
		}
	}

	if addedCount > 0 {
		d.logger.Info("added missing plugins", "count", addedCount, "server_id", serverID)
	}

	return nil
}

// migrateCertbotToCertificatesLocked merges certbot config into certificates and removes the certbot integration.
func (d *DB) migrateCertbotToCertificatesLocked(serverID string) error {
	// Get the existing certbot config
	var certbotConfigStr string
	var certbotEnabled bool
	err := d.db.QueryRow(`SELECT enabled, config FROM plugins WHERE server_id = ? AND name = ?`,
		serverID, PluginCertbot).Scan(&certbotEnabled, &certbotConfigStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // No certbot to migrate
		}
		return fmt.Errorf("failed to get certbot config: %w", err)
	}

	// Parse the certbot config
	var certbotConfig CertbotConfig
	if err := json.Unmarshal([]byte(certbotConfigStr), &certbotConfig); err != nil {
		// Use defaults if parsing fails
		certbotConfig = CertbotConfig{
			RenewalMethod: CertbotMethodSystemd,
			ServiceName:   "certbot.timer",
		}
	}

	// Create or update the certificates config with certbot settings
	var certsConfigStr string
	err = d.db.QueryRow(`SELECT config FROM plugins WHERE server_id = ? AND name = ?`,
		serverID, PluginCertificates).Scan(&certsConfigStr)

	var certsConfig CertificatesConfig
	if err == nil && certsConfigStr != "" && certsConfigStr != "{}" {
		// Parse existing certificates config
		json.Unmarshal([]byte(certsConfigStr), &certsConfig)
	}

	// Merge certbot settings
	certsConfig.CertbotEnabled = certbotEnabled
	certsConfig.Certbot = certbotConfig

	// Save the updated certificates config
	newConfigBytes, err := json.Marshal(certsConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal certificates config: %w", err)
	}

	_, err = d.db.Exec(`UPDATE plugins SET config = ?, updated_at = CURRENT_TIMESTAMP
		WHERE server_id = ? AND name = ?`, string(newConfigBytes), serverID, PluginCertificates)
	if err != nil {
		return fmt.Errorf("failed to update certificates config: %w", err)
	}

	// Delete the legacy certbot integration
	_, err = d.db.Exec(`DELETE FROM plugins WHERE server_id = ? AND name = ?`, serverID, PluginCertbot)
	if err != nil {
		return fmt.Errorf("failed to delete certbot integration: %w", err)
	}

	d.logger.Info("migrated certbot integration into certificates", "server_id", serverID)
	return nil
}

// createPluginWithSortOrderLocked creates a new integration with sort order.
func (d *DB) createPluginWithSortOrderLocked(integration *Plugin, sortOrder int) error {
	configStr := string(integration.Config)
	if configStr == "" {
		configStr = "{}"
	}

	result, err := d.db.Exec(`
		INSERT INTO plugins (server_id, name, display_name, description, enabled, config, sort_order, created_at, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, integration.ServerID, integration.Name, integration.DisplayName, integration.Description, integration.Enabled, configStr, sortOrder,
		integration.CreatedAt, integration.UpdatedAt, integration.UpdatedBy)

	if err != nil {
		return fmt.Errorf("failed to create integration: %w", err)
	}

	id, _ := result.LastInsertId()
	integration.ID = id
	return nil
}

// GetPlugin returns a specific integration by server and name.
func (d *DB) GetPlugin(serverID, name string) (*Plugin, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var i Plugin
	var configStr string
	err := d.db.QueryRow(`
		SELECT id, server_id, name, display_name, description, enabled, config, sort_order, created_at, updated_at, updated_by
		FROM plugins
		WHERE server_id = ? AND name = ?
	`, serverID, name).Scan(&i.ID, &i.ServerID, &i.Name, &i.DisplayName, &i.Description, &i.Enabled, &configStr, &i.SortOrder, &i.CreatedAt, &i.UpdatedAt, &i.UpdatedBy)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get integration: %w", err)
	}
	i.Config = json.RawMessage(configStr)

	return &i, nil
}

// CreatePlugin creates a new integration.
func (d *DB) CreatePlugin(integration *Plugin) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.createPluginLocked(integration)
}

// createPluginLocked creates a new integration (must be called with lock held).
func (d *DB) createPluginLocked(integration *Plugin) error {
	configStr := string(integration.Config)
	if configStr == "" {
		configStr = "{}"
	}

	result, err := d.db.Exec(`
		INSERT INTO plugins (server_id, name, display_name, description, enabled, config, created_at, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, integration.ServerID, integration.Name, integration.DisplayName, integration.Description, integration.Enabled, configStr,
		integration.CreatedAt, integration.UpdatedAt, integration.UpdatedBy)

	if err != nil {
		return fmt.Errorf("failed to create integration: %w", err)
	}

	id, _ := result.LastInsertId()
	integration.ID = id
	return nil
}

// UpdatePlugin updates an existing integration.
func (d *DB) UpdatePlugin(integration *Plugin) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	configStr := string(integration.Config)
	if configStr == "" {
		configStr = "{}"
	}

	_, err := d.db.Exec(`
		UPDATE plugins
		SET display_name = ?, description = ?, enabled = ?, config = ?, updated_at = ?, updated_by = ?
		WHERE server_id = ? AND name = ?
	`, integration.DisplayName, integration.Description, integration.Enabled, configStr,
		time.Now(), integration.UpdatedBy, integration.ServerID, integration.Name)

	if err != nil {
		return fmt.Errorf("failed to update integration: %w", err)
	}

	return nil
}

// SetPluginEnabled enables or disables an integration for a server.
func (d *DB) SetPluginEnabled(serverID, name string, enabled bool, userID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE plugins
		SET enabled = ?, updated_at = ?, updated_by = ?
		WHERE server_id = ? AND name = ?
	`, enabled, time.Now(), userID, serverID, name)

	if err != nil {
		return fmt.Errorf("failed to set integration enabled: %w", err)
	}

	return nil
}

// SetPluginConfig updates the config for an integration.
func (d *DB) SetPluginConfig(serverID, name string, config json.RawMessage, userID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	configStr := string(config)
	if configStr == "" {
		configStr = "{}"
	}

	_, err := d.db.Exec(`
		UPDATE plugins
		SET config = ?, updated_at = ?, updated_by = ?
		WHERE server_id = ? AND name = ?
	`, configStr, time.Now(), userID, serverID, name)

	if err != nil {
		return fmt.Errorf("failed to set integration config: %w", err)
	}

	return nil
}

// UpdatePluginSortOrders updates the sort order for multiple plugins.
// Takes a map of integration name to new sort order.
func (d *DB) UpdatePluginSortOrders(serverID string, orders map[string]int, userID *string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for name, order := range orders {
		_, err := d.db.Exec(`
			UPDATE plugins
			SET sort_order = ?, updated_at = ?, updated_by = ?
			WHERE server_id = ? AND name = ?
		`, order, time.Now(), userID, serverID, name)

		if err != nil {
			return fmt.Errorf("failed to update sort order for %s: %w", name, err)
		}
	}

	return nil
}

// IsPluginEnabled checks if an integration is enabled for a server.
func (d *DB) IsPluginEnabled(serverID, name string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var enabled bool
	err := d.db.QueryRow(`
		SELECT enabled FROM plugins WHERE server_id = ? AND name = ?
	`, serverID, name).Scan(&enabled)

	if err == sql.ErrNoRows {
		// If integration doesn't exist, assume enabled (default behavior)
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check integration status: %w", err)
	}

	return enabled, nil
}

// GetEnabledPlugins returns a map of integration name to enabled status for a server.
func (d *DB) GetEnabledPlugins(serverID string) (map[string]bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT name, enabled FROM plugins WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, fmt.Errorf("failed to scan integration: %w", err)
		}
		result[name] = enabled
	}

	return result, rows.Err()
}

// GetServicesConfig returns the services integration configuration for a server.
func (d *DB) GetServicesConfig(serverID string) (*ServicesConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginServices)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config
		return &ServicesConfig{
			MonitoredServices: []string{"haproxy", "gearbox-agent", "nftables", "fail2ban"},
			ShowAll:           false,
		}, nil
	}

	var config ServicesConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		return nil, fmt.Errorf("failed to parse services config: %w", err)
	}

	return &config, nil
}

// GetCertificatesConfig returns the certificates integration configuration for a server.
func (d *DB) GetCertificatesConfig(serverID string) (*CertificatesConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginCertificates)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config
		return &CertificatesConfig{
			CertbotEnabled: true,
			Certbot: CertbotConfig{
				RenewalMethod: CertbotMethodSystemd,
				ServiceName:   "certbot.timer",
			},
		}, nil
	}

	var config CertificatesConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		// Try legacy format (empty config or old certbot standalone)
		return &CertificatesConfig{
			CertbotEnabled: true,
			Certbot: CertbotConfig{
				RenewalMethod: CertbotMethodSystemd,
				ServiceName:   "certbot.timer",
			},
		}, nil
	}

	return &config, nil
}

// GetCertbotConfig returns the certbot configuration from the certificates integration.
// This is a convenience wrapper that extracts certbot settings from CertificatesConfig.
func (d *DB) GetCertbotConfig(serverID string) (*CertbotConfig, error) {
	certsConfig, err := d.GetCertificatesConfig(serverID)
	if err != nil {
		return nil, err
	}
	return &certsConfig.Certbot, nil
}

// IsCertbotEnabled returns whether certbot integration is enabled for a server.
func (d *DB) IsCertbotEnabled(serverID string) (bool, error) {
	certsConfig, err := d.GetCertificatesConfig(serverID)
	if err != nil {
		return false, err
	}
	return certsConfig.CertbotEnabled, nil
}

// GetMetricsConfig returns the metrics integration configuration for a server.
func (d *DB) GetMetricsConfig(serverID string) (*MetricsConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginMetrics)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config
		return &MetricsConfig{
			StoreHistory:    true,
			RetentionType:   MetricsRetentionByDays,
			RetentionDays:   7,
			RetentionSizeMB: 100,
		}, nil
	}

	var config MetricsConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		return nil, fmt.Errorf("failed to parse metrics config: %w", err)
	}

	return &config, nil
}

// GetTrafficConfig returns the traffic integration configuration for a server.
func (d *DB) GetTrafficConfig(serverID string) (*TrafficConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginTraffic)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config (1 Gbps)
		return &TrafficConfig{
			OutboundBandwidth:     1000,
			OutboundBandwidthUnit: BandwidthUnitMbps,
			InboundBandwidth:      0,
			InboundBandwidthUnit:  BandwidthUnitMbps,
		}, nil
	}

	var config TrafficConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		return nil, fmt.Errorf("failed to parse traffic config: %w", err)
	}

	// Set defaults if not specified
	if config.OutboundBandwidthUnit == "" {
		config.OutboundBandwidthUnit = BandwidthUnitMbps
	}
	if config.InboundBandwidthUnit == "" {
		config.InboundBandwidthUnit = BandwidthUnitMbps
	}
	if config.RetentionDays == 0 {
		config.RetentionDays = 7 // Default to 7 days
	}

	return &config, nil
}

// GetAlertsConfig returns the alerts integration configuration for a server.
func (d *DB) GetAlertsConfig(serverID string) (*AlertsConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginAlerts)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config
		return &AlertsConfig{
			RetentionDays:       30,
			AutoResolveHours:    0,
			SuppressAfterAck:    true,
			SuppressMinutes:     60,
			EnableLiveStreaming: true,
			ShowRulesInAlerts:   false,
		}, nil
	}

	var config AlertsConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		return nil, fmt.Errorf("failed to parse alerts config: %w", err)
	}

	// Set defaults if not specified
	if config.RetentionDays == 0 {
		config.RetentionDays = 30
	}
	if config.SuppressMinutes == 0 && config.SuppressAfterAck {
		config.SuppressMinutes = 60
	}

	return &config, nil
}

// GetOSUpdatesConfig returns the OS updates integration configuration for a server.
func (d *DB) GetOSUpdatesConfig(serverID string) (*OSUpdatesConfig, error) {
	integration, err := d.GetPlugin(serverID, PluginOSUpdates)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		// Return default config
		return &OSUpdatesConfig{
			CheckFrequencyMinutes:  60,
			AutoSecurityUpdates:    false,
			AutoReboot:             false,
			AlertOnAvailable:       true,
			AlertThreshold:         0,
			SecurityAlertThreshold: 0,
			CreateSnapshotBefore:   true,
			ShowPipx:               true,
			HistoryRetentionDays:   90,
		}, nil
	}

	var config OSUpdatesConfig
	if err := json.Unmarshal(integration.Config, &config); err != nil {
		return nil, fmt.Errorf("failed to parse os_updates config: %w", err)
	}

	// Set defaults if not specified
	if config.CheckFrequencyMinutes == 0 {
		config.CheckFrequencyMinutes = 60
	}
	if config.HistoryRetentionDays == 0 {
		config.HistoryRetentionDays = 90
	}

	return &config, nil
}
