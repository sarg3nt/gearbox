package collector

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// StatsCollectorInterface defines the interface for stats collection.
type StatsCollectorInterface interface {
	Collect() (*models.HAProxyStats, error)
}

// MetadataCollectorInterface defines the interface for metadata collection.
type MetadataCollectorInterface interface {
	Collect() (*models.Metadata, error)
}

// SystemCollectorInterface defines the interface for system metrics collection.
type SystemCollectorInterface interface {
	Collect() (*models.SystemMetrics, error)
}

// LogCollectorInterface defines the interface for log collection.
type LogCollectorInterface interface {
	CollectLog(logName string, lines int) (string, error)
}

// Manager coordinates all data collection for a server.
type Manager struct {
	serverID          string
	cache             *Cache
	statsCollector    StatsCollectorInterface
	metadataCollector MetadataCollectorInterface
	systemCollector   SystemCollectorInterface
	logCollector      LogCollectorInterface
	agentClient       *agent.Client // Direct access to agent client for security operations
	logger            *slog.Logger
	db                *database.DB
	stopCh            chan struct{}
	wg                sync.WaitGroup
}

// NewManager creates a new collector manager using the Agent API.
func NewManager(
	serverID string,
	agentClient *agent.Client,
	statsTTL, metadataTTL, metricsTTL, logsTTL time.Duration,
	logger *slog.Logger,
	db *database.DB,
) *Manager {
	cache := NewCache(statsTTL, metadataTTL, metricsTTL, logsTTL)

	statsCollector := NewAgentStatsCollector(agentClient)
	metadataCollector := NewAgentMetadataCollector(agentClient)
	systemCollector := NewAgentSystemCollector(agentClient)
	logCollector := NewAgentLogCollector(agentClient)

	return &Manager{
		serverID:          serverID,
		cache:             cache,
		statsCollector:    statsCollector,
		metadataCollector: metadataCollector,
		systemCollector:   systemCollector,
		logCollector:      logCollector,
		agentClient:       agentClient,
		logger:            logger,
		db:                db,
		stopCh:            make(chan struct{}),
	}
}

// Start begins background data collection.
func (m *Manager) Start(statsInterval, metadataInterval, metricsInterval, historyInterval time.Duration) {
	m.logger.Info("starting data collection", "server_id", m.serverID)

	// Start stats collection
	m.wg.Add(1)
	go m.collectStatsLoop(statsInterval)

	// Start metadata collection
	m.wg.Add(1)
	go m.collectMetadataLoop(metadataInterval)

	// Start system metrics collection
	m.wg.Add(1)
	go m.collectSystemMetricsLoop(metricsInterval)

	// Start history persistence loop
	if m.db != nil {
		m.wg.Add(1)
		go m.persistHistoryLoop(historyInterval)
	}
}

// Stop halts all background collection.
func (m *Manager) Stop() {
	m.logger.Info("stopping data collection", "server_id", m.serverID)
	close(m.stopCh)
	m.wg.Wait()
}

// GetCache returns the cache for direct access.
func (m *Manager) GetCache() *Cache {
	return m.cache
}

// collectStatsLoop periodically collects HAProxy stats.
func (m *Manager) collectStatsLoop(interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect immediately on start
	m.collectStats()

	for {
		select {
		case <-ticker.C:
			m.collectStats()
		case <-m.stopCh:
			return
		}
	}
}

// collectMetadataLoop periodically collects metadata.
func (m *Manager) collectMetadataLoop(interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect immediately on start
	m.collectMetadata()

	for {
		select {
		case <-ticker.C:
			m.collectMetadata()
		case <-m.stopCh:
			return
		}
	}
}

// collectSystemMetricsLoop periodically collects system metrics.
func (m *Manager) collectSystemMetricsLoop(interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect immediately on start
	m.collectSystemMetrics()

	for {
		select {
		case <-ticker.C:
			m.collectSystemMetrics()
		case <-m.stopCh:
			return
		}
	}
}

// persistHistoryLoop periodically saves stats to the database.
func (m *Manager) persistHistoryLoop(interval time.Duration) {
	defer m.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.persistHistory()
		case <-m.stopCh:
			return
		}
	}
}

// persistHistory saves current stats and metrics to the database.
func (m *Manager) persistHistory() {
	if m.db == nil {
		return
	}

	// Check if metrics integration is enabled and if store_history is enabled
	metricsEnabled, err := m.db.IsGearEnabled(m.serverID, "metrics")
	if err != nil {
		m.logger.Error("failed to check metrics integration status",
			"server_id", m.serverID,
			"error", err)
		// Default to enabled if we can't check
		metricsEnabled = true
	}

	if !metricsEnabled {
		return // Metrics integration is disabled, don't store anything
	}

	// Get metrics config to check if store_history is enabled
	metricsConfig, err := m.db.GetMetricsConfig(m.serverID)
	if err != nil {
		m.logger.Error("failed to get metrics config",
			"server_id", m.serverID,
			"error", err)
		// Default to storing if we can't check config
		metricsConfig = &database.MetricsConfig{StoreHistory: true}
	}

	if !metricsConfig.StoreHistory {
		return // Historical storage is disabled
	}

	// Save stats snapshot
	stats, _, ok := m.cache.GetStats()
	if ok && stats != nil {
		if err := m.db.SaveStatsSnapshot(m.serverID, stats); err != nil {
			m.logger.Error("failed to save stats snapshot",
				"server_id", m.serverID,
				"error", err)
		}
		if err := m.db.SaveBackendSnapshots(m.serverID, stats); err != nil {
			m.logger.Error("failed to save backend snapshots",
				"server_id", m.serverID,
				"error", err)
		}
	}

	// Save system metrics snapshot
	metrics, _, ok := m.cache.GetSystemMetrics()
	if ok && metrics != nil {
		if err := m.db.SaveSystemMetricsSnapshot(m.serverID, metrics); err != nil {
			m.logger.Error("failed to save system metrics snapshot",
				"server_id", m.serverID,
				"error", err)
		}
	}
}

