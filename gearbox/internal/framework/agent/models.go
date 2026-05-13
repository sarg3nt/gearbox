// Package agent provides an HTTP client for the HAProxy Agent API.
package agent

import "time"

// HealthResponse represents the response from the /health endpoint.
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
}

// RuntimeInfo represents HAProxy runtime information from /api/v1/haproxy/info.
type RuntimeInfo struct {
	Version        string            `json:"version"`
	Uptime         string            `json:"uptime"`
	UptimeSec      int64             `json:"uptime_sec"`
	PID            int               `json:"pid"`
	ProcessNum     int               `json:"process_num"`
	NBThread       int               `json:"nbthread"`
	CurrConn       int64             `json:"curr_conn"`
	MaxConn        int64             `json:"maxconn"`
	CumSess        int64             `json:"cum_sess"`
	MemMaxMB       int64             `json:"mem_max_mb"`
	PoolUsedMB     int64             `json:"pool_used_mb"`
	PoolAllocMB    int64             `json:"pool_alloc_mb"`
	IdlePct        float64           `json:"idle_pct"`
	LoadAverage1   float64           `json:"load_average_1"`
	LoadAverage5   float64           `json:"load_average_5"`
	LoadAverage15  float64           `json:"load_average_15"`
	SSLFeRate      int64             `json:"ssl_fe_rate"`
	SSLBeRate      int64             `json:"ssl_be_rate"`
	SSLCacheUsage  int64             `json:"ssl_cache_usage"`
	Extra          map[string]string `json:"extra,omitempty"`
}

// StatsResponse represents the response from /api/v1/haproxy/stats (JSON format).
type StatsResponse struct {
	Frontends []FrontendStats `json:"frontends"`
	Backends  []BackendStats  `json:"backends"`
	ParsedAt  string          `json:"parsed_at"`
}

// FrontendStats represents statistics for a single frontend.
type FrontendStats struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Scur     int64  `json:"scur"`
	Smax     int64  `json:"smax"`
	Stot     int64  `json:"stot"`
	ReqTot   int64  `json:"req_tot"`
	Bin      int64  `json:"bin"`
	Bout     int64  `json:"bout"`
	Dreq     int64  `json:"dreq"`
	Dresp    int64  `json:"dresp"`
	Hrsp2xx  int64  `json:"hrsp_2xx"`
	Hrsp3xx  int64  `json:"hrsp_3xx"`
	Hrsp4xx  int64  `json:"hrsp_4xx"`
	Hrsp5xx  int64  `json:"hrsp_5xx"`
}

// BackendStats represents statistics for a single backend.
type BackendStats struct {
	Name          string              `json:"name"`
	Status        string              `json:"status"`
	Servers       []BackendServerStats `json:"servers"`
	Scur          int64               `json:"scur"`
	Qcur          int64               `json:"qcur"`
	Act           int64               `json:"act"`
	Bck           int64               `json:"bck"`
	HealthPercent float64             `json:"health_percent"`
}

// BackendServerStats represents statistics for an individual server in a backend.
type BackendServerStats struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Scur   int64  `json:"scur"`
	Qcur   int64  `json:"qcur"`
	Weight int64  `json:"weight"`
}

// StickTablesResponse represents the response from /api/v1/haproxy/tables.
type StickTablesResponse struct {
	Tables []StickTableInfo `json:"tables"`
}

// StickTableInfo represents information about a stick table.
type StickTableInfo struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Size      int64             `json:"size"`
	Used      int64             `json:"used"`
	DataTypes []string          `json:"data_types"`
	Entries   []StickTableEntry `json:"entries,omitempty"`
}

// StickTableEntry represents an entry in a stick table.
type StickTableEntry struct {
	Key  string                 `json:"key"`
	Data map[string]interface{} `json:"data"`
}

// ConfigValidationResponse represents the response from /api/v1/haproxy/validate.
type ConfigValidationResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

