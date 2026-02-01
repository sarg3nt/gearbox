package models

import "time"

// TrafficSource represents a unique traffic source (IP address).
type TrafficSource struct {
	IPAddress     string    `json:"ip_address"`
	Country       string    `json:"country"`
	CountryCode   string    `json:"country_code"`
	City          string    `json:"city,omitempty"`
	ASN           string    `json:"asn,omitempty"`
	ASNOrg        string    `json:"asn_org,omitempty"`
	IsKnownBot    bool      `json:"is_known_bot"`
	IsTor         bool      `json:"is_tor"`
	IsProxy       bool      `json:"is_proxy"`
	IsVPN         bool      `json:"is_vpn"`
	ThreatScore   int       `json:"threat_score"` // 0-100, higher = more suspicious
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
	TotalRequests int64     `json:"total_requests"`
	TotalBytes    int64     `json:"total_bytes"`
}

// TrafficFlow represents traffic flow from a source to a backend.
type TrafficFlow struct {
	ID              int64     `json:"id"`
	ServerID        string    `json:"server_id"`
	SourceIP        string    `json:"source_ip"`
	Country         string    `json:"country"`
	CountryCode     string    `json:"country_code"`
	BackendName     string    `json:"backend_name"`
	FrontendName    string    `json:"frontend_name"`
	Timestamp       time.Time `json:"timestamp"`
	RequestCount    int64     `json:"request_count"`
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	Response2xx     int64     `json:"response_2xx"`
	Response3xx     int64     `json:"response_3xx"`
	Response4xx     int64     `json:"response_4xx"`
	Response5xx     int64     `json:"response_5xx"`
	AvgResponseTime int64     `json:"avg_response_time"` // in milliseconds
	MaxResponseTime int64     `json:"max_response_time"`
	ErrorRate       float64   `json:"error_rate"` // percentage of 4xx+5xx
}

// TrafficSnapshot represents aggregated traffic data at a point in time.
type TrafficSnapshot struct {
	ID                int64     `json:"id"`
	ServerID          string    `json:"server_id"`
	CollectedAt       time.Time `json:"collected_at"`
	TotalRequests     int64     `json:"total_requests"`
	TotalBytesIn      int64     `json:"total_bytes_in"`
	TotalBytesOut     int64     `json:"total_bytes_out"`
	UniqueIPs         int       `json:"unique_ips"`
	UniqueCountries   int       `json:"unique_countries"`
	TotalResponse2xx  int64     `json:"total_response_2xx"`
	TotalResponse3xx  int64     `json:"total_response_3xx"`
	TotalResponse4xx  int64     `json:"total_response_4xx"`
	TotalResponse5xx  int64     `json:"total_response_5xx"`
	AvgResponseTime   int64     `json:"avg_response_time"`
	P95ResponseTime   int64     `json:"p95_response_time"`
	P99ResponseTime   int64     `json:"p99_response_time"`
	RequestsPerSecond float64   `json:"requests_per_second"`
	BytesPerSecond    float64   `json:"bytes_per_second"`
}

// TrafficAnalysis represents the complete traffic analysis for a server.
type TrafficAnalysis struct {
	ServerID        string                    `json:"server_id"`
	CollectedAt     time.Time                 `json:"collected_at"`
	Summary         *TrafficSummary           `json:"summary"`
	TopSources      []TrafficSourceStats      `json:"top_sources"`
	TopBackends     []TrafficBackendStats     `json:"top_backends"`
	TopCountries    []TrafficCountryStats     `json:"top_countries"`
	RecentFlows     []TrafficFlow             `json:"recent_flows"`
	Anomalies       []TrafficAnomaly          `json:"anomalies"`
	NetworkNodes    []NetworkNode             `json:"network_nodes"`
	NetworkEdges    []NetworkEdge             `json:"network_edges"`
	HourlyBreakdown []HourlyTrafficStats      `json:"hourly_breakdown"`
	StatusCodeDist  *StatusCodeDistribution   `json:"status_code_distribution"`
}

// TrafficSummary provides high-level traffic metrics.
type TrafficSummary struct {
	TotalRequests       int64   `json:"total_requests"`
	TotalBytesIn        int64   `json:"total_bytes_in"`
	TotalBytesOut       int64   `json:"total_bytes_out"`
	UniqueIPs           int     `json:"unique_ips"`
	UniqueCountries     int     `json:"unique_countries"`
	RequestsPerSecond   float64 `json:"requests_per_second"`
	AvgResponseTime     int64   `json:"avg_response_time"`
	ErrorRate           float64 `json:"error_rate"`
	SuccessRate         float64 `json:"success_rate"`
	ThroughputMbps      float64 `json:"throughput_mbps"`
	ActiveConnections   int64   `json:"active_connections"`
	PeakConnectionsHour string  `json:"peak_connections_hour"`
}