// collectStats fetches and caches HAProxy stats.
func (m *Manager) collectStats() {
	stats, err := m.statsCollector.Collect()
	if err != nil {
		m.logger.Error("failed to collect stats",
			"server_id", m.serverID,
			"error", err)
		return
	}

	m.cache.SetStats(stats)
}

// collectMetadata fetches and caches metadata.
func (m *Manager) collectMetadata() {
	metadata, err := m.metadataCollector.Collect()
	if err != nil {
		m.logger.Error("failed to collect metadata",
			"server_id", m.serverID,
			"error", err)
		return
	}

	m.cache.SetMetadata(metadata)
}

// collectSystemMetrics fetches and caches system metrics.
func (m *Manager) collectSystemMetrics() {
	metrics, err := m.systemCollector.Collect()
	if err != nil {
		m.logger.Error("failed to collect system metrics",
			"server_id", m.serverID,
			"error", err)
		return
	}

	m.cache.SetSystemMetrics(metrics)
}

// GetStats retrieves cached stats.
func (m *Manager) GetStats() (*models.HAProxyStats, time.Time, bool) {
	return m.cache.GetStats()
}

// GetMetadata retrieves cached metadata.
func (m *Manager) GetMetadata() (*models.Metadata, time.Time, bool) {
	return m.cache.GetMetadata()
}

// GetSystemMetrics retrieves cached system metrics.
func (m *Manager) GetSystemMetrics() (*models.SystemMetrics, time.Time, bool) {
	return m.cache.GetSystemMetrics()
}

// GetLog fetches and caches logs.
// logName is used to identify the log source via the Agent API.
func (m *Manager) GetLog(logName string, lines int) (string, error) {
	// Check cache first
	if logData, _, fresh := m.cache.GetLog(logName); fresh {
		return logData.Content, nil
	}

	// Fetch fresh logs via Agent API
	content, err := m.logCollector.CollectLog(logName, lines)
	if err != nil {
		return "", err
	}

	// Cache the result
	lineCount := len(content)
	if content != "" {
		lineCount = len([]rune(content))
	}
	m.cache.SetLog(logName, content, lineCount)

	return content, nil
}

// ForceRefresh forces immediate refresh of all data.
func (m *Manager) ForceRefresh() error {
	var errs []error

	// Collect stats
	if err := m.collectStatsWithError(); err != nil {
		errs = append(errs, fmt.Errorf("stats: %w", err))
	}

	// Collect metadata
	if err := m.collectMetadataWithError(); err != nil {
		errs = append(errs, fmt.Errorf("metadata: %w", err))
	}

	// Collect system metrics
	if err := m.collectSystemMetricsWithError(); err != nil {
		errs = append(errs, fmt.Errorf("system: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("refresh errors: %v", errs)
	}

	return nil
}

func (m *Manager) collectStatsWithError() error {
	stats, err := m.statsCollector.Collect()
	if err != nil {
		return err
	}
	m.cache.SetStats(stats)
	return nil
}

func (m *Manager) collectMetadataWithError() error {
	metadata, err := m.metadataCollector.Collect()
	if err != nil {
		return err
	}
	m.cache.SetMetadata(metadata)
	return nil
}

func (m *Manager) collectSystemMetricsWithError() error {
	metrics, err := m.systemCollector.Collect()
	if err != nil {
		return err
	}
	m.cache.SetSystemMetrics(metrics)
	return nil
}

// RefreshStats fetches fresh stats from the agent and updates the cache.
func (m *Manager) RefreshStats() error {
	return m.collectStatsWithError()
}

// RefreshMetrics fetches fresh metrics from the agent and updates the cache.
func (m *Manager) RefreshMetrics() error {
	return m.collectSystemMetricsWithError()
}

// GetSecuritySummary retrieves security summary from the agent.
func (m *Manager) GetSecuritySummary() (*agent.SecuritySummary, error) {
	if m.agentClient == nil {
		return nil, fmt.Errorf("agent client not available")
	}
	return m.agentClient.GetSecuritySummary()
}

// GetFail2BanStats retrieves fail2ban statistics from the agent.
func (m *Manager) GetFail2BanStats() (*agent.Fail2BanStats, error) {
	if m.agentClient == nil {
		return nil, fmt.Errorf("agent client not available")
	}
	return m.agentClient.GetFail2BanStats(nil)
}

// GetBlockedIPs retrieves the list of blocked IPs from the firewall.
func (m *Manager) GetBlockedIPs() ([]agent.BlockedIPInfo, error) {
	if m.agentClient == nil {
		return nil, fmt.Errorf("agent client not available")
	}
	resp, err := m.agentClient.GetBlockedIPs()
	if err != nil {
		return nil, err
	}
	return resp.BlockedIPs, nil
}

// BlockIP blocks an IP address in the firewall.
func (m *Manager) BlockIP(ip, reason string) error {
	if m.agentClient == nil {
		return fmt.Errorf("agent client not available")
	}
	_, err := m.agentClient.BlockIP(ip, reason)
	return err
}

// UnblockIP removes an IP address from the firewall blocklist.
func (m *Manager) UnblockIP(ip string) error {
	if m.agentClient == nil {
		return fmt.Errorf("agent client not available")
	}
	_, err := m.agentClient.UnblockIP(ip)
	return err
}