// MetricsResponse represents the response from /api/v1/metrics.
type MetricsResponse struct {
	Metrics  SystemMetrics   `json:"metrics"`
	Services []ServiceStatus `json:"services"`
}

// SystemMetrics represents system-level metrics.
type SystemMetrics struct {
	CPUCount            int       `json:"cpu_count"`
	LoadAverage1        float64   `json:"load_average_1"`
	LoadAverage5        float64   `json:"load_average_5"`
	LoadAverage15       float64   `json:"load_average_15"`
	MemoryTotalBytes    int64     `json:"memory_total_bytes"`
	MemoryUsedBytes     int64     `json:"memory_used_bytes"`
	MemoryFreeBytes     int64     `json:"memory_free_bytes"`
	MemoryUsagePercent  float64   `json:"memory_usage_percent"`
	DiskTotalBytes      int64     `json:"disk_total_bytes"`
	DiskUsedBytes       int64     `json:"disk_used_bytes"`
	DiskFreeBytes       int64     `json:"disk_free_bytes"`
	DiskUsagePercent    float64   `json:"disk_usage_percent"`
	NetworkRxBytes      int64     `json:"network_rx_bytes"`
	NetworkTxBytes      int64     `json:"network_tx_bytes"`
	NetworkRxMbps       float64   `json:"network_rx_mbps"`
	NetworkTxMbps       float64   `json:"network_tx_mbps"`
	NetworkRxPercent    float64   `json:"network_rx_percent"`
	NetworkTxPercent    float64   `json:"network_tx_percent"`
	CollectedAt         time.Time `json:"collected_at"`
}