// TrafficSourceStats represents traffic statistics for a specific source IP.
type TrafficSourceStats struct {
	IPAddress       string    `json:"ip_address"`
	Country         string    `json:"country"`
	CountryCode     string    `json:"country_code"`
	City            string    `json:"city,omitempty"`
	Requests        int64     `json:"requests"`
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	AvgResponseTime int64     `json:"avg_response_time"`
	ErrorRate       float64   `json:"error_rate"`
	Response2xx     int64     `json:"response_2xx"`
	Response3xx     int64     `json:"response_3xx"`
	Response4xx     int64     `json:"response_4xx"`
	Response5xx     int64     `json:"response_5xx"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	ThreatLevel     string    `json:"threat_level"` // "low", "medium", "high", "critical"
	IsBlocked       bool      `json:"is_blocked"`
	BackendsHit     []string  `json:"backends_hit"`
}

// TrafficBackendStats represents traffic statistics for a backend.
type TrafficBackendStats struct {
	BackendName       string  `json:"backend_name"`
	DisplayName       string  `json:"display_name"`
	TotalRequests     int64   `json:"total_requests"`
	BytesIn           int64   `json:"bytes_in"`
	BytesOut          int64   `json:"bytes_out"`
	UniqueIPs         int     `json:"unique_ips"`
	AvgResponseTime   int64   `json:"avg_response_time"`
	P95ResponseTime   int64   `json:"p95_response_time"`
	ErrorRate         float64 `json:"error_rate"`
	RequestsPerSecond float64 `json:"requests_per_second"`
	Response2xx       int64   `json:"response_2xx"`
	Response3xx       int64   `json:"response_3xx"`
	Response4xx       int64   `json:"response_4xx"`
	Response5xx       int64   `json:"response_5xx"`
	HealthStatus      string  `json:"health_status"` // "healthy", "degraded", "down"
}

// TrafficCountryStats represents traffic statistics by country.
type TrafficCountryStats struct {
	Country         string  `json:"country"`
	CountryCode     string  `json:"country_code"`
	Requests        int64   `json:"requests"`
	UniqueIPs       int     `json:"unique_ips"`
	BytesIn         int64   `json:"bytes_in"`
	BytesOut        int64   `json:"bytes_out"`
	AvgResponseTime int64   `json:"avg_response_time"`
	ErrorRate       float64 `json:"error_rate"`
	Percentage      float64 `json:"percentage"` // percentage of total traffic
}

// TrafficAnomaly represents detected traffic anomalies.
type TrafficAnomaly struct {
	ID          int64     `json:"id"`
	Type        string    `json:"type"` // "spike", "drop", "high_error", "slow_response", "suspicious_ip", "ddos"
	Severity    string    `json:"severity"` // "info", "warning", "critical"
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AffectedIP  string    `json:"affected_ip,omitempty"`
	AffectedBackend string `json:"affected_backend,omitempty"`
	DetectedAt  time.Time `json:"detected_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Metrics     map[string]interface{} `json:"metrics,omitempty"`
}

// NetworkNode represents a node in the traffic network visualization.
type NetworkNode struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"` // "source", "frontend", "backend", "server", "country"
	Label       string  `json:"label"`
	SubLabel    string  `json:"sub_label,omitempty"`
	Size        float64 `json:"size"`        // relative size based on traffic volume
	Color       string  `json:"color"`       // color based on status/health
	X           float64 `json:"x,omitempty"` // position for pre-layout
	Y           float64 `json:"y,omitempty"`
	Status      string  `json:"status,omitempty"` // "healthy", "warning", "error"
	Requests    int64   `json:"requests"`
	BytesIn     int64   `json:"bytes_in"`
	BytesOut    int64   `json:"bytes_out"`
	ErrorRate   float64 `json:"error_rate"`
	AvgLatency  int64   `json:"avg_latency"`
	CountryCode string  `json:"country_code,omitempty"`
	IPAddress   string  `json:"ip_address,omitempty"`
}

