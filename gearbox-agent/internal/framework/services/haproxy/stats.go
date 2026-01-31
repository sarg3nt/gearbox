// Package haproxy provides HAProxy interaction utilities.
package haproxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// allowedStatsHosts are the only hosts allowed for HTTP stats endpoint.
// This prevents SSRF attacks via the HAPROXY_STATS_URL environment variable.
var allowedStatsHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// StatsClient provides access to HAProxy statistics.
type StatsClient struct {
	socketPath string
	statsURL   string
	username   string
	password   string
	httpClient *http.Client
}

// NewStatsClient creates a new stats client.
// It can use either the Unix socket or HTTP stats endpoint.
// The statsURL is validated to prevent SSRF attacks - only localhost is allowed.
func NewStatsClient(socketPath, statsURL, username, password string) *StatsClient {
	// Validate statsURL to prevent SSRF
	validatedURL := ""
	if statsURL != "" {
		if isAllowedStatsURL(statsURL) {
			validatedURL = statsURL
		}
		// If URL is not allowed, silently ignore it (socket will be used)
	}

	return &StatsClient{
		socketPath: socketPath,
		statsURL:   validatedURL,
		username:   username,
		password:   password,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// isAllowedStatsURL validates that the URL is safe to use (localhost only).
func isAllowedStatsURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Only allow http and https schemes
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	// Extract hostname (without port)
	host := parsed.Hostname()

	// Check against allowlist
	return allowedStatsHosts[host]
}

// GetStatsCSV fetches HAProxy stats in CSV format.
func (c *StatsClient) GetStatsCSV() (string, error) {
	// Prefer socket if available
	if c.socketPath != "" {
		csv, err := c.getStatsFromSocket()
		if err == nil {
			return csv, nil
		}
		// Fall back to HTTP if socket fails
	}

	if c.statsURL != "" {
		return c.getStatsFromHTTP()
	}

	return "", fmt.Errorf("no stats source configured (socket or HTTP)")
}

// getStatsFromSocket fetches stats via Unix socket.
func (c *StatsClient) getStatsFromSocket() (string, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	// Set read/write deadline
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Send command
	_, err = conn.Write([]byte("show stat\n"))
	if err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// Read response
	var sb strings.Builder
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return sb.String(), nil
}

// getStatsFromHTTP fetches stats via HTTP endpoint.
func (c *StatsClient) getStatsFromHTTP() (string, error) {
	url := c.statsURL
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	url += ";csv"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stats endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

// RuntimeInfo contains HAProxy runtime information.
type RuntimeInfo struct {
	Version          string            `json:"version"`
	ReleaseDate      string            `json:"release_date"`
	Uptime           string            `json:"uptime"`
	UptimeSeconds    int64             `json:"uptime_sec"`
	ProcessNum       int               `json:"process_num"`
	Pid              int               `json:"pid"`
	Nbthread         int               `json:"nbthread"`
	MaxConn          int               `json:"maxconn"`
	CurrentConn      int               `json:"curr_conn"`
	CurrentConnRate  int               `json:"conn_rate"`
	MaxConnRate      int               `json:"max_conn_rate"`
	TotalSessions    int64             `json:"cum_sess"`
	MaxSessRate      int               `json:"max_sess_rate"`
	Tasks            int               `json:"tasks"`
	RunQueue         int               `json:"run_queue"`
	MemMaxMB         int               `json:"mem_max_mb"`
	PoolAllocMB      int               `json:"pool_alloc_mb"`
	PoolUsedMB       int               `json:"pool_used_mb"`
	SslCacheUsage    int               `json:"ssl_cache_usage,omitempty"`
	SslCacheMax      int               `json:"ssl_cache_max,omitempty"`
	CompressBpsIn    int64             `json:"compress_bps_in,omitempty"`
	CompressBpsOut   int64             `json:"compress_bps_out,omitempty"`
	SslFrontendRate  int               `json:"ssl_fe_rate,omitempty"`
	SslBackendRate   int               `json:"ssl_be_rate,omitempty"`
	IdlePct          int               `json:"idle_pct"`
	Node             string            `json:"node,omitempty"`
	Description      string            `json:"description,omitempty"`
	Extra            map[string]string `json:"extra,omitempty"`
}

// GetRuntimeInfo fetches HAProxy runtime information.
func (c *StatsClient) GetRuntimeInfo() (*RuntimeInfo, error) {
	if c.socketPath != "" {
		return c.getRuntimeInfoFromSocket()
	}

	// HTTP endpoint doesn't expose runtime info the same way
	// Return basic info from version command
	return c.getRuntimeInfoFromCommand()
}

// getRuntimeInfoFromSocket fetches info via Unix socket.
func (c *StatsClient) getRuntimeInfoFromSocket() (*RuntimeInfo, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	_, err = conn.Write([]byte("show info\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	info := &RuntimeInfo{
		Extra: make(map[string]string),
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if err := parseInfoField(info, key, value); err != nil {
			// Store unrecognized fields in Extra
			info.Extra[key] = value
		}
	}

	return info, scanner.Err()
}

// parseInfoField parses a single info field into the RuntimeInfo struct.
func parseInfoField(info *RuntimeInfo, key, value string) error {
	var intVal int
	var int64Val int64

	switch key {
	case "Version":
		info.Version = value
	case "Release_date":
		info.ReleaseDate = value
	case "Uptime":
		info.Uptime = value
	case "Uptime_sec":
		fmt.Sscanf(value, "%d", &int64Val)
		info.UptimeSeconds = int64Val
	case "Process_num":
		fmt.Sscanf(value, "%d", &intVal)
		info.ProcessNum = intVal
	case "Pid":
		fmt.Sscanf(value, "%d", &intVal)
		info.Pid = intVal
	case "Nbthread":
		fmt.Sscanf(value, "%d", &intVal)
		info.Nbthread = intVal
	case "Maxconn":
		fmt.Sscanf(value, "%d", &intVal)
		info.MaxConn = intVal
	case "CurrConns":
		fmt.Sscanf(value, "%d", &intVal)
		info.CurrentConn = intVal
	case "ConnRate":
		fmt.Sscanf(value, "%d", &intVal)
		info.CurrentConnRate = intVal
	case "MaxConnRate":
		fmt.Sscanf(value, "%d", &intVal)
		info.MaxConnRate = intVal
	case "CumSess", "CumConns":
		fmt.Sscanf(value, "%d", &int64Val)
		info.TotalSessions = int64Val
	case "MaxSessRate":
		fmt.Sscanf(value, "%d", &intVal)
		info.MaxSessRate = intVal
	case "Tasks":
		fmt.Sscanf(value, "%d", &intVal)
		info.Tasks = intVal
	case "Run_queue":
		fmt.Sscanf(value, "%d", &intVal)
		info.RunQueue = intVal
	case "MemMax_MB":
		fmt.Sscanf(value, "%d", &intVal)
		info.MemMaxMB = intVal
	case "PoolAlloc_MB":
		fmt.Sscanf(value, "%d", &intVal)
		info.PoolAllocMB = intVal
	case "PoolUsed_MB":
		fmt.Sscanf(value, "%d", &intVal)
		info.PoolUsedMB = intVal
	case "SslCacheLookups":
		fmt.Sscanf(value, "%d", &intVal)
		info.SslCacheUsage = intVal
	case "SslCacheMisses":
		fmt.Sscanf(value, "%d", &intVal)
		info.SslCacheMax = intVal
	case "CompressBpsIn":
		fmt.Sscanf(value, "%d", &int64Val)
		info.CompressBpsIn = int64Val
	case "CompressBpsOut":
		fmt.Sscanf(value, "%d", &int64Val)
		info.CompressBpsOut = int64Val
	case "SslFrontendKeyRate":
		fmt.Sscanf(value, "%d", &intVal)
		info.SslFrontendRate = intVal
	case "SslBackendKeyRate":
		fmt.Sscanf(value, "%d", &intVal)
		info.SslBackendRate = intVal
	case "Idle_pct":
		fmt.Sscanf(value, "%d", &intVal)
		info.IdlePct = intVal
	case "node":
		info.Node = value
	case "description":
		info.Description = value
	default:
		return fmt.Errorf("unknown field: %s", key)
	}
	return nil
}

// getRuntimeInfoFromCommand gets basic info from haproxy -vv.
func (c *StatsClient) getRuntimeInfoFromCommand() (*RuntimeInfo, error) {
	cmd := exec.Command("haproxy", "-vv")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get haproxy version: %w", err)
	}

	info := &RuntimeInfo{
		Extra: make(map[string]string),
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		// First line is typically "HAProxy version X.X.X YYYY/MM/DD"
		parts := strings.Fields(lines[0])
		if len(parts) >= 3 {
			info.Version = parts[2]
		}
		if len(parts) >= 4 {
			info.ReleaseDate = parts[3]
		}
	}

	return info, nil
}

// StickTableInfo contains stick table information.
type StickTableInfo struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Size       int               `json:"size"`
	Used       int               `json:"used"`
	DataTypes  []string          `json:"data_types"`
	Entries    []StickTableEntry `json:"entries,omitempty"`
}

// StickTableEntry represents a single entry in a stick table.
type StickTableEntry struct {
	Key  string                 `json:"key"`
	Data map[string]any `json:"data"`
}

// GetStickTables fetches stick table information.
func (c *StatsClient) GetStickTables() ([]StickTableInfo, error) {
	if c.socketPath == "" {
		return nil, fmt.Errorf("stick tables require socket access")
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	_, err = conn.Write([]byte("show table\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var tables []StickTableInfo
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse line like: "# table: https_front, type: ip, size:102400, used:5"
		if strings.HasPrefix(line, "# table:") {
			table := parseTableLine(line)
			if table.Name != "" {
				tables = append(tables, table)
			}
		}
	}

	return tables, scanner.Err()
}

// parseTableLine parses a stick table summary line.
func parseTableLine(line string) StickTableInfo {
	info := StickTableInfo{}

	// Remove "# table: " prefix
	line = strings.TrimPrefix(line, "# table: ")

	parts := strings.Split(line, ", ")
	for _, part := range parts {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			// First part is the table name
			info.Name = strings.TrimSpace(part)
			continue
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "type":
			info.Type = value
		case "size":
			fmt.Sscanf(value, "%d", &info.Size)
		case "used":
			fmt.Sscanf(value, "%d", &info.Used)
		}
	}

	return info
}

// GetStickTableEntries fetches entries from a specific stick table.
func (c *StatsClient) GetStickTableEntries(tableName string, limit int) ([]StickTableEntry, error) {
	if c.socketPath == "" {
		return nil, fmt.Errorf("stick tables require socket access")
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Command: show table <name> data.
	cmd := fmt.Sprintf("show table %s\n", tableName)
	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var entries []StickTableEntry
	scanner := bufio.NewScanner(conn)

	// Increase buffer size for potentially large tables
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Skip header lines
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "table:") {
			continue
		}

		// Parse entry line
		// Format: "0x12345678: key=value use=1 exp=300 gpc0=100 http_req_rate(10000)=5"
		entry := parseStickTableEntry(line)
		if entry.Key != "" {
			entries = append(entries, entry)
			lineCount++
			if limit > 0 && lineCount >= limit {
				break
			}
		}
	}

	return entries, scanner.Err()
}

// parseStickTableEntry parses a stick table entry line.
func parseStickTableEntry(line string) StickTableEntry {
	entry := StickTableEntry{
		Data: make(map[string]any),
	}

	// Split by spaces, but be careful of values with parentheses
	parts := strings.Fields(line)

	for _, part := range parts {
		// Skip memory address prefix
		if strings.HasPrefix(part, "0x") && strings.HasSuffix(part, ":") {
			continue
		}

		// Parse key=value pairs
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				key := kv[0]
				value := kv[1]

				// Handle special "key" field
				if key == "key" {
					entry.Key = value
					continue
				}

				// Handle rate fields like "http_req_rate(10000)=5"
				// Extract base name before parenthesis
				baseName := key
				if idx := strings.Index(key, "("); idx > 0 {
					baseName = key[:idx]
				}

				// Parse numeric value
				var numVal float64
				if _, err := fmt.Sscanf(value, "%f", &numVal); err == nil {
					entry.Data[baseName] = numVal
				} else {
					entry.Data[baseName] = value
				}
			}
		}
	}

	return entry
}

