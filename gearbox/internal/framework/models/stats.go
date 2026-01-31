package models

import "time"

// HAProxyStats represents the complete parsed stats from HAProxy.
type HAProxyStats struct {
	Frontends []Frontend `json:"frontends"`
	Backends  []Backend  `json:"backends"`
	ParsedAt  time.Time  `json:"parsed_at"`
}

// Frontend represents HAProxy frontend statistics.
type Frontend struct {
	Name             string `json:"name"`
	Status           string `json:"status"`
	CurrentSessions  int64  `json:"scur"`
	MaxSessions      int64  `json:"smax"`
	SessionLimit     int64  `json:"slim"`
	TotalSessions    int64  `json:"stot"`
	BytesIn          int64  `json:"bin"`
	BytesOut         int64  `json:"bout"`
	RequestRate      int64  `json:"req_rate"`
	RequestTotal     int64  `json:"req_tot"`
	ResponseTime     int64  `json:"rtime"`
	Response2xx      int64  `json:"hrsp_2xx"`
	Response3xx      int64  `json:"hrsp_3xx"`
	Response4xx      int64  `json:"hrsp_4xx"`
	Response5xx      int64  `json:"hrsp_5xx"`
	DeniedRequests   int64  `json:"dreq"`
	DeniedResponses  int64  `json:"dresp"`
	ErrorsRequest    int64  `json:"ereq"`
	ErrorsConnection int64  `json:"econ"`
}

// Backend represents HAProxy backend statistics.
type Backend struct {
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	CurrentSessions int64           `json:"scur"`
	MaxSessions     int64           `json:"smax"`
	SessionLimit    int64           `json:"slim"`
	TotalSessions   int64           `json:"stot"`
	BytesIn         int64           `json:"bin"`
	BytesOut        int64           `json:"bout"`
	RequestRate     int64           `json:"req_rate"`
	RequestTotal    int64           `json:"req_tot"`
	QueueTime       int64           `json:"qtime"`
	ConnectTime     int64           `json:"ctime"`
	ResponseTime    int64           `json:"rtime"`
	TotalTime       int64           `json:"ttime"`
	Response2xx     int64           `json:"hrsp_2xx"`
	Response3xx     int64           `json:"hrsp_3xx"`
	Response4xx     int64           `json:"hrsp_4xx"`
	Response5xx     int64           `json:"hrsp_5xx"`
	Servers         []BackendServer `json:"servers"`
	Weight          int64           `json:"weight"`
	ActiveServers   int64           `json:"act"`
	BackupServers   int64           `json:"bck"`
	DownServers     int64           `json:"down_servers"`
	TotalServers    int64           `json:"total_servers"`
	HealthPercent   float64         `json:"health_percent"`
	QueueCurrent    int64           `json:"qcur"`
}

// BackendServer represents an individual server in a backend.
type BackendServer struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Weight          int64   `json:"weight"`
	CurrentSessions int64   `json:"scur"`
	MaxSessions     int64   `json:"smax"`
	TotalSessions   int64   `json:"stot"`
	BytesIn         int64   `json:"bin"`
	BytesOut        int64   `json:"bout"`
	CheckStatus     string  `json:"check_status"`
	CheckDuration   int64   `json:"check_duration"`
	LastChange      int64   `json:"lastchg"`
	Downtime        int64   `json:"downtime"`
	ThrottlePercent int64   `json:"throttle"`
	HealthPercent   float64 `json:"health_percent"`
	QueueCurrent    int64   `json:"qcur"`
	RequestRate     int64   `json:"req_rate"`
	ResponseTime    int64   `json:"rtime"`
}

// HealthStatus represents the overall health of a frontend or backend.
type HealthStatus string

const (
	HealthStatusUp      HealthStatus = "UP"
	HealthStatusWarning HealthStatus = "WARNING"
	HealthStatusDown    HealthStatus = "DOWN"
)

// CalculateHealth calculates the health status of a backend.
func (b *Backend) CalculateHealth() HealthStatus {
	if b.Status == "DOWN" || b.ActiveServers == 0 {
		return HealthStatusDown
	}

	totalServers := b.ActiveServers + b.BackupServers
	if totalServers == 0 {
		return HealthStatusDown
	}

	healthyServers := b.ActiveServers - b.DownServers
	healthPercent := float64(healthyServers) / float64(totalServers) * 100

	// Calculate error rate
	totalResponses := b.Response2xx + b.Response3xx + b.Response4xx + b.Response5xx
	errorRate := float64(0)
	if totalResponses > 0 {
		errorRate = float64(b.Response5xx) / float64(totalResponses) * 100
	}

	if healthPercent < 50 || errorRate > 5 {
		return HealthStatusWarning
	}

	return HealthStatusUp
}

// CalculateHealth calculates the health status of a frontend.
func (f *Frontend) CalculateHealth() HealthStatus {
	if f.Status == "DOWN" {
		return HealthStatusDown
	}

	// Calculate error rate
	totalResponses := f.Response2xx + f.Response3xx + f.Response4xx + f.Response5xx
	errorRate := float64(0)
	if totalResponses > 0 {
		errorRate = float64(f.Response5xx) / float64(totalResponses) * 100
	}

	if errorRate > 5 {
		return HealthStatusWarning
	}

	return HealthStatusUp
}