// ServiceStatus represents the status of a systemd service.
type ServiceStatus struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Active      bool   `json:"active"`
	Description string `json:"description,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

// ServicesResponse represents the response from /api/v1/services.
type ServicesResponse struct {
	Services []ServiceStatus `json:"services"`
}

// AvailableServicesResponse represents the response from /api/v1/services/available.
type AvailableServicesResponse struct {
	Services []string `json:"services"`
}

// LogSourcesResponse represents the response from /api/v1/logs.
type LogSourcesResponse struct {
	Sources []LogSource `json:"sources"`
}

// LogSource represents an available log source.
type LogSource struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Unit     string `json:"unit,omitempty"`
	FilePath string `json:"file_path,omitempty"`
	Filter   string `json:"filter,omitempty"`
	Priority string `json:"priority,omitempty"`
}

// LogResponse represents the response from /api/v1/logs/{name}.
type LogResponse struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Content string `json:"content"`
}

// SecuritySummary represents the response from /api/v1/security/summary.
type SecuritySummary struct {
	Fail2Ban Fail2BanSummary `json:"fail2ban"`
	Firewall FirewallSummary `json:"firewall"`
}

// Fail2BanSummary represents a brief summary of fail2ban status.
type Fail2BanSummary struct {
	Available   bool   `json:"available"`
	Running     bool   `json:"running"`
	TotalBanned int    `json:"total_banned"`
	JailCount   int    `json:"jail_count"`
	Error       string `json:"error,omitempty"`
}

// FirewallSummary represents a brief summary of firewall status.
type FirewallSummary struct {
	Available    bool   `json:"available"`
	Running      bool   `json:"running"`
	RecentBlocks int    `json:"recent_blocks"`
	Error        string `json:"error,omitempty"`
}

// Fail2BanStats represents detailed fail2ban statistics from /api/v1/security/fail2ban.
type Fail2BanStats struct {
	Available   bool        `json:"available"`
	Running     bool        `json:"running"`
	TotalBanned int         `json:"total_banned"`
	Jails       []JailStats `json:"jails"`
	RecentBans  []BanEvent  `json:"recent_bans,omitempty"`
	CollectedAt time.Time   `json:"collected_at"`
}

// JailStats represents statistics for a single fail2ban jail.
type JailStats struct {
	Name             string   `json:"name"`
	CurrentlyBanned  int      `json:"currently_banned"`
	TotalBanned      int      `json:"total_banned"`
	CurrentlyFailed  int      `json:"currently_failed"`
	TotalFailed      int      `json:"total_failed"`
	BannedIPs        []string `json:"banned_ips,omitempty"`
}

// BanEvent represents a recent ban event.
type BanEvent struct {
	IP        string `json:"ip"`
	Jail      string `json:"jail"`
	Action    string `json:"action"`
	Timestamp string `json:"timestamp"`
}

// FirewallStats represents detailed firewall statistics from /api/v1/security/firewall.
type FirewallStats struct {
	Available    bool              `json:"available"`
	Running      bool              `json:"running"`
	Tables       []TableStats      `json:"tables,omitempty"`
	BlockCounts  map[string]int64  `json:"block_counts,omitempty"`
	RecentBlocks []BlockEvent      `json:"recent_blocks,omitempty"`
	CollectedAt  time.Time         `json:"collected_at"`
}

// TableStats represents statistics for a firewall table.
type TableStats struct {
	Name   string       `json:"name"`
	Family string       `json:"family"`
	Chains []ChainStats `json:"chains,omitempty"`
}

// ChainStats represents statistics for a firewall chain.
type ChainStats struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Hook    string      `json:"hook"`
	Policy  string      `json:"policy"`
	Packets int64       `json:"packets"`
	Bytes   int64       `json:"bytes"`
	Rules   []RuleStats `json:"rules,omitempty"`
}

// RuleStats represents statistics for a firewall rule.
type RuleStats struct {
	Handle  int64  `json:"handle"`
	Expr    string `json:"expr"`
	Packets int64  `json:"packets"`
	Bytes   int64  `json:"bytes"`
}

// BlockEvent represents a recent firewall block event.
type BlockEvent struct {
	SrcIP     string `json:"src_ip"`
	DstIP     string `json:"dst_ip"`
	SrcPort   int    `json:"src_port"`
	DstPort   int    `json:"dst_port"`
	Interface string `json:"interface"`
	Protocol  string `json:"protocol"`
	Action    string `json:"action"`
	Timestamp string `json:"timestamp"`
}

// SyncStatusResponse represents the response from /api/v1/sync/status.
type SyncStatusResponse struct {
	LastSyncTime string `json:"last_sync_time"`
	BackendCount int    `json:"backend_count"`
	LastError    string `json:"last_error,omitempty"`
}

// MetadataResponse represents the response from /api/v1/metadata.
type MetadataResponse struct {
	Metadata     Metadata `json:"metadata"`
	LastSyncTime string   `json:"last_sync_time"`
	LastError    string   `json:"last_error,omitempty"`
}

// Metadata represents HAProxy configuration metadata.
type Metadata struct {
	Version     string             `json:"version"`
	GeneratedAt string             `json:"generated_at"`
	Frontends   []FrontendMetadata `json:"frontends"`
	Backends    []BackendMetadata  `json:"backends"`
}

// FrontendMetadata represents metadata for a frontend.
type FrontendMetadata struct {
	Name     string   `json:"name"`
	Backends []string `json:"backends"`
}

// BackendMetadata represents metadata for a backend.
type BackendMetadata struct {
	Name       string              `json:"name"`
	Frontend   string              `json:"frontend"`
	Hostname   string              `json:"hostname"`
	AppName    string              `json:"app_name"`
	Public     bool                `json:"public"`
	SSLEnabled bool                `json:"ssl_enabled"`
	RateLimit  int                 `json:"rate_limit"`
	SSLVerify  string              `json:"ssl_verify"`
	Servers    []string            `json:"servers"`
	ACLs       []ACLMetadata       `json:"acls,omitempty"`
	Containers []ContainerMetadata `json:"containers,omitempty"`
}

// ACLMetadata represents an ACL applied to a backend.
type ACLMetadata struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ContainerMetadata represents container information for a backend.
type ContainerMetadata struct {
	Name        string   `json:"name"`
	Image       string   `json:"image,omitempty"`
	NetworkMode string   `json:"network_mode,omitempty"`
	IsBackend   bool     `json:"is_backend,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

