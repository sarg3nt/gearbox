package haproxy

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HAProxyStats represents parsed HAProxy statistics.
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

// ParseStatsCSV parses HAProxy stats CSV format into structured data.
func ParseStatsCSV(csvData string) (*HAProxyStats, error) {
	stats := &HAProxyStats{
		ParsedAt: time.Now(),
	}

	reader := csv.NewReader(strings.NewReader(csvData))
	reader.FieldsPerRecord = -1 // Allow variable number of fields
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("empty CSV data")
	}

	// Find header line (starts with #)
	headerIndex := -1
	for i, record := range records {
		if len(record) > 0 && strings.HasPrefix(record[0], "#") {
			headerIndex = i
			break
		}
	}

	if headerIndex < 0 || headerIndex >= len(records)-1 {
		return nil, fmt.Errorf("no data rows found after header")
	}

	// Parse header to get column indices
	header := records[headerIndex]
	if len(header) == 0 {
		return nil, fmt.Errorf("empty header")
	}

	// Remove # prefix from first column
	header[0] = strings.TrimPrefix(header[0], "# ")

	colMap := makeColumnMap(header)

	// Track backends to aggregate server stats
	backendMap := make(map[string]*Backend)
	// Track servers by backend name
	serverMap := make(map[string][]BackendServer)

	// Parse data rows
	for _, record := range records[headerIndex+1:] {
		if len(record) < 2 {
			continue // Skip invalid rows
		}

		pxname := getField(record, colMap, "pxname")
		svname := getField(record, colMap, "svname")

		if pxname == "" || svname == "" {
			continue
		}

		// Frontend stats
		if svname == "FRONTEND" {
			frontend := parseFrontend(record, colMap, pxname)
			stats.Frontends = append(stats.Frontends, frontend)
			continue
		}

		// Backend aggregate stats
		if svname == "BACKEND" {
			backend := parseBackend(record, colMap, pxname)
			backendMap[pxname] = &backend
			continue
		}

		// Individual server in a backend
		server := parseBackendServer(record, colMap, svname)
		serverMap[pxname] = append(serverMap[pxname], server)
	}

	// Attach servers to their backends and calculate stats
	for backendName, backend := range backendMap {
		if servers, exists := serverMap[backendName]; exists {
			backend.Servers = servers
		}

		// Calculate server counts
		backend.ActiveServers = 0
		backend.DownServers = 0
		for _, srv := range backend.Servers {
			switch srv.Status {
			case "UP":
				backend.ActiveServers++
			case "DOWN":
				backend.DownServers++
			}
		}
		backend.TotalServers = int64(len(backend.Servers))
		if backend.TotalServers > 0 {
			backend.HealthPercent = float64(backend.ActiveServers) / float64(backend.TotalServers) * 100
		}

		stats.Backends = append(stats.Backends, *backend)
	}

	// Sort for consistent output
	sort.Slice(stats.Backends, func(i, j int) bool {
		return stats.Backends[i].Name < stats.Backends[j].Name
	})
	sort.Slice(stats.Frontends, func(i, j int) bool {
		return stats.Frontends[i].Name < stats.Frontends[j].Name
	})

	return stats, nil
}

// parseFrontend parses a frontend CSV row.
func parseFrontend(record []string, colMap map[string]int, name string) Frontend {
	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return Frontend{
		Name:             name,
		Status:           getField(record, colMap, "status"),
		CurrentSessions:  getInt64Field(record, colMap, "scur"),
		MaxSessions:      getInt64Field(record, colMap, "smax"),
		SessionLimit:     getInt64Field(record, colMap, "slim"),
		TotalSessions:    getInt64Field(record, colMap, "stot"),
		BytesIn:          getInt64Field(record, colMap, "bin"),
		BytesOut:         getInt64Field(record, colMap, "bout"),
		RequestRate:      requestRate,
		RequestTotal:     getInt64Field(record, colMap, "req_tot"),
		Response2xx:      getInt64Field(record, colMap, "hrsp_2xx"),
		Response3xx:      getInt64Field(record, colMap, "hrsp_3xx"),
		Response4xx:      getInt64Field(record, colMap, "hrsp_4xx"),
		Response5xx:      getInt64Field(record, colMap, "hrsp_5xx"),
		DeniedRequests:   getInt64Field(record, colMap, "dreq"),
		DeniedResponses:  getInt64Field(record, colMap, "dresp"),
		ErrorsRequest:    getInt64Field(record, colMap, "ereq"),
		ErrorsConnection: getInt64Field(record, colMap, "econ"),
	}
}