// NetworkEdge represents a connection between nodes in the visualization.
type NetworkEdge struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`      // source node ID
	Target      string  `json:"target"`      // target node ID
	Weight      float64 `json:"weight"`      // line thickness based on traffic volume
	Color       string  `json:"color"`       // color based on latency/errors
	Animated    bool    `json:"animated"`    // animate high-traffic flows
	Label       string  `json:"label,omitempty"`
	Requests    int64   `json:"requests"`
	BytesIn     int64   `json:"bytes_in"`
	BytesOut    int64   `json:"bytes_out"`
	AvgLatency  int64   `json:"avg_latency"`
	ErrorRate   float64 `json:"error_rate"`
	StatusColor string  `json:"status_color"` // derived from error rate/latency
}

// HourlyTrafficStats represents traffic aggregated by hour.
type HourlyTrafficStats struct {
	Hour            string  `json:"hour"` // "HH:00" format
	Timestamp       time.Time `json:"timestamp"`
	Requests        int64   `json:"requests"`
	BytesIn         int64   `json:"bytes_in"`
	BytesOut        int64   `json:"bytes_out"`
	UniqueIPs       int     `json:"unique_ips"`
	AvgResponseTime int64   `json:"avg_response_time"`
	ErrorRate       float64 `json:"error_rate"`
	Response2xx     int64   `json:"response_2xx"`
	Response3xx     int64   `json:"response_3xx"`
	Response4xx     int64   `json:"response_4xx"`
	Response5xx     int64   `json:"response_5xx"`
}

// StatusCodeDistribution represents the distribution of HTTP status codes.
type StatusCodeDistribution struct {
	Total       int64   `json:"total"`
	Success2xx  int64   `json:"success_2xx"`
	Redirect3xx int64   `json:"redirect_3xx"`
	ClientErr4xx int64  `json:"client_error_4xx"`
	ServerErr5xx int64  `json:"server_error_5xx"`
	Success2xxPct  float64 `json:"success_2xx_pct"`
	Redirect3xxPct float64 `json:"redirect_3xx_pct"`
	ClientErr4xxPct float64 `json:"client_error_4xx_pct"`
	ServerErr5xxPct float64 `json:"server_error_5xx_pct"`
}

// TrafficFilter represents filter options for traffic queries.
type TrafficFilter struct {
	BoxID         string    `json:"box_id"`
	BackendName   string    `json:"backend_name,omitempty"`
	FrontendName  string    `json:"frontend_name,omitempty"`
	SourceIP      string    `json:"source_ip,omitempty"`
	Country       string    `json:"country,omitempty"`
	CountryCode   string    `json:"country_code,omitempty"`
	MinRequests   int64     `json:"min_requests,omitempty"`
	MinErrorRate  float64   `json:"min_error_rate,omitempty"`
	MaxErrorRate  float64   `json:"max_error_rate,omitempty"`
	MinLatency    int64     `json:"min_latency,omitempty"`
	MaxLatency    int64     `json:"max_latency,omitempty"`
	StatusCode    int       `json:"status_code,omitempty"`
	StartTime     time.Time `json:"start_time,omitempty"`
	EndTime       time.Time `json:"end_time,omitempty"`
	TimeRange     string    `json:"time_range,omitempty"` // "1h", "6h", "24h", "7d", "30d"
	Limit         int       `json:"limit,omitempty"`
	Offset        int       `json:"offset,omitempty"`
	SortBy        string    `json:"sort_by,omitempty"`   // "requests", "bytes", "latency", "errors"
	SortOrder     string    `json:"sort_order,omitempty"` // "asc", "desc"
}

// GeoLocation represents geographic location data.
type GeoLocation struct {
	IPAddress   string  `json:"ip_address"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	City        string  `json:"city,omitempty"`
	Region      string  `json:"region,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Timezone    string  `json:"timezone,omitempty"`
	ASN         int     `json:"asn,omitempty"`
	ASNOrg      string  `json:"asn_org,omitempty"`
}

// CalculateThreatLevel determines the threat level based on metrics.
func (s *TrafficSourceStats) CalculateThreatLevel() string {
	if s.ErrorRate > 50 || s.Response5xx > 1000 {
		return "critical"
	}
	if s.ErrorRate > 25 || s.Response5xx > 500 {
		return "high"
	}
	if s.ErrorRate > 10 || s.Response5xx > 100 {
		return "medium"
	}
	return "low"
}

// GetEdgeStatusColor returns a color based on error rate and latency.
func (e *NetworkEdge) GetEdgeStatusColor() string {
	if e.ErrorRate > 20 {
		return "#ef4444" // red
	}
	if e.ErrorRate > 5 || e.AvgLatency > 1000 {
		return "#f59e0b" // amber/orange
	}
	if e.AvgLatency > 500 {
		return "#eab308" // yellow
	}
	return "#22c55e" // green
}

// GetNodeStatusColor returns a color based on node status.
func (n *NetworkNode) GetNodeStatusColor() string {
	switch n.Status {
	case "error", "down":
		return "#ef4444" // red
	case "warning", "degraded":
		return "#f59e0b" // amber
	default:
		return "#22c55e" // green
	}
}

// FormatBytes formats bytes into human-readable format.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return formatInt64(bytes) + " B"
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return formatFloat64(float64(bytes)/float64(div)) + " " + string("KMGTPE"[exp]) + "B"
}

func formatInt64(n int64) string {
	return string(rune(n + '0'))
}

func formatFloat64(f float64) string {
	if f == float64(int64(f)) {
		return string(rune(int64(f) + '0'))
	}
	return string(rune(int64(f*10)/10 + '0'))
}
