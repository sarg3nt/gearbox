package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// DB wraps the SQLite database connection.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
	mu     sync.RWMutex
}

// New creates a new database connection.
func New(dbPath string, logger *slog.Logger) (*DB, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	d := &DB{
		db:     db,
		logger: logger,
	}

	// Initialize user schema FIRST (other tables have foreign key references to users)
	if err := d.initUserSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize user schema: %w", err)
	}

	// Initialize main schema (depends on users table existing)
	if err := d.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Initialize plugins schema (depends on users table existing)
	if err := d.initPluginsSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize plugins schema: %w", err)
	}

	// Initialize permissions schema (depends on users table existing)
	if err := d.initPermissionsSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize permissions schema: %w", err)
	}

	// Initialize traffic analysis schema
	if err := d.initTrafficSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize traffic schema: %w", err)
	}

	// Initialize alerts schema
	if err := d.initAlertsSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize alerts schema: %w", err)
	}

	// Initialize firewall/blocked IPs schema
	if err := d.initFirewallSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize firewall schema: %w", err)
	}

	// Initialize configuration management schema
	if err := d.initConfigSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize config schema: %w", err)
	}

	// Run schema migrations AFTER all schemas are initialized
	// (migrations may reference tables from any schema)
	if err := d.runSchemaMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run schema migrations: %w", err)
	}

	logger.Info("database initialized", "path", dbPath)
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// GetDB returns the underlying *sql.DB connection.
// This is primarily used by the plugin system for direct database access.
func (d *DB) GetDB() *sql.DB {
	return d.db
}

// initSchema creates the database tables if they don't exist.
func (d *DB) initSchema() error {
	schema := `
	-- Stats history table for storing periodic snapshots
	CREATE TABLE IF NOT EXISTS stats_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		collected_at DATETIME NOT NULL,
		total_frontends INTEGER NOT NULL DEFAULT 0,
		total_backends INTEGER NOT NULL DEFAULT 0,
		total_servers INTEGER NOT NULL DEFAULT 0,
		healthy_servers INTEGER NOT NULL DEFAULT 0,
		total_sessions INTEGER NOT NULL DEFAULT 0,
		total_requests INTEGER NOT NULL DEFAULT 0,
		total_bytes_in INTEGER NOT NULL DEFAULT 0,
		total_bytes_out INTEGER NOT NULL DEFAULT 0,
		avg_response_time INTEGER NOT NULL DEFAULT 0,
		total_5xx_errors INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_stats_history_box_time
		ON stats_history(box_id, collected_at);

	-- Backend history for individual backend metrics
	CREATE TABLE IF NOT EXISTS backend_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		backend_name TEXT NOT NULL,
		collected_at DATETIME NOT NULL,
		status TEXT NOT NULL,
		health_percent REAL NOT NULL DEFAULT 0,
		active_servers INTEGER NOT NULL DEFAULT 0,
		total_servers INTEGER NOT NULL DEFAULT 0,
		current_sessions INTEGER NOT NULL DEFAULT 0,
		response_time INTEGER NOT NULL DEFAULT 0,
		queue_current INTEGER NOT NULL DEFAULT 0,
		bytes_in INTEGER NOT NULL DEFAULT 0,
		bytes_out INTEGER NOT NULL DEFAULT 0,
		response_5xx INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_backend_history_box_backend_time
		ON backend_history(box_id, backend_name, collected_at);

	-- System metrics history
	CREATE TABLE IF NOT EXISTS system_metrics_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		collected_at DATETIME NOT NULL,
		load_average_1 REAL NOT NULL DEFAULT 0,
		load_average_5 REAL NOT NULL DEFAULT 0,
		load_average_15 REAL NOT NULL DEFAULT 0,
		memory_usage_percent REAL NOT NULL DEFAULT 0,
		disk_usage_percent REAL NOT NULL DEFAULT 0,
		network_rx_bytes INTEGER NOT NULL DEFAULT 0,
		network_tx_bytes INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_system_metrics_history_box_time
		ON system_metrics_history(box_id, collected_at);

	-- Incidents/events table for tracking notable events
	CREATE TABLE IF NOT EXISTS incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		backend_name TEXT,
		incident_type TEXT NOT NULL,
		severity TEXT NOT NULL,
		message TEXT NOT NULL,
		started_at DATETIME NOT NULL,
		resolved_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_incidents_box_time
		ON incidents(box_id, started_at);
	CREATE INDEX IF NOT EXISTS idx_incidents_unresolved
		ON incidents(box_id, resolved_at) WHERE resolved_at IS NULL;

	-- Alerts configuration table
	CREATE TABLE IF NOT EXISTS alert_configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		backend_name TEXT,
		alert_type TEXT NOT NULL,
		threshold REAL NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_configs_unique
		ON alert_configs(box_id, COALESCE(backend_name, ''), alert_type);

	-- User preferences/settings
	CREATE TABLE IF NOT EXISTS user_preferences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		preferences TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Box configurations (Agent API - supports any monitored box)
	CREATE TABLE IF NOT EXISTS boxes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		location TEXT NOT NULL DEFAULT '',
		notes TEXT NOT NULL DEFAULT '',
		agent_url TEXT NOT NULL,
		api_key_encrypted BLOB NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		auto_discovery INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_by TEXT, -- UUID
		FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_boxes_enabled
		ON boxes(enabled);


	-- Log source settings: enabled log sources per box
	CREATE TABLE IF NOT EXISTS log_source_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id INTEGER NOT NULL,
		log_name TEXT NOT NULL,
		display_name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (box_id) REFERENCES boxes(id) ON DELETE CASCADE,
		UNIQUE(box_id, log_name)
	);

	CREATE INDEX IF NOT EXISTS idx_log_source_settings_box
		ON log_source_settings(box_id);

	-- Disabled entities: track disabled backends, frontends, and services
	CREATE TABLE IF NOT EXISTS disabled_entities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		box_id TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_name TEXT NOT NULL,
		disabled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		disabled_by TEXT, -- UUID
		notes TEXT,
		FOREIGN KEY (disabled_by) REFERENCES users(id) ON DELETE SET NULL,
		UNIQUE(box_id, entity_type, entity_name)
	);

	CREATE INDEX IF NOT EXISTS idx_disabled_entities_box_type
		ON disabled_entities(box_id, entity_type);
	`

	_, err := d.db.Exec(schema)
	return err
}

