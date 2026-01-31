package collector

import (
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Cache stores collected data with TTL.
type Cache struct {
	mu                    sync.RWMutex
	stats                 *models.HAProxyStats
	statsUpdatedAt        time.Time
	metadata              *models.Metadata
	metadataUpdatedAt     time.Time
	systemMetrics         *models.SystemMetrics
	metricsUpdatedAt      time.Time
	certificates          *models.CertificateMetrics
	certificatesUpdatedAt time.Time
	logs                  map[string]*LogData
	logsUpdatedAt         map[string]time.Time
	statsTTL              time.Duration
	metadataTTL           time.Duration
	metricsTTL            time.Duration
	certificatesTTL       time.Duration
	logsTTL               time.Duration
}

// LogData holds log content with metadata.
type LogData struct {
	Content string
	Lines   int
}

// NewCache creates a new cache with specified TTLs.
func NewCache(statsTTL, metadataTTL, metricsTTL, logsTTL time.Duration) *Cache {
	return &Cache{
		logs:            make(map[string]*LogData),
		logsUpdatedAt:   make(map[string]time.Time),
		statsTTL:        statsTTL,
		metadataTTL:     metadataTTL,
		metricsTTL:      metricsTTL,
		certificatesTTL: 2 * time.Minute, // Certificates update less frequently
		logsTTL:         logsTTL,
	}
}

// SetStats stores HAProxy stats in cache.
func (c *Cache) SetStats(stats *models.HAProxyStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = stats
	c.statsUpdatedAt = time.Now()
}

// GetStats retrieves HAProxy stats from cache.
func (c *Cache) GetStats() (*models.HAProxyStats, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.stats == nil {
		return nil, time.Time{}, false
	}

	// Check if expired
	if time.Since(c.statsUpdatedAt) > c.statsTTL {
		return c.stats, c.statsUpdatedAt, false // Return stale data with expired flag
	}

	return c.stats, c.statsUpdatedAt, true
}

// SetMetadata stores metadata in cache.
func (c *Cache) SetMetadata(metadata *models.Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata = metadata
	c.metadataUpdatedAt = time.Now()
}

// GetMetadata retrieves metadata from cache.
func (c *Cache) GetMetadata() (*models.Metadata, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.metadata == nil {
		return nil, time.Time{}, false
	}

	if time.Since(c.metadataUpdatedAt) > c.metadataTTL {
		return c.metadata, c.metadataUpdatedAt, false
	}

	return c.metadata, c.metadataUpdatedAt, true
}

// SetSystemMetrics stores system metrics in cache.
func (c *Cache) SetSystemMetrics(metrics *models.SystemMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemMetrics = metrics
	c.metricsUpdatedAt = time.Now()
}

// GetSystemMetrics retrieves system metrics from cache.
func (c *Cache) GetSystemMetrics() (*models.SystemMetrics, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.systemMetrics == nil {
		return nil, time.Time{}, false
	}

	if time.Since(c.metricsUpdatedAt) > c.metricsTTL {
		return c.systemMetrics, c.metricsUpdatedAt, false
	}

	return c.systemMetrics, c.metricsUpdatedAt, true
}

// SetCertificates stores certificate metrics in cache.
func (c *Cache) SetCertificates(certificates *models.CertificateMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.certificates = certificates
	c.certificatesUpdatedAt = time.Now()
}

// GetCertificates retrieves certificate metrics from cache.
func (c *Cache) GetCertificates() (*models.CertificateMetrics, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.certificates == nil {
		return nil, time.Time{}, false
	}

	if time.Since(c.certificatesUpdatedAt) > c.certificatesTTL {
		return c.certificates, c.certificatesUpdatedAt, false
	}

	return c.certificates, c.certificatesUpdatedAt, true
}

// SetLog stores log data in cache.
func (c *Cache) SetLog(logName, content string, lines int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs[logName] = &LogData{
		Content: content,
		Lines:   lines,
	}
	c.logsUpdatedAt[logName] = time.Now()
}

// GetLog retrieves log data from cache.
func (c *Cache) GetLog(logName string) (*LogData, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	logData, exists := c.logs[logName]
	if !exists {
		return nil, time.Time{}, false
	}

	updatedAt := c.logsUpdatedAt[logName]
	if time.Since(updatedAt) > c.logsTTL {
		return logData, updatedAt, false
	}

	return logData, updatedAt, true
}

// IsStale returns true if any cached data is stale.
func (c *Cache) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.stats != nil && time.Since(c.statsUpdatedAt) > c.statsTTL {
		return true
	}
	if c.metadata != nil && time.Since(c.metadataUpdatedAt) > c.metadataTTL {
		return true
	}
	if c.systemMetrics != nil && time.Since(c.metricsUpdatedAt) > c.metricsTTL {
		return true
	}

	return false
}
