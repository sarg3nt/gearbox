package parser

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// ParseStatsCSV parses HAProxy stats CSV format into structured data.
func ParseStatsCSV(csvData string) (*models.HAProxyStats, error) {
	stats := &models.HAProxyStats{
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

	// First line is the header (starts with #)
	headerIndex := 0
	for i, record := range records {
		if len(record) > 0 && strings.HasPrefix(record[0], "#") {
			headerIndex = i
			break
		}
	}

	if headerIndex >= len(records)-1 {
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
	backendMap := make(map[string]*models.Backend)
	// Track servers by backend name
	serverMap := make(map[string][]models.BackendServer)

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

	// Attach servers to their backends
	for backendName, backend := range backendMap {
		if servers, exists := serverMap[backendName]; exists {
			backend.Servers = servers
		}
	}

	// Convert backend map to slice
	for _, backend := range backendMap {
		// Update backend stats from servers
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
		// Set total servers and calculate average uptime percentage
		backend.TotalServers = int64(len(backend.Servers))
		if backend.TotalServers > 0 {
			// Calculate average uptime percentage from all servers
			var totalUptime float64
			for _, srv := range backend.Servers {
				totalUptime += srv.HealthPercent
			}
			backend.HealthPercent = totalUptime / float64(backend.TotalServers)
		}
		stats.Backends = append(stats.Backends, *backend)
	}

	// Sort backends alphabetically by name
	sort.Slice(stats.Backends, func(i, j int) bool {
		return stats.Backends[i].Name < stats.Backends[j].Name
	})

	// Sort frontends alphabetically by name
	sort.Slice(stats.Frontends, func(i, j int) bool {
		return stats.Frontends[i].Name < stats.Frontends[j].Name
	})

	return stats, nil
}

// parseFrontend parses a frontend CSV row.
func parseFrontend(record []string, colMap map[string]int, name string) models.Frontend {
	// Try req_rate first, fall back to rate
	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return models.Frontend{
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
func parseBackend(record []string, colMap map[string]int, name string) models.Backend {
	// Try req_rate first, fall back to rate
	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return models.Backend{
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
	}
}

// parseBackendServer parses a backend server CSV row.
func parseBackendServer(record []string, colMap map[string]int, name string) models.BackendServer {
	status := getField(record, colMap, "status")

	// Calculate availability score based on current status
	// For single-server backends, this will be 100% or 0%
	// For multi-server backends, the backend average will show partial availability
	var uptimePercent float64
	if status == "UP" {
		uptimePercent = 100.0
	} else if status == "DOWN" {
		uptimePercent = 0.0
	} else {
		// MAINT, DRAIN, etc. - consider as unavailable for traffic
		uptimePercent = 0.0
	}

	// Try req_rate first, fall back to rate
	requestRate := getInt64Field(record, colMap, "req_rate")
	if requestRate == 0 {
		requestRate = getInt64Field(record, colMap, "rate")
	}

	return models.BackendServer{
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
		HealthPercent:   uptimePercent,
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