// StatsSnapshot represents aggregated stats at a point in time.
type StatsSnapshot struct {
	ID              int64     `json:"id"`
	BoxID           string    `json:"box_id"`
	CollectedAt     time.Time `json:"collected_at"`
	TotalFrontends  int       `json:"total_frontends"`
	TotalBackends   int       `json:"total_backends"`
	TotalServers    int       `json:"total_servers"`
	HealthyServers  int       `json:"healthy_servers"`
	TotalSessions   int64     `json:"total_sessions"`
	TotalRequests   int64     `json:"total_requests"`
	TotalBytesIn    int64     `json:"total_bytes_in"`
	TotalBytesOut   int64     `json:"total_bytes_out"`
	AvgResponseTime int64     `json:"avg_response_time"`
	Total5xxErrors  int64     `json:"total_5xx_errors"`
}

// BackendSnapshot represents backend metrics at a point in time.
type BackendSnapshot struct {
	ID              int64     `json:"id"`
	BoxID           string    `json:"box_id"`
	BackendName     string    `json:"backend_name"`
	CollectedAt     time.Time `json:"collected_at"`
	Status          string    `json:"status"`
	HealthPercent   float64   `json:"health_percent"`
	ActiveServers   int64     `json:"active_servers"`
	TotalServers    int64     `json:"total_servers"`
	CurrentSessions int64     `json:"current_sessions"`
	ResponseTime    int64     `json:"response_time"`
	QueueCurrent    int64     `json:"queue_current"`
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	Response5xx     int64     `json:"response_5xx"`
}

// SystemMetricsSnapshot represents system metrics at a point in time.
type SystemMetricsSnapshot struct {
	ID                 int64     `json:"id"`
	BoxID              string    `json:"box_id"`
	CollectedAt        time.Time `json:"collected_at"`
	LoadAverage1       float64   `json:"load_average_1"`
	LoadAverage5       float64   `json:"load_average_5"`
	LoadAverage15      float64   `json:"load_average_15"`
	MemoryUsagePercent float64   `json:"memory_usage_percent"`
	DiskUsagePercent   float64   `json:"disk_usage_percent"`
	NetworkRxBytes     int64     `json:"network_rx_bytes"`
	NetworkTxBytes     int64     `json:"network_tx_bytes"`
}