// ActiveSession represents an active connection through HAProxy.
type ActiveSession struct {
	ID           string `json:"id"`
	SourceIP     string `json:"source_ip"`
	SourcePort   int    `json:"source_port"`
	Frontend     string `json:"frontend"`
	Backend      string `json:"backend"`
	Server       string `json:"server"`
	Age          int    `json:"age_seconds"`       // How long the session has been active
	Idle         int    `json:"idle_seconds"`      // How long since last activity
	ReqCount     int    `json:"request_count"`     // Number of requests in this session
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
}

// GetActiveSessions fetches current active sessions from HAProxy.
// This shows real-time source IP to backend connections.
func (c *StatsClient) GetActiveSessions(limit int) ([]ActiveSession, error) {
	if c.socketPath == "" {
		return nil, fmt.Errorf("sessions require socket access")
	}

	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Command: show sess
	_, err = conn.Write([]byte("show sess\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var sessions []ActiveSession
	scanner := bufio.NewScanner(conn)

	// Increase buffer for large session lists
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse session line
		// Format: "0x12345: proto=tcpv4 src=1.2.3.4:54321 fe=https_front be=backend/server ..."
		session := parseSessionLine(line)
		if session.SourceIP != "" && session.Backend != "" {
			sessions = append(sessions, session)
			lineCount++
			if limit > 0 && lineCount >= limit {
				break
			}
		}
	}

	return sessions, scanner.Err()
}

// parseSessionLine parses a HAProxy session line.
func parseSessionLine(line string) ActiveSession {
	session := ActiveSession{}

	// Extract session ID (first field before colon)
	if idx := strings.Index(line, ":"); idx > 0 {
		session.ID = strings.TrimPrefix(line[:idx], "0x")
		line = line[idx+1:]
	}

	// Parse key=value pairs
	parts := strings.Fields(line)
	for _, part := range parts {
		if !strings.Contains(part, "=") {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, value := kv[0], kv[1]

		switch key {
		case "src":
			// Format: IP:port
			if idx := strings.LastIndex(value, ":"); idx > 0 {
				session.SourceIP = value[:idx]
				fmt.Sscanf(value[idx+1:], "%d", &session.SourcePort)
			}
		case "fe":
			session.Frontend = value
		case "be":
			// Format: backend/server
			parts := strings.SplitN(value, "/", 2)
			session.Backend = parts[0]
			if len(parts) > 1 {
				session.Server = parts[1]
			}
		case "age":
			// Age can be in format "1s", "10s", "1m30s", etc.
			session.Age = parseDuration(value)
		case "idle":
			session.Idle = parseDuration(value)
		case "req":
			fmt.Sscanf(value, "%d", &session.ReqCount)
		case "txn":
			// Transaction count (similar to req)
			fmt.Sscanf(value, "%d", &session.ReqCount)
		}
	}

	return session
}

// parseDuration parses HAProxy duration format (e.g., "1s", "10s", "1m30s").
func parseDuration(s string) int {
	var total int
	var num int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else if c == 's' {
			total += num
			num = 0
		} else if c == 'm' {
			total += num * 60
			num = 0
		} else if c == 'h' {
			total += num * 3600
			num = 0
		}
	}
	return total
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

// isValidExternalIP checks if an IP is a valid external (non-internal) IP.
func isValidExternalIP(ip string) bool {
	// Filter out internal/invalid sources
	if ip == "" || ip == "unix" || ip == "localhost" || ip == "127.0.0.1" {
		return false
	}
	// Filter out Unix socket paths
	if strings.HasPrefix(ip, "/") {
		return false
	}
	// Filter out internal HAProxy addresses
	if strings.HasPrefix(ip, "unix@") {
		return false
	}
	return true
}

// isValidBackend checks if a backend name is valid for display.
func isValidBackend(backend string) bool {
	if backend == "" || backend == "<NOSRV>" || backend == "stats" || backend == "no_backend" {
		return false
	}
	// Filter out system/internal backends
	if backend == "<none>" || backend == "NOSRV" {
		return false
	}
	return true
}

// GetConnectionFlows aggregates active sessions into source->backend flows.
func (c *StatsClient) GetConnectionFlows(limit int) ([]ConnectionFlow, error) {
	sessions, err := c.GetActiveSessions(limit * 10) // Get more sessions for aggregation
	if err != nil {
		return nil, err
	}

	// Aggregate by source IP + backend
	flowMap := make(map[string]*ConnectionFlow)
	for _, sess := range sessions {
		// Filter invalid sources and backends
		if !isValidExternalIP(sess.SourceIP) || !isValidBackend(sess.Backend) {
			continue
		}

		key := sess.SourceIP + "->" + sess.Backend
		flow, exists := flowMap[key]
		if !exists {
			flow = &ConnectionFlow{
				SourceIP: sess.SourceIP,
				Backend:  sess.Backend,
			}
			flowMap[key] = flow
		}
		flow.ActiveCount++
		flow.TotalRequests += sess.ReqCount
		flow.BytesIn += sess.BytesIn
		flow.BytesOut += sess.BytesOut
	}

	// Convert to slice
	flows := make([]ConnectionFlow, 0, len(flowMap))
	for _, flow := range flowMap {
		flows = append(flows, *flow)
	}

	// Sort by active count
	for i := 0; i < len(flows)-1; i++ {
		for j := i + 1; j < len(flows); j++ {
			if flows[j].ActiveCount > flows[i].ActiveCount {
				flows[i], flows[j] = flows[j], flows[i]
			}
		}
	}

	// Limit results
	if limit > 0 && len(flows) > limit {
		flows = flows[:limit]
	}

	return flows, nil
}

// ValidateConfig validates the HAProxy configuration.
func ValidateConfig(configPath string) (bool, string, error) {
	cmd := exec.Command("haproxy", "-c", "-f", configPath)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Config is invalid
		return false, string(output), nil
	}

	return true, string(output), nil
}

// ReloadHAProxy reloads the HAProxy service.
func ReloadHAProxy() error {
	cmd := exec.Command("systemctl", "reload", "haproxy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload failed: %s", string(output))
	}
	return nil
}