// WSTokenResponse represents the response from /api/v1/events/token.
type WSTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// WSInfoResponse represents the response from /api/v1/events/info.
type WSInfoResponse struct {
	Enabled     bool     `json:"enabled"`
	Endpoint    string   `json:"endpoint"`
	EventTypes  []string `json:"event_types"`
	Subscribers int      `json:"subscribers"`
}

// WSEvent represents a real-time event received via WebSocket.
type WSEvent struct {
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// WebhookInfoResponse represents the response from /api/v1/webhook/info.
type WebhookInfoResponse struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Message    string `json:"message"`
}

// CertificatesResponse represents the response from /api/v1/certificates.
type CertificatesResponse struct {
	Certificates []Certificate `json:"certificates"`

	// New fields for certbot support
	CertManager        string `json:"cert_manager"`                   // "certbot", "acme.sh", "none"
	CertManagerVersion string `json:"cert_manager_version,omitempty"` // Version string
	CertManagerHome    string `json:"cert_manager_home,omitempty"`    // Home directory
	AutoRenewalEnabled bool   `json:"auto_renewal_enabled"`           // Timer/cron active

	// Legacy fields for backwards compatibility (deprecated)
	AcmeshInstalled bool   `json:"acmesh_installed"`
	AcmeshVersion   string `json:"acmesh_version,omitempty"`
	AcmeshHome      string `json:"acmesh_home,omitempty"`
	CronEnabled     bool   `json:"cron_enabled"`

	CollectedAt time.Time `json:"collected_at"`
	Error       string    `json:"error,omitempty"`
}

// Certificate represents a TLS certificate from the agent.
type Certificate struct {
	Domain          string    `json:"domain"`
	CommonName      string    `json:"common_name"`
	Issuer          string    `json:"issuer"`
	NotBefore       time.Time `json:"not_before"`
	NotAfter        time.Time `json:"not_after"`
	DaysUntilExpiry int       `json:"days_until_expiry"`
	IsExpired       bool      `json:"is_expired"`
	Source          string    `json:"source"`
	FilePath        string    `json:"file_path"`
	SANs            []string  `json:"sans"`
	SerialNumber    string    `json:"serial_number"`
	Fingerprint     string    `json:"fingerprint"`
}