// parseBackend parses a backend CSV row.
func parseBackend(record []string, colMap map[string]int, name string) Backend {
	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return Backend{
		Name:            name,
		Status:          getField(record, colMap, "status"),
		CurrentSessions: getInt64Field(record, colMap, "scur"),
		MaxSessions:     getInt64Field(record, colMap, "smax"),
		SessionLimit:    getInt64Field(record, colMap, "slim"),
		TotalSessions:   getInt64Field(record, colMap, "stot"),
		BytesIn:         getInt64Field(record, colMap, "bin"),
		BytesOut:        getInt64Field(record, colMap, "bout"),
		RequestRate:     requestRate,
		RequestTotal:    getInt64Field(record, colMap, "req_tot"),
		QueueTime:       getInt64Field(record, colMap, "qtime"),
		ConnectTime:     getInt64Field(record, colMap, "ctime"),
		ResponseTime:    getInt64Field(record, colMap, "rtime"),
		TotalTime:       getInt64Field(record, colMap, "ttime"),
		Response2xx:     getInt64Field(record, colMap, "hrsp_2xx"),
		Response3xx:     getInt64Field(record, colMap, "hrsp_3xx"),
		Response4xx:     getInt64Field(record, colMap, "hrsp_4xx"),
		Response5xx:     getInt64Field(record, colMap, "hrsp_5xx"),
		Weight:          getInt64Field(record, colMap, "weight"),
		QueueCurrent:    getInt64Field(record, colMap, "qcur"),
	}
}

// parseBackendServer parses a backend server CSV row.
func parseBackendServer(record []string, colMap map[string]int, name string) BackendServer {
	status := getField(record, colMap, "status")

	var healthPercent float64
	switch status {
	case "UP":
		healthPercent = 100.0
	case "DOWN":
		healthPercent = 0.0
	default:
		healthPercent = 50.0 // MAINT, DRAIN, etc.
	}

	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return BackendServer{
		Name:            name,
		Status:          status,
		Weight:          getInt64Field(record, colMap, "weight"),
		CurrentSessions: getInt64Field(record, colMap, "scur"),
		MaxSessions:     getInt64Field(record, colMap, "smax"),
		TotalSessions:   getInt64Field(record, colMap, "stot"),
		BytesIn:         getInt64Field(record, colMap, "bin"),
		BytesOut:        getInt64Field(record, colMap, "bout"),
		CheckStatus:     getField(record, colMap, "check_status"),
		CheckDuration:   getInt64Field(record, colMap, "check_duration"),
		LastChange:      getInt64Field(record, colMap, "lastchg"),
		Downtime:        getInt64Field(record, colMap, "downtime"),
		ThrottlePercent: getInt64Field(record, colMap, "throttle"),
		HealthPercent:   healthPercent,
		QueueCurrent:    getInt64Field(record, colMap, "qcur"),
		RequestRate:     requestRate,
		ResponseTime:    getInt64Field(record, colMap, "rtime"),
	}
}

// makeColumnMap creates a map of column names to indices.
func makeColumnMap(header []string) map[string]int {
	colMap := make(map[string]int)
	for i, col := range header {
		colMap[strings.TrimSpace(col)] = i
	}
	return colMap
}

// getField retrieves a string field from a record.
func getField(record []string, colMap map[string]int, field string) string {
	if idx, ok := colMap[field]; ok && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}

// getInt64Field retrieves an int64 field from a record.
func getInt64Field(record []string, colMap map[string]int, field string) int64 {
	val := getField(record, colMap, field)
	if val == "" {
		return 0
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0
	}
	return i
}