// Incident represents a notable event or issue.
type Incident struct {
	ID           int64      `json:"id"`
	BoxID        string     `json:"box_id"`
	BackendName  *string    `json:"backend_name,omitempty"`
	IncidentType string     `json:"incident_type"`
	Severity     string     `json:"severity"`
	Message      string     `json:"message"`
	StartedAt    time.Time  `json:"started_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
}

// SaveStatsSnapshot saves a stats snapshot to the database.
func (d *DB) SaveStatsSnapshot(boxID string, stats *models.HAProxyStats) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Calculate aggregated metrics
	var totalServers, healthyServers int64
	var totalSessions, totalRequests, totalBytesIn, totalBytesOut int64
	var totalResponseTime, responseTimeCount int64
	var total5xx int64

	for _, backend := range stats.Backends {
		totalServers += backend.TotalServers
		healthyServers += backend.ActiveServers
		totalSessions += backend.CurrentSessions
		totalRequests += backend.RequestTotal
		totalBytesIn += backend.BytesIn
		totalBytesOut += backend.BytesOut
		total5xx += backend.Response5xx
		if backend.ResponseTime > 0 {
			totalResponseTime += backend.ResponseTime
			responseTimeCount++
		}
	}

	for _, frontend := range stats.Frontends {
		totalRequests += frontend.RequestTotal
		totalBytesIn += frontend.BytesIn
		totalBytesOut += frontend.BytesOut
	}

	avgResponseTime := int64(0)
	if responseTimeCount > 0 {
		avgResponseTime = totalResponseTime / responseTimeCount
	}

	_, err := d.db.Exec(`
		INSERT INTO stats_history (
			box_id, collected_at, total_frontends, total_backends,
			total_servers, healthy_servers, total_sessions, total_requests,
			total_bytes_in, total_bytes_out, avg_response_time, total_5xx_errors
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boxID, stats.ParsedAt, len(stats.Frontends), len(stats.Backends),
		totalServers, healthyServers, totalSessions, totalRequests,
		totalBytesIn, totalBytesOut, avgResponseTime, total5xx,
	)
	return err
}