// RefreshCertificateResponse represents the response from /api/v1/certificates/{domain}/refresh.
type RefreshCertificateResponse struct {
	Domain    string `json:"domain"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Output    string `json:"output,omitempty"`
	Renewed   bool   `json:"renewed"`
	Installed bool   `json:"installed"`
	Reloaded  bool   `json:"reloaded"`
}

// ServiceControlRequest represents a request to control a systemd service.
type ServiceControlRequest struct {
	Service string `json:"service"`
	Action  string `json:"action"` // start, stop, restart
}

// ServiceControlResponse represents the response from service control.
type ServiceControlResponse struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Output  string `json:"output,omitempty"`
}

// TrafficResponse represents the response from /api/v1/traffic.
type TrafficResponse struct {
	CollectedAt      time.Time            `json:"collected_at"`
	TotalSources     int                  `json:"total_sources"`
	Sources          []TrafficSourceData  `json:"sources"`
	TopByRequests    []TrafficSourceData  `json:"top_by_requests"`
	TopByBytes       []TrafficSourceData  `json:"top_by_bytes"`
	TopByConnections []TrafficSourceData  `json:"top_by_connections"`
	BackendTraffic   []BackendTrafficData `json:"backend_traffic"`
	ConnectionFlows  []ConnectionFlow     `json:"connection_flows"`
	ActiveSessions   []ActiveSession      `json:"active_sessions"`
}

// ConnectionFlow represents a source IP to backend connection flow.
type ConnectionFlow struct {
	SourceIP      string `json:"source_ip"`
	Backend       string `json:"backend"`
	ActiveCount   int    `json:"active_connections"`
	TotalRequests int    `json:"total_requests"`
	BytesIn       int64  `json:"bytes_in"`
	BytesOut      int64  `json:"bytes_out"`
}

// ActiveSession represents an active connection through HAProxy.
type ActiveSession struct {
	ID         string `json:"id"`
	SourceIP   string `json:"source_ip"`
	SourcePort int    `json:"source_port"`
	Frontend   string `json:"frontend"`
	Backend    string `json:"backend"`
	Server     string `json:"server"`
	Age        int    `json:"age_seconds"`
	Idle       int    `json:"idle_seconds"`
	ReqCount   int    `json:"request_count"`
	BytesIn    int64  `json:"bytes_in"`
	BytesOut   int64  `json:"bytes_out"`
}

// TrafficSourceData represents traffic data for a source IP.
type TrafficSourceData struct {
	IPAddress       string    `json:"ip_address"`
	Country         string    `json:"country,omitempty"`
	CountryCode     string    `json:"country_code,omitempty"`
	Requests        int64     `json:"requests"`
	SessionRate     int64     `json:"session_rate"`
	HTTPRequestRate int64     `json:"http_request_rate"`
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	Connections     int64     `json:"connections"`
	LastSeen        time.Time `json:"last_seen"`
	Backend         string    `json:"backend,omitempty"`
	Frontend        string    `json:"frontend,omitempty"`
}

// BackendTrafficData represents aggregated traffic for a backend.
type BackendTrafficData struct {
	Name            string  `json:"name"`
	TotalRequests   int64   `json:"total_requests"`
	BytesIn         int64   `json:"bytes_in"`
	BytesOut        int64   `json:"bytes_out"`
	UniqueIPs       int     `json:"unique_ips"`
	AvgResponseTime int64   `json:"avg_response_time"`
	Response2xx     int64   `json:"response_2xx"`
	Response3xx     int64   `json:"response_3xx"`
	Response4xx     int64   `json:"response_4xx"`
	Response5xx     int64   `json:"response_5xx"`
	ErrorRate       float64 `json:"error_rate"`
}

// TrafficSummaryResponse represents the response from /api/v1/traffic/summary.
type TrafficSummaryResponse struct {
	CollectedAt      time.Time `json:"collected_at"`
	TotalRequests    int64     `json:"total_requests"`
	TotalBytesIn     int64     `json:"total_bytes_in"`
	TotalBytesOut    int64     `json:"total_bytes_out"`
	UniqueIPs        int       `json:"unique_ips"`
	AvgResponseTime  int64     `json:"avg_response_time"`
	TotalResponse2xx int64     `json:"total_response_2xx"`
	TotalResponse3xx int64     `json:"total_response_3xx"`
	TotalResponse4xx int64     `json:"total_response_4xx"`
	TotalResponse5xx int64     `json:"total_response_5xx"`
	ErrorRate        float64   `json:"error_rate"`
	BackendCount     int       `json:"backend_count"`
	FrontendCount    int       `json:"frontend_count"`
}

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int    `json:"-"`
	Message    string `json:"error"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return e.Message
}

// HAProxy Config Management Types

// ConfigSection represents a parsed section of the HAProxy config.
type ConfigSection struct {
	Type      string `json:"type"`       // "global", "defaults", "frontend", "backend", "listen"
	Name      string `json:"name"`       // Section name (empty for global/defaults)
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed, inclusive
	IsAutoGen bool   `json:"is_auto_gen"`
}

// MarkerRange represents an auto-generated section range.
type MarkerRange struct {
	Type      string `json:"type"`       // "routing" or "backend"
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed, inclusive
}

// HAProxyConfigResponse represents the response from GET /api/v1/haproxy/config.
type HAProxyConfigResponse struct {
	Content      string          `json:"content"`
	Sections     []ConfigSection `json:"sections"`
	MarkerRanges []MarkerRange   `json:"marker_ranges"`
	SHA256       string          `json:"sha256"`
	LastModified time.Time       `json:"last_modified"`
	FilePath     string          `json:"file_path"`
}

