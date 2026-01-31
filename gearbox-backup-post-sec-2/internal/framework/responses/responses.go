// Package responses provides type-safe response structures for API endpoints.
//
// This package defines strongly-typed response structures for all API endpoints,
// replacing the use of map[string]any with explicit struct types. This provides:
//   - Type safety at compile time
//   - Clear API contracts
//   - Better IDE autocomplete support
//   - Easier API documentation generation
//   - Reduced risk of missing or misnamed fields
//
// All response types are JSON-serializable and follow consistent naming conventions.
//
// Example usage:
//
//	func (h *Handler) APIStatsHandler(w http.ResponseWriter, r *http.Request) {
//	    stats, updatedAt, fresh := collector.GetStats()
//	    response := responses.StatsResponse{
//	        Stats:     stats,
//	        UpdatedAt: updatedAt,
//	        Fresh:     fresh,
//	    }
//	    h.writeJSON(w, response)
//	}
package responses

import (
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// StatsResponse represents the response for HAProxy stats endpoint.
type StatsResponse struct {
	Stats     *models.HAProxyStats `json:"stats"`
	UpdatedAt time.Time            `json:"updated_at"`
	Fresh     bool                 `json:"fresh"`
}

// MetadataResponse represents the response for metadata endpoint.
type MetadataResponse struct {
	Metadata  *models.Metadata `json:"metadata"`
	UpdatedAt time.Time        `json:"updated_at"`
	Fresh     bool             `json:"fresh"`
}

// SystemMetricsResponse represents the response for system metrics endpoint.
type SystemMetricsResponse struct {
	Metrics   *models.SystemMetrics `json:"metrics"`
	UpdatedAt time.Time             `json:"updated_at"`
	Fresh     bool                  `json:"fresh"`
}

// LogsResponse represents the response for logs endpoint.
type LogsResponse struct {
	Logs      string    `json:"logs"`
	UpdatedAt time.Time `json:"updated_at"`
	Fresh     bool      `json:"fresh"`
}

// ServerInfo represents server information in the servers list.
type ServerInfo struct {
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"`
}

// ServersResponse represents the response for servers list endpoint.
type ServersResponse struct {
	Servers []ServerInfo `json:"servers"`
}

// BackendHistoryItem represents a single backend history entry.
type BackendHistoryItem struct {
	BackendName string                 `json:"backend_name"`
	Timestamp   time.Time              `json:"timestamp"`
	Metrics     map[string]any `json:"metrics"`
}

// BackendHistoryResponse represents the response for backend history endpoint.
type BackendHistoryResponse struct {
	History []BackendHistoryItem `json:"history"`
}

// TrafficSourceItem represents traffic data for a single source.
type TrafficSourceItem struct {
	Source          string  `json:"source"`
	Requests        int64   `json:"requests"`
	BytesIn         int64   `json:"bytes_in"`
	BytesOut        int64   `json:"bytes_out"`
	AvgResponseTime float64 `json:"avg_response_time,omitempty"`
}

// TrafficBackendItem represents traffic data for a single backend.
type TrafficBackendItem struct {
	Backend         string  `json:"backend"`
	Requests        int64   `json:"requests"`
	BytesIn         int64   `json:"bytes_in"`
	BytesOut        int64   `json:"bytes_out"`
	AvgResponseTime float64 `json:"avg_response_time,omitempty"`
	ActiveServers   int     `json:"active_servers,omitempty"`
}

// TrafficDataResponse represents the response for traffic data endpoint.
type TrafficDataResponse struct {
	Sources  []TrafficSourceItem  `json:"sources"`
	Backends []TrafficBackendItem `json:"backends"`
}

// CertificateInfo represents SSL certificate information.
type CertificateInfo struct {
	Domain      string    `json:"domain"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	DaysLeft    int       `json:"days_left"`
	IsValid     bool      `json:"is_valid"`
	SNI         string    `json:"sni,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
}

// CertificatesResponse represents the response for certificates endpoint.
type CertificatesResponse struct {
	Certificates []CertificateInfo `json:"certificates"`
}

// ServiceInfo represents service status information.
type ServiceInfo struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Active      bool   `json:"active"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

// ServicesResponse represents the response for services endpoint.
type ServicesResponse struct {
	Services []ServiceInfo `json:"services"`
}

// ServiceControlResponse represents the response for service control actions.
type ServiceControlResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}

// ErrorResponse represents a standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// SuccessResponse represents a standard success response.
type SuccessResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// BandwidthConfig represents bandwidth configuration.
type BandwidthConfig struct {
	Unit         string `json:"unit"`
	MaxBandwidth int64  `json:"max_bandwidth"`
}

// HistoricalData represents historical data configuration.
type HistoricalData struct {
	Enabled   bool   `json:"enabled"`
	TimeRange string `json:"time_range"`
}

// ConfigResponse represents configuration data response.
type ConfigResponse struct {
	Config           map[string]any `json:"config"`
	BandwidthConfig  BandwidthConfig        `json:"bandwidth_config"`
	Historical       HistoricalData         `json:"historical"`
	BackendID        string                 `json:"backend_id,omitempty"`
	SourceID         string                 `json:"source_id,omitempty"`
	AvailableMetrics []string               `json:"available_metrics,omitempty"`
}

// DatabaseStatsResponse represents database statistics.
type DatabaseStatsResponse struct {
	Stats map[string]int64 `json:"stats"`
}

// Alert represents an alert with its details.
type Alert struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	Severity    string                 `json:"severity"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Acknowledged bool                  `json:"acknowledged"`
	AckBy       string                 `json:"ack_by,omitempty"`
	AckAt       *time.Time             `json:"ack_at,omitempty"`
}