// SaveBackendSnapshots saves individual backend metrics.
func (d *DB) SaveBackendSnapshots(boxID string, stats *models.HAProxyStats) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO backend_history (
			box_id, backend_name, collected_at, status, health_percent,
			active_servers, total_servers, current_sessions, response_time,
			queue_current, bytes_in, bytes_out, response_5xx
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, backend := range stats.Backends {
		_, err := stmt.Exec(
			boxID, backend.Name, stats.ParsedAt, backend.Status, backend.HealthPercent,
			backend.ActiveServers, backend.TotalServers, backend.CurrentSessions, backend.ResponseTime,
			backend.QueueCurrent, backend.BytesIn, backend.BytesOut, backend.Response5xx,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SaveSystemMetricsSnapshot saves system metrics to the database.
func (d *DB) SaveSystemMetricsSnapshot(boxID string, metrics *models.SystemMetrics) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO system_metrics_history (
			box_id, collected_at, load_average_1, load_average_5, load_average_15,
			memory_usage_percent, disk_usage_percent, network_rx_bytes, network_tx_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boxID, metrics.CollectedAt, metrics.LoadAverage1, metrics.LoadAverage5, metrics.LoadAverage15,
		metrics.MemoryUsagePercent, metrics.DiskUsagePercent, metrics.NetworkRxBytes, metrics.NetworkTxBytes,
	)
	return err
}

// GetStatsHistory retrieves stats history for a time range.
func (d *DB) GetStatsHistory(boxID string, since time.Time, limit int) ([]StatsSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, collected_at, total_frontends, total_backends,
			total_servers, healthy_servers, total_sessions, total_requests,
			total_bytes_in, total_bytes_out, avg_response_time, total_5xx_errors
		FROM stats_history
		WHERE box_id = ? AND collected_at >= ?
		ORDER BY collected_at DESC
		LIMIT ?`,
		boxID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var snapshots []StatsSnapshot
	for rows.Next() {
		var s StatsSnapshot
		err := rows.Scan(
			&s.ID, &s.BoxID, &s.CollectedAt, &s.TotalFrontends, &s.TotalBackends,
			&s.TotalServers, &s.HealthyServers, &s.TotalSessions, &s.TotalRequests,
			&s.TotalBytesIn, &s.TotalBytesOut, &s.AvgResponseTime, &s.Total5xxErrors,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	// Reverse to get chronological order
	for i, j := 0, len(snapshots)-1; i < j; i, j = i+1, j-1 {
		snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
	}

	return snapshots, nil
}

// GetBackendHistory retrieves backend history for a specific backend.
func (d *DB) GetBackendHistory(boxID, backendName string, since time.Time, limit int) ([]BackendSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, backend_name, collected_at, status, health_percent,
			active_servers, total_servers, current_sessions, response_time,
			queue_current, bytes_in, bytes_out, response_5xx
		FROM backend_history
		WHERE box_id = ? AND backend_name = ? AND collected_at >= ?
		ORDER BY collected_at DESC
		LIMIT ?`,
		boxID, backendName, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var snapshots []BackendSnapshot
	for rows.Next() {
		var s BackendSnapshot
		err := rows.Scan(
			&s.ID, &s.BoxID, &s.BackendName, &s.CollectedAt, &s.Status, &s.HealthPercent,
			&s.ActiveServers, &s.TotalServers, &s.CurrentSessions, &s.ResponseTime,
			&s.QueueCurrent, &s.BytesIn, &s.BytesOut, &s.Response5xx,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	// Reverse to get chronological order
	for i, j := 0, len(snapshots)-1; i < j; i, j = i+1, j-1 {
		snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
	}

	return snapshots, nil
}

// GetSystemMetricsHistory retrieves system metrics history.
func (d *DB) GetSystemMetricsHistory(boxID string, since time.Time, limit int) ([]SystemMetricsSnapshot, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, collected_at, load_average_1, load_average_5, load_average_15,
			memory_usage_percent, disk_usage_percent, network_rx_bytes, network_tx_bytes
		FROM system_metrics_history
		WHERE box_id = ? AND collected_at >= ?
		ORDER BY collected_at DESC
		LIMIT ?`,
		boxID, since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var snapshots []SystemMetricsSnapshot
	for rows.Next() {
		var s SystemMetricsSnapshot
		err := rows.Scan(
			&s.ID, &s.BoxID, &s.CollectedAt, &s.LoadAverage1, &s.LoadAverage5, &s.LoadAverage15,
			&s.MemoryUsagePercent, &s.DiskUsagePercent, &s.NetworkRxBytes, &s.NetworkTxBytes,
		)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	// Reverse to get chronological order
	for i, j := 0, len(snapshots)-1; i < j; i, j = i+1, j-1 {
		snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
	}

	return snapshots, nil
}

// CreateIncident creates a new incident record.
func (d *DB) CreateIncident(boxID string, backendName *string, incidentType, severity, message string) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec(`
		INSERT INTO incidents (box_id, backend_name, incident_type, severity, message, started_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		boxID, backendName, incidentType, severity, message, time.Now(),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// ResolveIncident marks an incident as resolved.
func (d *DB) ResolveIncident(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE incidents SET resolved_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// GetActiveIncidents retrieves unresolved incidents.
func (d *DB) GetActiveIncidents(boxID string) ([]Incident, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, backend_name, incident_type, severity, message, started_at, resolved_at
		FROM incidents
		WHERE box_id = ? AND resolved_at IS NULL
		ORDER BY started_at DESC`,
		boxID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var incidents []Incident
	for rows.Next() {
		var i Incident
		err := rows.Scan(&i.ID, &i.BoxID, &i.BackendName, &i.IncidentType, &i.Severity, &i.Message, &i.StartedAt, &i.ResolvedAt)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, i)
	}
	return incidents, nil
}

// GetRecentIncidents retrieves recent incidents (both resolved and unresolved).
func (d *DB) GetRecentIncidents(boxID string, limit int) ([]Incident, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, backend_name, incident_type, severity, message, started_at, resolved_at
		FROM incidents
		WHERE box_id = ?
		ORDER BY started_at DESC
		LIMIT ?`,
		boxID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var incidents []Incident
	for rows.Next() {
		var i Incident
		err := rows.Scan(&i.ID, &i.BoxID, &i.BackendName, &i.IncidentType, &i.Severity, &i.Message, &i.StartedAt, &i.ResolvedAt)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, i)
	}
	return incidents, nil
}

// CleanupOldData removes data older than the specified duration.
func (d *DB) CleanupOldData(retention time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-retention)

	queries := []string{
		`DELETE FROM stats_history WHERE collected_at < ?`,
		`DELETE FROM backend_history WHERE collected_at < ?`,
		`DELETE FROM system_metrics_history WHERE collected_at < ?`,
		`DELETE FROM incidents WHERE resolved_at IS NOT NULL AND resolved_at < ?`,
	}

	for _, query := range queries {
		if _, err := d.db.Exec(query, cutoff); err != nil {
			return err
		}
	}

	// Vacuum to reclaim space
	_, err := d.db.Exec("VACUUM")
	return err
}

// validTableNames is a whitelist of allowed table names for statistics queries.
// This prevents SQL injection when querying table counts.
var validTableNames = map[string]bool{
	"stats_history":          true,
	"backend_history":        true,
	"system_metrics_history": true,
	"incidents":              true,
}

// GetDatabaseStats returns database statistics.
// Only queries tables from the validTableNames whitelist to prevent SQL injection.
func (d *DB) GetDatabaseStats() (map[string]int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := make(map[string]int64)
	tables := []string{"stats_history", "backend_history", "system_metrics_history", "incidents"}

	for _, table := range tables {
		// Validate table name against whitelist
		if !validTableNames[table] {
			return nil, fmt.Errorf("invalid table name: %s", table)
		}

		var count int64
		// Safe to use Sprintf here as table name is validated against whitelist above
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table) //#nosec G201 -- table name validated against whitelist
		err := d.db.QueryRow(query).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("failed to count rows in table %s: %w", table, err)
		}
		stats[table] = count
	}

	return stats, nil
}

// MetricsStorageStats holds statistics about metrics data storage.
type MetricsStorageStats struct {
	TotalRecords       int64     `json:"total_records"`
	StatsRecords       int64     `json:"stats_records"`
	BackendRecords     int64     `json:"backend_records"`
	SystemMetrics      int64     `json:"system_metrics_records"`
	OldestRecord       time.Time `json:"oldest_record"`
	NewestRecord       time.Time `json:"newest_record"`
	EstimatedSizeBytes int64     `json:"estimated_size_bytes"`
}

// GetMetricsStorageStats returns statistics about metrics data storage for a box.
func (d *DB) GetMetricsStorageStats(boxID string) (*MetricsStorageStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := &MetricsStorageStats{}

	// Get counts for each table
	var statsCount, backendCount, metricsCount int64

	err := d.db.QueryRow("SELECT COUNT(*) FROM stats_history WHERE box_id = ?", boxID).Scan(&statsCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count stats_history: %w", err)
	}
	stats.StatsRecords = statsCount

	err = d.db.QueryRow("SELECT COUNT(*) FROM backend_history WHERE box_id = ?", boxID).Scan(&backendCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count backend_history: %w", err)
	}
	stats.BackendRecords = backendCount

	err = d.db.QueryRow("SELECT COUNT(*) FROM system_metrics_history WHERE box_id = ?", boxID).Scan(&metricsCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count system_metrics_history: %w", err)
	}
	stats.SystemMetrics = metricsCount

	stats.TotalRecords = statsCount + backendCount + metricsCount

	// Get oldest and newest records
	var oldestStr, newestStr sql.NullString

	// Find oldest record across all tables
	err = d.db.QueryRow(`
		SELECT MIN(collected_at) FROM (
			SELECT MIN(collected_at) as collected_at FROM stats_history WHERE box_id = ?
			UNION ALL
			SELECT MIN(collected_at) FROM backend_history WHERE box_id = ?
			UNION ALL
			SELECT MIN(collected_at) FROM system_metrics_history WHERE box_id = ?
		)
	`, boxID, boxID, boxID).Scan(&oldestStr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get oldest record: %w", err)
	}
	if oldestStr.Valid && oldestStr.String != "" {
		stats.OldestRecord, _ = time.Parse("2006-01-02 15:04:05", oldestStr.String)
	}

	// Find newest record across all tables
	err = d.db.QueryRow(`
		SELECT MAX(collected_at) FROM (
			SELECT MAX(collected_at) as collected_at FROM stats_history WHERE box_id = ?
			UNION ALL
			SELECT MAX(collected_at) FROM backend_history WHERE box_id = ?
			UNION ALL
			SELECT MAX(collected_at) FROM system_metrics_history WHERE box_id = ?
		)
	`, boxID, boxID, boxID).Scan(&newestStr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get newest record: %w", err)
	}
	if newestStr.Valid && newestStr.String != "" {
		stats.NewestRecord, _ = time.Parse("2006-01-02 15:04:05", newestStr.String)
	}

	// Estimate size (rough estimate based on typical row sizes)
	// stats_history: ~150 bytes/row, backend_history: ~200 bytes/row, system_metrics_history: ~100 bytes/row
	stats.EstimatedSizeBytes = statsCount*150 + backendCount*200 + metricsCount*100

	return stats, nil
}

// ClearMetricsData removes all metrics data for a box.
func (d *DB) ClearMetricsData(boxID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	queries := []string{
		`DELETE FROM stats_history WHERE box_id = ?`,
		`DELETE FROM backend_history WHERE box_id = ?`,
		`DELETE FROM system_metrics_history WHERE box_id = ?`,
	}

	for _, query := range queries {
		if _, err := d.db.Exec(query, boxID); err != nil {
			return fmt.Errorf("failed to clear metrics data: %w", err)
		}
	}

	// Vacuum to reclaim space
	if _, err := d.db.Exec("VACUUM"); err != nil {
		d.logger.Warn("failed to vacuum after clearing metrics", "error", err)
	}

	return nil
}

// CleanupMetricsByAge removes metrics data older than the specified duration for a box.
func (d *DB) CleanupMetricsByAge(boxID string, retention time.Duration) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-retention)
	var totalDeleted int64

	queries := []string{
		`DELETE FROM stats_history WHERE box_id = ? AND collected_at < ?`,
		`DELETE FROM backend_history WHERE box_id = ? AND collected_at < ?`,
		`DELETE FROM system_metrics_history WHERE box_id = ? AND collected_at < ?`,
	}

	for _, query := range queries {
		result, err := d.db.Exec(query, boxID, cutoff)
		if err != nil {
			return totalDeleted, fmt.Errorf("failed to cleanup metrics: %w", err)
		}
		deleted, _ := result.RowsAffected()
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// CleanupMetricsBySize removes oldest metrics data to keep estimated size under the specified limit.
func (d *DB) CleanupMetricsBySize(boxID string, maxSizeMB int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get current stats
	var statsCount, backendCount, metricsCount int64
	_ = d.db.QueryRow("SELECT COUNT(*) FROM stats_history WHERE box_id = ?", boxID).Scan(&statsCount)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM backend_history WHERE box_id = ?", boxID).Scan(&backendCount)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM system_metrics_history WHERE box_id = ?", boxID).Scan(&metricsCount)

	// Estimate current size
	currentSizeBytes := statsCount*150 + backendCount*200 + metricsCount*100
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	if currentSizeBytes <= maxSizeBytes {
		return 0, nil // Already under limit
	}

	// Calculate how much to delete (aim to get to 80% of max)
	targetSizeBytes := int64(float64(maxSizeBytes) * 0.8)
	bytesToDelete := currentSizeBytes - targetSizeBytes

	// Delete oldest records proportionally from each table
	var totalDeleted int64
	totalRecords := statsCount + backendCount + metricsCount
	if totalRecords == 0 {
		return 0, nil
	}

	// Delete from stats_history
	if statsCount > 0 {
		deleteCount := int64(float64(bytesToDelete) * float64(statsCount) / float64(currentSizeBytes) / 150)
		if deleteCount > 0 {
			result, err := d.db.Exec(`
				DELETE FROM stats_history WHERE box_id = ? AND id IN (
					SELECT id FROM stats_history WHERE box_id = ? ORDER BY collected_at ASC LIMIT ?
				)
			`, boxID, boxID, deleteCount)
			if err == nil {
				deleted, _ := result.RowsAffected()
				totalDeleted += deleted
			}
		}
	}

	// Delete from backend_history
	if backendCount > 0 {
		deleteCount := int64(float64(bytesToDelete) * float64(backendCount) / float64(currentSizeBytes) / 200)
		if deleteCount > 0 {
			result, err := d.db.Exec(`
				DELETE FROM backend_history WHERE box_id = ? AND id IN (
					SELECT id FROM backend_history WHERE box_id = ? ORDER BY collected_at ASC LIMIT ?
				)
			`, boxID, boxID, deleteCount)
			if err == nil {
				deleted, _ := result.RowsAffected()
				totalDeleted += deleted
			}
		}
	}

	// Delete from system_metrics_history
	if metricsCount > 0 {
		deleteCount := int64(float64(bytesToDelete) * float64(metricsCount) / float64(currentSizeBytes) / 100)
		if deleteCount > 0 {
			result, err := d.db.Exec(`
				DELETE FROM system_metrics_history WHERE box_id = ? AND id IN (
					SELECT id FROM system_metrics_history WHERE box_id = ? ORDER BY collected_at ASC LIMIT ?
				)
			`, boxID, boxID, deleteCount)
			if err == nil {
				deleted, _ := result.RowsAffected()
				totalDeleted += deleted
			}
		}
	}

	return totalDeleted, nil
}

// runSchemaMigrations runs any pending schema migrations using golang-migrate.
func (d *DB) runSchemaMigrations() error {
	migrateManager := NewMigrateManager(d.db, d.logger)

	// Up() logs the current version and runs migrations
	if err := migrateManager.Up(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// EntityType represents the type of entity that can be disabled.
type EntityType string

const (
	EntityTypeBackend  EntityType = "backend"
	EntityTypeFrontend EntityType = "frontend"
	EntityTypeService  EntityType = "service"
)

// DisabledEntity represents a disabled entity record.
type DisabledEntity struct {
	ID         int64      `json:"id"`
	BoxID      string     `json:"box_id"`
	EntityType EntityType `json:"entity_type"`
	EntityName string     `json:"entity_name"`
	DisabledAt time.Time  `json:"disabled_at"`
	DisabledBy *string    `json:"disabled_by,omitempty"` // UUID
	Notes      string     `json:"notes,omitempty"`
}

// DisableEntity marks an entity as disabled.
func (d *DB) DisableEntity(boxID string, entityType EntityType, entityName string, userID *string, notes string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO disabled_entities (box_id, entity_type, entity_name, disabled_at, disabled_by, notes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		boxID, entityType, entityName, time.Now(), userID, notes,
	)
	return err
}

// EnableEntity removes the disabled status from an entity.
func (d *DB) EnableEntity(boxID string, entityType EntityType, entityName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		DELETE FROM disabled_entities
		WHERE box_id = ? AND entity_type = ? AND entity_name = ?`,
		boxID, entityType, entityName,
	)
	return err
}

// IsEntityDisabled checks if an entity is disabled.
func (d *DB) IsEntityDisabled(boxID string, entityType EntityType, entityName string) (bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM disabled_entities
		WHERE box_id = ? AND entity_type = ? AND entity_name = ?`,
		boxID, entityType, entityName,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetDisabledEntities retrieves all disabled entities for a box.
func (d *DB) GetDisabledEntities(boxID string) ([]DisabledEntity, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, box_id, entity_type, entity_name, disabled_at, disabled_by, notes
		FROM disabled_entities
		WHERE box_id = ?
		ORDER BY disabled_at DESC`,
		boxID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entities []DisabledEntity
	for rows.Next() {
		var e DisabledEntity
		var notes sql.NullString
		err := rows.Scan(&e.ID, &e.BoxID, &e.EntityType, &e.EntityName, &e.DisabledAt, &e.DisabledBy, &notes)
		if err != nil {
			return nil, err
		}
		if notes.Valid {
			e.Notes = notes.String
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// GetDisabledEntity retrieves a specific disabled entity if it exists.
func (d *DB) GetDisabledEntity(boxID string, entityType EntityType, entityName string) (*DisabledEntity, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var e DisabledEntity
	var notes sql.NullString
	err := d.db.QueryRow(`
		SELECT id, box_id, entity_type, entity_name, disabled_at, disabled_by, notes
		FROM disabled_entities
		WHERE box_id = ? AND entity_type = ? AND entity_name = ?`,
		boxID, entityType, entityName,
	).Scan(&e.ID, &e.BoxID, &e.EntityType, &e.EntityName, &e.DisabledAt, &e.DisabledBy, &notes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if notes.Valid {
		e.Notes = notes.String
	}
	return &e, nil
}

// GetDisabledEntitiesByType retrieves disabled entities of a specific type for a box.
func (d *DB) GetDisabledEntitiesByType(boxID string, entityType EntityType) (map[string]bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT entity_name FROM disabled_entities
		WHERE box_id = ? AND entity_type = ?`,
		boxID, entityType,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	entities := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		entities[name] = true
	}
	return entities, nil
}