// HAProxyConfigUpdateRequest represents the request body for POST /api/v1/haproxy/config.
type HAProxyConfigUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedSHA  string `json:"expected_sha"`   // For optimistic locking
	BackupReason string `json:"backup_reason"`  // Reason for the change
	DryRun       bool   `json:"dry_run"`        // If true, validate only
}

// HAProxyConfigUpdateResponse represents the response from POST /api/v1/haproxy/config.
type HAProxyConfigUpdateResponse struct {
	Success          bool     `json:"success"`
	ValidationOutput string   `json:"validation_output"`
	BackupPath       string   `json:"backup_path,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Message          string   `json:"message"`
	NewSHA256        string   `json:"new_sha256,omitempty"`
}

// ConfigBackup represents a configuration backup file.
type ConfigBackup struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
}

// HAProxyBackupsResponse represents the response from GET /api/v1/haproxy/config/backups.
type HAProxyBackupsResponse struct {
	Backups []ConfigBackup `json:"backups"`
}

// HAProxyRestoreRequest represents the request body for POST /api/v1/haproxy/config/restore.
type HAProxyRestoreRequest struct {
	BackupID string `json:"backup_id"`
	DryRun   bool   `json:"dry_run"`
}

// HAProxyRestoreResponse represents the response from POST /api/v1/haproxy/config/restore.
type HAProxyRestoreResponse struct {
	Success          bool   `json:"success"`
	Message          string `json:"message"`
	ValidationOutput string `json:"validation_output,omitempty"`
	NewSHA256        string `json:"new_sha256,omitempty"`
}

// Firewall Config Management Types (mirrors HAProxy config types)

// FirewallConfigResponse represents the response from GET /api/v1/firewall/config.
type FirewallConfigResponse struct {
	Content      string          `json:"content"`
	Sections     []ConfigSection `json:"sections"`
	SHA256       string          `json:"sha256"`
	LastModified time.Time       `json:"last_modified"`
	FilePath     string          `json:"file_path"`
}

// FirewallConfigUpdateRequest represents the request body for POST /api/v1/firewall/config.
type FirewallConfigUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedSHA  string `json:"expected_sha"`
	BackupReason string `json:"backup_reason"`
	DryRun       bool   `json:"dry_run"`
}

// FirewallConfigUpdateResponse represents the response from POST /api/v1/firewall/config.
type FirewallConfigUpdateResponse struct {
	Success          bool     `json:"success"`
	ValidationOutput string   `json:"validation_output"`
	BackupPath       string   `json:"backup_path,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Message          string   `json:"message"`
	NewSHA256        string   `json:"new_sha256,omitempty"`
}

// FirewallBackupsResponse represents the response from GET /api/v1/firewall/config/backups.
type FirewallBackupsResponse struct {
	Backups []ConfigBackup `json:"backups"`
}

// FirewallRestoreRequest represents the request body for POST /api/v1/firewall/config/restore.
type FirewallRestoreRequest struct {
	BackupID string `json:"backup_id"`
	DryRun   bool   `json:"dry_run"`
}

// FirewallRestoreResponse represents the response from POST /api/v1/firewall/config/restore.
type FirewallRestoreResponse struct {
	Success          bool   `json:"success"`
	Message          string `json:"message"`
	ValidationOutput string `json:"validation_output,omitempty"`
	NewSHA256        string `json:"new_sha256,omitempty"`
}

// OS Updates Types

// UpdateStatusResponse represents the current OS update status.
type UpdateStatusResponse struct {
	Available        bool   `json:"available"`
	TotalUpdates     int    `json:"total_updates"`
	SecurityUpdates  int    `json:"security_updates"`
	RebootRequired   bool   `json:"reboot_required"`
	LastCheck        string `json:"last_check"`
	UnattendedActive bool   `json:"unattended_active"`
}

