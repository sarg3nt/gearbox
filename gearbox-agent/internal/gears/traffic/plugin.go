package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin implements the traffic analysis gear.
type Gear struct {
	gear.BaseGear
	statsClient *haproxy.StatsClient
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "traffic",
		DisplayName: "Traffic Analysis",
		Description: "Provides traffic analysis and monitoring from HAProxy stick tables",
		Version:     "1.0.0",
		Category:    "monitoring",
		Core:        false,
	}
}

// Initialize sets up the gear.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}

	// Only create stats client if HAProxy is configured
	if deps.HAProxyStatsSocket != "" || deps.HAProxyStatsURL != "" {
		p.statsClient = haproxy.NewStatsClient(
			deps.HAProxyStatsSocket,
			deps.HAProxyStatsURL,
			deps.HAProxyStatsUser,
			deps.HAProxyStatsPassword,
		)
	}

	return nil
}

// Start is a no-op - this plugin is stateless.
func (p *Gear) Start(ctx context.Context) error {
	return nil
}

// Stop cleans up resources.
func (p *Gear) Stop(ctx context.Context) error {
	return nil
}

// Health returns the health status.
func (p *Gear) Health() gear.HealthStatus {
	if p.statsClient == nil {
		return gear.NewDegradedStatus("HAProxy stats not configured")
	}
	return gear.NewHealthyStatus("operational")
}

// Probe reports whether the agent has a usable HAProxy stats surface.
// Traffic analysis reads stick tables over the same stats socket / URL as
// the haproxy gear, so the prerequisite is identical: a URL configured or
// a socket that exists on disk.
func (p *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	if deps.HAProxyStatsURL != "" {
		return gear.ProbeAvailable("stats URL configured", map[string]string{
			"stats_url": deps.HAProxyStatsURL,
		})
	}
	if deps.HAProxyStatsSocket != "" {
		if _, err := os.Stat(deps.HAProxyStatsSocket); err == nil {
			return gear.ProbeAvailable("stats socket reachable", map[string]string{
				"stats_socket": deps.HAProxyStatsSocket,
			})
		}
		return gear.ProbeInaccessible(fmt.Sprintf(
			"stats socket configured at %s but does not exist",
			deps.HAProxyStatsSocket,
		))
	}
	return gear.ProbeNotInstalled("no HAProxy stats socket or URL configured")
}

// RegisterRoutes registers the plugin's HTTP routes.
func (p *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/traffic", p.HandleTraffic)
	r.Get("/api/v1/traffic/summary", p.HandleTrafficSummary)
}

// TrafficSourceData represents traffic data for a single source IP.
type TrafficSourceData struct {
	IPAddress       string    `json:"ip_address"`
	Requests        int64     `json:"requests"`
	SessionRate     int64     `json:"session_rate"`      // Sessions per period
	HTTPRequestRate int64     `json:"http_request_rate"` // HTTP requests per period
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	Connections     int64     `json:"connections"`
	LastSeen        time.Time `json:"last_seen"`
	Backend         string    `json:"backend,omitempty"`
	Frontend        string    `json:"frontend,omitempty"`
}