// AlertsResponse represents a list of alerts.
type AlertsResponse struct {
	Alerts []Alert `json:"alerts"`
	Total  int     `json:"total"`
}

// AlertSummary represents alert summary statistics.
type AlertSummary struct {
	Total        int `json:"total"`
	Critical     int `json:"critical"`
	Warning      int `json:"warning"`
	Info         int `json:"info"`
	Acknowledged int `json:"acknowledged"`
	Active       int `json:"active"`
}

// AlertSummaryResponse represents the alert summary response.
type AlertSummaryResponse struct {
	Summary AlertSummary `json:"summary"`
}

// SecuritySummary represents security summary statistics.
type SecuritySummary struct {
	BlockedIPs     int       `json:"blocked_ips"`
	RecentAttacks  int       `json:"recent_attacks"`
	Fail2BanActive bool      `json:"fail2ban_active"`
	LastUpdate     time.Time `json:"last_update"`
}

// SecuritySummaryResponse represents the security summary response.
type SecuritySummaryResponse struct {
	Summary SecuritySummary `json:"summary"`
}

// Fail2BanStats represents Fail2Ban statistics.
type Fail2BanStats struct {
	ActiveJails   int                    `json:"active_jails"`
	BannedIPs     int                    `json:"banned_ips"`
	JailStats     map[string]any `json:"jail_stats"`
	ServiceStatus string                 `json:"service_status"`
}

// Fail2BanStatsResponse represents Fail2Ban stats response.
type Fail2BanStatsResponse struct {
	Stats Fail2BanStats `json:"stats"`
}

// BackupInfo represents backup file information.
type BackupInfo struct {
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Hash      string    `json:"hash,omitempty"`
}

// BackupsResponse represents list of backups.
type BackupsResponse struct {
	Backups []BackupInfo `json:"backups"`
}

// ConfigHistoryItem represents a configuration history entry.
type ConfigHistoryItem struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user"`
	Action    string    `json:"action"`
	Summary   string    `json:"summary"`
}

// ConfigHistoryResponse represents configuration history.
type ConfigHistoryResponse struct {
	History []ConfigHistoryItem `json:"history"`
}

// ValidationResult represents configuration validation result.
type ValidationResult struct {
	Valid   bool     `json:"valid"`
	Errors  []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ValidationResponse represents validation result response.
type ValidationResponse struct {
	Result ValidationResult `json:"result"`
}