// Package represents an available package update.
type Package struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
	Architecture     string `json:"architecture"`
	IsSecurityUpdate bool   `json:"is_security_update"`
	IsHeld           bool   `json:"is_held"` // Held packages (apt-mark/dpkg pinned) won't be upgraded even when a newer version is available. Always serialized so the dashboard's strict-equality filter (`is_held === false`) sees the explicit value.
	Priority         string `json:"priority"`
	Repository       string `json:"repository"`
	Size             int64  `json:"size_bytes"`
	ChangelogURL     string `json:"changelog_url"`
	PackageURL       string `json:"package_url,omitempty"`
}

// PackageListResponse represents a list of packages.
type PackageListResponse struct {
	Packages []Package `json:"packages"`
	Count    int       `json:"count"`
}

// InstallUpdatesRequest represents a request to install updates.
type InstallUpdatesRequest struct {
	Packages       []string `json:"packages"`
	SecurityOnly   bool     `json:"security_only"`
	CreateSnapshot bool     `json:"create_snapshot"`
}

// InstallUpdatesResponse represents the result of installing updates.
type InstallUpdatesResponse struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	Packages   []string `json:"packages"`
	SnapshotID string   `json:"snapshot_id,omitempty"`
}

// StreamingInstallResponse represents the response for a streaming install operation.
type StreamingInstallResponse struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

// OperationStatusResponse represents the status of an apt operation.
type OperationStatusResponse struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	StartedAt    string   `json:"started_at"`
	CompletedAt  *string  `json:"completed_at,omitempty"`
	ExitCode     int      `json:"exit_code"`
	Packages     []string `json:"packages,omitempty"`
	SecurityOnly bool     `json:"security_only,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// UpdateHistoryEntry represents a past update action.
type UpdateHistoryEntry struct {
	Timestamp   string `json:"timestamp"`
	Action      string `json:"action"`
	Package     string `json:"package"`
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"`
	Status      string `json:"status"`
}

// UpdateHistoryResponse represents package update history.
type UpdateHistoryResponse struct {
	History []UpdateHistoryEntry `json:"history"`
	Count   int                  `json:"count"`
}

// UpdateLogEntry represents a persisted update operation log.
type UpdateLogEntry struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	StartedAt    string   `json:"started_at"`
	CompletedAt  string   `json:"completed_at,omitempty"`
	Packages     []string `json:"packages,omitempty"`
	SecurityOnly bool     `json:"security_only,omitempty"`
	ExitCode     int      `json:"exit_code"`
	Error        string   `json:"error,omitempty"`
	Output       string   `json:"output,omitempty"`
}

// UpdateLogsResponse represents a list of update logs.
type UpdateLogsResponse struct {
	Logs  []UpdateLogEntry `json:"logs"`
	Count int              `json:"count"`
}

// RebootRequiredResponse represents reboot status.
type RebootRequiredResponse struct {
	Required bool     `json:"required"`
	Packages []string `json:"packages"`
}

// ScheduleRebootRequest represents a reboot scheduling request.
type ScheduleRebootRequest struct {
	When string `json:"when"`
}

// ScheduleRebootResponse represents reboot scheduling result.
type ScheduleRebootResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// AptSnapshot represents an APT snapshot for rollback.
type AptSnapshot struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Reason    string `json:"reason,omitempty"`
}

// SnapshotsResponse represents available snapshots.
type SnapshotsResponse struct {
	Snapshots []AptSnapshot `json:"snapshots"`
	Count     int           `json:"count"`
}

// CreateSnapshotRequest represents a snapshot creation request.
type CreateSnapshotRequest struct {
	Reason string `json:"reason"`
}

// CreateSnapshotResponse represents snapshot creation result.
type CreateSnapshotResponse struct {
	Success  bool         `json:"success"`
	Message  string       `json:"message"`
	Snapshot *AptSnapshot `json:"snapshot,omitempty"`
}

// RestoreSnapshotRequest represents a snapshot restore request.
type RestoreSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