// TrafficResponse contains aggregated traffic data.
type TrafficResponse struct {
	CollectedAt      time.Time                    `json:"collected_at"`
	TotalSources     int                          `json:"total_sources"`
	Sources          []TrafficSourceData          `json:"sources"`
	TopByRequests    []TrafficSourceData          `json:"top_by_requests"`
	TopByBytes       []TrafficSourceData          `json:"top_by_bytes"`
	TopByConnections []TrafficSourceData          `json:"top_by_connections"`
	BackendTraffic   []BackendTrafficData         `json:"backend_traffic"`
	ConnectionFlows  []haproxy.ConnectionFlow     `json:"connection_flows"`
	ActiveSessions   []haproxy.ActiveSession      `json:"active_sessions"`
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

// HandleTraffic handles GET /api/v1/traffic - returns traffic analysis data.
//
//	@Summary		Traffic analysis
//	@Description	Returns traffic analysis data including per-IP statistics from stick tables and backend aggregations.
//	@Tags			Traffic
//	@Produce		json
//	@Security		BearerAuth
//	@Param			limit		query		int		false	"Limit number of sources returned"	default(100)
//	@Param			top_n		query		int		false	"Number of top sources to return"	default(10)
//	@Param			include_all	query		bool	false	"Include all IPs, not just those with data"	default(false)
//	@Success		200	{object}	TrafficResponse	"Traffic analysis data"
//	@Failure		401	{string}	string			"Unauthorized"
//	@Failure		500	{string}	string			"Failed to fetch traffic data"
//	@Failure		503	{string}	string			"HAProxy stats not configured"
//	@Router			/api/v1/traffic [get]
func (p *Gear) HandleTraffic(w http.ResponseWriter, r *http.Request) {
	if p.statsClient == nil {
		http.Error(w, "HAProxy stats not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 10000 {
			limit = l
		}
	}

	topN := 10
	if topNStr := r.URL.Query().Get("top_n"); topNStr != "" {
		if n, err := strconv.Atoi(topNStr); err == nil && n > 0 && n <= 100 {
			topN = n
		}
	}

	// Collect traffic data from stick tables
	sources, err := p.collectTrafficFromStickTables(limit)
	if err != nil {
		http.Error(w, "Failed to collect traffic data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get backend stats
	backendTraffic, err := p.collectBackendTraffic()
	if err != nil {
		// Log but continue - backend traffic is not critical
		backendTraffic = []BackendTrafficData{}
	}

	// Get active connection flows (source IP -> backend)
	connectionFlows, err := p.statsClient.GetConnectionFlows(50)
	if err != nil {
		connectionFlows = []haproxy.ConnectionFlow{}
	}

	// Get active sessions for real-time view
	activeSessions, err := p.statsClient.GetActiveSessions(100)
	if err != nil {
		activeSessions = []haproxy.ActiveSession{}
	}

	// Build response
	resp := TrafficResponse{
		CollectedAt:      time.Now(),
		TotalSources:     len(sources),
		Sources:          sources,
		TopByRequests:    getTopByRequests(sources, topN),
		TopByBytes:       getTopByBytes(sources, topN),
		TopByConnections: getTopByConnections(sources, topN),
		BackendTraffic:   backendTraffic,
		ConnectionFlows:  connectionFlows,
		ActiveSessions:   activeSessions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// collectTrafficFromStickTables collects traffic data from HAProxy stick tables.
func (p *Gear) collectTrafficFromStickTables(limit int) ([]TrafficSourceData, error) {
	tables, err := p.statsClient.GetStickTables()
	if err != nil {
		return nil, err
	}

	// Map to aggregate data per IP across all tables
	ipData := make(map[string]*TrafficSourceData)

	for _, table := range tables {
		// Get detailed entries for this table
		entries, err := p.statsClient.GetStickTableEntries(table.Name, limit)
		if err != nil {
			continue // Skip tables we can't read
		}

		for _, entry := range entries {
			key := entry.Key
			if key == "" {
				continue
			}

			// Filter out internal/invalid IPs
			if key == "unix" || key == "localhost" || key == "127.0.0.1" {
				continue
			}
			if len(key) > 0 && (key[0] == '/' || key[0] == ':') {
				continue
			}

			// Get or create entry for this IP
			data, exists := ipData[key]
			if !exists {
				data = &TrafficSourceData{
					IPAddress: key,
					LastSeen:  time.Now(),
				}
				ipData[key] = data
			}

			// Extract data from entry fields
			for fieldName, fieldValue := range entry.Data {
				switch fieldName {
				case "http_req_rate":
					if v, ok := fieldValue.(float64); ok {
						data.HTTPRequestRate += int64(v)
					}
				case "http_req_cnt":
					if v, ok := fieldValue.(float64); ok {
						data.Requests += int64(v)
					}
				case "sess_rate":
					if v, ok := fieldValue.(float64); ok {
						data.SessionRate += int64(v)
					}
				case "sess_cnt":
					if v, ok := fieldValue.(float64); ok {
						data.Connections += int64(v)
					}
				case "bytes_in_rate":
					if v, ok := fieldValue.(float64); ok {
						data.BytesIn += int64(v)
					}
				case "bytes_out_rate":
					if v, ok := fieldValue.(float64); ok {
						data.BytesOut += int64(v)
					}
				case "gpc0", "gpc1": // General purpose counters (often used for request counts)
					if v, ok := fieldValue.(float64); ok {
						data.Requests += int64(v)
					}
				}
			}

			// Track which backend/frontend this table belongs to
			if table.Name != "" {
				if data.Backend == "" {
					data.Backend = table.Name
				} else if data.Backend != table.Name {
					data.Backend = "multiple"
				}
			}
		}
	}

	// Convert map to slice
	sources := make([]TrafficSourceData, 0, len(ipData))
	for _, data := range ipData {
		sources = append(sources, *data)
	}

	return sources, nil
}

// collectBackendTraffic collects traffic stats from backend stats.
func (p *Gear) collectBackendTraffic() ([]BackendTrafficData, error) {
	csvData, err := p.statsClient.GetStatsCSV()
	if err != nil {
		return nil, err
	}

	stats, err := haproxy.ParseStatsCSV(csvData)
	if err != nil {
		return nil, err
	}

	backendTraffic := make([]BackendTrafficData, 0, len(stats.Backends))
	for _, backend := range stats.Backends {
		bt := BackendTrafficData{
			Name:            backend.Name,
			TotalRequests:   backend.RequestTotal,
			BytesIn:         backend.BytesIn,
			BytesOut:        backend.BytesOut,
			AvgResponseTime: backend.ResponseTime,
			Response2xx:     backend.Response2xx,
			Response3xx:     backend.Response3xx,
			Response4xx:     backend.Response4xx,
			Response5xx:     backend.Response5xx,
		}

		// Calculate error rate
		total := bt.Response2xx + bt.Response3xx + bt.Response4xx + bt.Response5xx
		if total > 0 {
			bt.ErrorRate = float64(bt.Response4xx+bt.Response5xx) / float64(total) * 100
		}

		backendTraffic = append(backendTraffic, bt)
	}

	return backendTraffic, nil
}

// Helper functions for top-N calculations

func getTopByRequests(sources []TrafficSourceData, n int) []TrafficSourceData {
	return getTopN(sources, n, func(s TrafficSourceData) int64 { return s.Requests + s.HTTPRequestRate })
}

func getTopByBytes(sources []TrafficSourceData, n int) []TrafficSourceData {
	return getTopN(sources, n, func(s TrafficSourceData) int64 { return s.BytesIn + s.BytesOut })
}

func getTopByConnections(sources []TrafficSourceData, n int) []TrafficSourceData {
	return getTopN(sources, n, func(s TrafficSourceData) int64 { return s.Connections + s.SessionRate })
}

func getTopN(sources []TrafficSourceData, n int, getValue func(TrafficSourceData) int64) []TrafficSourceData {
	if len(sources) == 0 {
		return sources
	}

	// Simple selection sort for top N
	result := make([]TrafficSourceData, 0, n)
	used := make(map[int]bool)

	for i := 0; i < n && i < len(sources); i++ {
		maxIdx := -1
		maxVal := int64(-1)

		for j, s := range sources {
			if !used[j] {
				val := getValue(s)
				if val > maxVal {
					maxVal = val
					maxIdx = j
				}
			}
		}

		if maxIdx >= 0 {
			used[maxIdx] = true
			result = append(result, sources[maxIdx])
		}
	}

	return result
}

// TrafficSummary contains aggregated traffic summary.
type TrafficSummary struct {
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

// HandleTrafficSummary handles GET /api/v1/traffic/summary - returns summary stats only.
//
//	@Summary		Traffic summary
//	@Description	Returns a summary of traffic statistics without individual IP details.
//	@Tags			Traffic
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	TrafficSummary	"Traffic summary"
//	@Failure		401	{string}	string			"Unauthorized"
//	@Failure		500	{string}	string			"Failed to fetch traffic data"
//	@Router			/api/v1/traffic/summary [get]
func (p *Gear) HandleTrafficSummary(w http.ResponseWriter, r *http.Request) {
	if p.statsClient == nil {
		http.Error(w, "HAProxy stats not configured", http.StatusServiceUnavailable)
		return
	}

	// Get basic stats
	csvData, err := p.statsClient.GetStatsCSV()
	if err != nil {
		http.Error(w, "Failed to fetch stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stats, err := haproxy.ParseStatsCSV(csvData)
	if err != nil {
		http.Error(w, "Failed to parse stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate totals
	var totalRequests, totalBytesIn, totalBytesOut int64
	var total2xx, total3xx, total4xx, total5xx int64
	var totalResponseTime int64
	var responseTimeCount int64

	for _, backend := range stats.Backends {
		totalRequests += backend.RequestTotal
		totalBytesIn += backend.BytesIn
		totalBytesOut += backend.BytesOut
		total2xx += backend.Response2xx
		total3xx += backend.Response3xx
		total4xx += backend.Response4xx
		total5xx += backend.Response5xx
		if backend.ResponseTime > 0 {
			totalResponseTime += backend.ResponseTime
			responseTimeCount++
		}
	}

	// Get unique IPs from stick tables
	sources, _ := p.collectTrafficFromStickTables(10000)
	uniqueIPs := len(sources)

	// Calculate averages and rates
	avgResponseTime := int64(0)
	if responseTimeCount > 0 {
		avgResponseTime = totalResponseTime / responseTimeCount
	}

	totalResponses := total2xx + total3xx + total4xx + total5xx
	errorRate := float64(0)
	if totalResponses > 0 {
		errorRate = float64(total4xx+total5xx) / float64(totalResponses) * 100
	}

	summary := TrafficSummary{
		CollectedAt:      time.Now(),
		TotalRequests:    totalRequests,
		TotalBytesIn:     totalBytesIn,
		TotalBytesOut:    totalBytesOut,
		UniqueIPs:        uniqueIPs,
		AvgResponseTime:  avgResponseTime,
		TotalResponse2xx: total2xx,
		TotalResponse3xx: total3xx,
		TotalResponse4xx: total4xx,
		TotalResponse5xx: total5xx,
		ErrorRate:        errorRate,
		BackendCount:     len(stats.Backends),
		FrontendCount:    len(stats.Frontends),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}