// RestoreSnapshotResponse represents snapshot restore result.
type RestoreSnapshotResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PackageChange represents a version change for a single package in a snapshot preview.
type PackageChange struct {
	Package        string `json:"package"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
}

// SnapshotPreviewResponse represents the changes that would be applied by restoring a snapshot.
type SnapshotPreviewResponse struct {
	SnapshotID   string          `json:"snapshot_id"`
	HasVersions  bool            `json:"has_versions"`
	Downgrades   []PackageChange `json:"downgrades"`
	Installs     []string        `json:"installs"`
	Removals     []string        `json:"removals"`
	TotalChanges int             `json:"total_changes"`
}

// InstalledPackage represents a package that can be installed.
type InstalledPackage struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Architecture     string `json:"architecture"`
	Description      string `json:"description,omitempty"`
	AutoInstall      bool   `json:"auto_install"`
	Installed        bool   `json:"installed,omitempty"`
	UpdateAvailable  bool   `json:"update_available,omitempty"`
	AvailableVersion string `json:"available_version,omitempty"`
	IsSecurityUpdate bool   `json:"is_security_update,omitempty"`
	IsHeld           bool   `json:"is_held"` // Always serialized — see note on Package.IsHeld above.
	PackageURL       string `json:"package_url,omitempty"`
}

// HoldPackageRequest represents a package hold/unhold request.
type HoldPackageRequest struct {
	Name string `json:"name"`
}

// HoldPackageResponse represents the result of a hold/unhold operation.
type HoldPackageResponse struct {
	Success bool   `json:"success"`
	Name    string `json:"name"`
	Held    bool   `json:"held"`
}


// InstalledPackagesResponse wraps the list of installed packages.
type InstalledPackagesResponse struct {
	Packages []InstalledPackage `json:"packages"`
	Count    int                `json:"count"`
}

// PackageSearchResponse represents package search results.
type PackageSearchResponse struct {
	Packages []InstalledPackage `json:"packages"`
	Count    int                `json:"count"`
}

// InstallPackageRequest represents a package installation request.
type InstallPackageRequest struct {
	Name string `json:"name"`
}

// InstallPackageResponse represents package installation result.
type InstallPackageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Package string `json:"package"`
}

// RemovePackageRequest represents a package removal request.
type RemovePackageRequest struct {
	Name  string `json:"name"`
	Purge bool   `json:"purge"`
}

// PipxPackage represents a pipx-installed Python package.
type PipxPackage struct {
	Name            string   `json:"name"`
	Version         string   `json:"version"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	UpdateAvailable bool     `json:"update_available,omitempty"`
	PythonPath      string   `json:"python_path,omitempty"`
	Apps            []string `json:"apps,omitempty"`
}

// PipxStatusResponse represents pipx availability status.
type PipxStatusResponse struct {
	Available bool          `json:"available"`
	Packages  []PipxPackage `json:"packages,omitempty"`
	Count     int           `json:"count"`
}

// PipxPackageRequest represents a pipx package operation request.
type PipxPackageRequest struct {
	Name string `json:"name"`
}

// PipxPackageResponse represents a pipx operation result.
type PipxPackageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Package string `json:"package"`
}

// PipStatusResponse represents pip availability status.
type PipStatusResponse struct {
	Available bool          `json:"available"`
	Packages  []PipxPackage `json:"packages,omitempty"`
	Count     int           `json:"count"`
}

// PythonToolsStatusResponse represents combined pip + pipx status.
type PythonToolsStatusResponse struct {
	Pip  PipStatusResponse  `json:"pip"`
	Pipx PipxStatusResponse `json:"pipx"`
}

// UnattendedConfigResponse represents unattended-upgrades configuration.
type UnattendedConfigResponse struct {
	Enabled     bool `json:"enabled"`
	AutoUpdate  bool `json:"auto_update"`
	AutoUpgrade bool `json:"auto_upgrade"`
	AutoReboot  bool `json:"auto_reboot"`
	MailOnError bool `json:"mail_on_error"`
}

// ConfigureUnattendedRequest represents unattended-upgrades configuration request.
type ConfigureUnattendedRequest struct {
	Enabled    bool `json:"enabled"`
	AutoReboot bool `json:"auto_reboot"`
}
