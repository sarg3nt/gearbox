// Package metrics collects system metrics from the local machine.
package metrics

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemMetrics holds current system metrics.
type SystemMetrics struct {
	CollectedAt time.Time `json:"collected_at"`

	// CPU / Load
	LoadAverage1  float64 `json:"load_average_1"`
	LoadAverage5  float64 `json:"load_average_5"`
	LoadAverage15 float64 `json:"load_average_15"`
	CPUCount      int     `json:"cpu_count"`

	// Memory
	MemoryTotalBytes   int64   `json:"memory_total_bytes"`
	MemoryUsedBytes    int64   `json:"memory_used_bytes"`
	MemoryFreeBytes    int64   `json:"memory_free_bytes"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`

	// Disk
	DiskTotalBytes   int64   `json:"disk_total_bytes"`
	DiskUsedBytes    int64   `json:"disk_used_bytes"`
	DiskFreeBytes    int64   `json:"disk_free_bytes"`
	DiskUsagePercent float64 `json:"disk_usage_percent"`

	// Network
	NetworkRxBytes   int64   `json:"network_rx_bytes"`
	NetworkTxBytes   int64   `json:"network_tx_bytes"`
	NetworkRxMbps    float64 `json:"network_rx_mbps"`
	NetworkTxMbps    float64 `json:"network_tx_mbps"`
	NetworkRxPercent float64 `json:"network_rx_percent"`
	NetworkTxPercent float64 `json:"network_tx_percent"`
}

// ServiceStatus represents the status of a systemd service.
type ServiceStatus struct {
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	StartedAt   string `json:"started_at,omitempty"` // ISO 8601 format
	Uptime      string `json:"uptime,omitempty"`     // Human-readable duration
}

// Collector collects system metrics.
type Collector struct {
	mu sync.Mutex

	// For calculating network rates
	prevNetworkRx   int64
	prevNetworkTx   int64
	prevCollectedAt time.Time

	// Network capacity in Mbps (default 10 Gbps)
	networkCapacityMbps float64
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		networkCapacityMbps: 10000, // 10 Gbps default
	}
}

// Collect gathers current system metrics.
func (c *Collector) Collect() (*SystemMetrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := &SystemMetrics{
		CollectedAt: time.Now(),
	}

	// Collect all metrics, continue even if some fail
	c.collectLoadAverage(m)
	c.collectCPUCount(m)
	c.collectMemory(m)
	c.collectDisk(m)
	c.collectNetwork(m)

	return m, nil
}

// CollectServiceStatuses checks the status of specified services.
func (c *Collector) CollectServiceStatuses(services []string) []ServiceStatus {
	var statuses []ServiceStatus

	for _, svc := range services {
		status := ServiceStatus{Name: svc}

		// Get detailed service information using systemctl show
		// Include multiple timestamp properties as fallbacks
		out, err := exec.Command("systemctl", "show", svc,
			"--property=ActiveState,Description,ActiveEnterTimestamp,ExecMainStartTimestamp,StateChangeTimestamp").Output()
		if err != nil {
			status.Status = "unknown"
			status.Active = false
			statuses = append(statuses, status)
			continue
		}

		// Parse the output
		lines := strings.Split(string(out), "\n")
		var timestamps []string // Collect all timestamps to try
		for _, line := range lines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			switch key {
			case "ActiveState":
				status.Status = value
				status.Active = value == "active"
			case "Description":
				status.Description = value
			case "ActiveEnterTimestamp", "ExecMainStartTimestamp", "StateChangeTimestamp":
				// Collect timestamps to try - prefer ActiveEnterTimestamp
				if value != "" && value != "n/a" && value != "0" {
					timestamps = append(timestamps, value)
				}
			}
		}

		// Try to parse a timestamp (first one that works)
		for _, ts := range timestamps {
			status.StartedAt, status.Uptime = parseSystemdTimestamp(ts)
			if status.StartedAt != "" {
				break
			}
		}

		statuses = append(statuses, status)
	}

	return statuses
}

// parseSystemdTimestamp parses a systemd timestamp and returns ISO 8601 format and human-readable uptime.
func parseSystemdTimestamp(timestamp string) (string, string) {
	// systemd timestamp format examples:
	// "Tue 2024-01-17 10:30:00 UTC"
	// "Wed 2026-01-15 21:57:00 EST"
	// Try multiple formats since systemd can output different formats
	formats := []string{
		"Mon 2006-01-02 15:04:05 MST",
		"Mon 2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 -0700",
	}

	var startTime time.Time
	var err error
	for _, format := range formats {
		startTime, err = time.Parse(format, timestamp)
		if err == nil {
			break
		}
	}

	if err != nil {
		// If standard parsing fails, try to extract just the date/time portion
		// Handle format like "Wed 2026-01-15 21:57:00 EST" by parsing without day name
		parts := strings.Fields(timestamp)
		if len(parts) >= 3 {
			// Try parsing "2026-01-15 21:57:00 EST" (without day name)
			dateTimeStr := strings.Join(parts[1:], " ")
			for _, format := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05"} {
				startTime, err = time.Parse(format, dateTimeStr)
				if err == nil {
					break
				}
			}
		}
	}

	if err != nil {
		return "", ""
	}

	// Convert to ISO 8601
	iso8601 := startTime.Format(time.RFC3339)

	// Calculate uptime
	uptime := formatDuration(time.Since(startTime))

	return iso8601, uptime
}

// formatDuration formats a duration as a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		if secs > 0 {
			return fmt.Sprintf("%dm %ds", mins, secs)
		}
		return fmt.Sprintf("%dm", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		if mins > 0 {
			return fmt.Sprintf("%dh %dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if hours > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dd", days)
}

// ControlService starts, stops, or restarts a systemd service.
// Returns the command output and any error.
//
// The "--" separator before the unit name prevents an attacker-chosen unit
// name (e.g. "--all" or "-h") from being interpreted as a systemctl flag.
// This is defense-in-depth on top of validUnitName regex validation at the
// HTTP boundary (plugin.go). See 2026-05 audit P1-1.
func (c *Collector) ControlService(service, action string) (string, error) {
	cmd := exec.Command("systemctl", action, "--", service)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// ListAvailableServices returns a list of all available systemd services on the system.
// It filters out template services (those containing @) and returns only actual service units.
func (c *Collector) ListAvailableServices() ([]string, error) {
	// Use systemctl to list all services (both loaded and available)
	cmd := exec.Command("systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	var services []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			// Service name is the first field (e.g., "haproxy.service")
			serviceName := fields[0]
			// Remove .service suffix
			serviceName = strings.TrimSuffix(serviceName, ".service")
			// Skip template services (contain @)
			if strings.Contains(serviceName, "@") {
				continue
			}
			// Skip empty names
			if serviceName == "" {
				continue
			}
			services = append(services, serviceName)
		}
	}

	return services, scanner.Err()
}

func (c *Collector) collectLoadAverage(m *SystemMetrics) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}

	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		m.LoadAverage1, _ = strconv.ParseFloat(fields[0], 64)
		m.LoadAverage5, _ = strconv.ParseFloat(fields[1], 64)
		m.LoadAverage15, _ = strconv.ParseFloat(fields[2], 64)
	}
}

func (c *Collector) collectCPUCount(m *SystemMetrics) {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	m.CPUCount = count
}

func (c *Collector) collectMemory(m *SystemMetrics) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer file.Close()

	var memTotal, memFree, memAvailable, buffers, cached int64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, _ := strconv.ParseInt(fields[1], 10, 64)
		value *= 1024 // Convert from KB to bytes

		switch fields[0] {
		case "MemTotal:":
			memTotal = value
		case "MemFree:":
			memFree = value
		case "MemAvailable:":
			memAvailable = value
		case "Buffers:":
			buffers = value
		case "Cached:":
			cached = value
		}
	}

	m.MemoryTotalBytes = memTotal
	m.MemoryFreeBytes = memAvailable
	if m.MemoryFreeBytes == 0 {
		// Fallback if MemAvailable not present
		m.MemoryFreeBytes = memFree + buffers + cached
	}
	m.MemoryUsedBytes = m.MemoryTotalBytes - m.MemoryFreeBytes

	if m.MemoryTotalBytes > 0 {
		m.MemoryUsagePercent = float64(m.MemoryUsedBytes) / float64(m.MemoryTotalBytes) * 100
	}
}

func (c *Collector) collectDisk(m *SystemMetrics) {
	out, err := exec.Command("df", "-B1", "/").Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return
	}

	m.DiskTotalBytes, _ = strconv.ParseInt(fields[1], 10, 64)
	m.DiskUsedBytes, _ = strconv.ParseInt(fields[2], 10, 64)
	m.DiskFreeBytes, _ = strconv.ParseInt(fields[3], 10, 64)

	if m.DiskTotalBytes > 0 {
		m.DiskUsagePercent = float64(m.DiskUsedBytes) / float64(m.DiskTotalBytes) * 100
	}
}

func (c *Collector) collectNetwork(m *SystemMetrics) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer file.Close()

	var totalRx, totalTx int64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") || strings.Contains(line, "lo:") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}

		rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		txBytes, _ := strconv.ParseInt(fields[8], 10, 64)

		totalRx += rxBytes
		totalTx += txBytes
	}

	m.NetworkRxBytes = totalRx
	m.NetworkTxBytes = totalTx

	// Calculate rates
	if !c.prevCollectedAt.IsZero() {
		timeDelta := m.CollectedAt.Sub(c.prevCollectedAt).Seconds()
		if timeDelta > 0 {
			rxBytesPerSec := float64(totalRx-c.prevNetworkRx) / timeDelta
			txBytesPerSec := float64(totalTx-c.prevNetworkTx) / timeDelta

			// Convert to Mbps (bytes/sec * 8 / 1,000,000)
			m.NetworkRxMbps = (rxBytesPerSec * 8) / 1000000
			m.NetworkTxMbps = (txBytesPerSec * 8) / 1000000

			if c.networkCapacityMbps > 0 {
				m.NetworkRxPercent = (m.NetworkRxMbps / c.networkCapacityMbps) * 100
				m.NetworkTxPercent = (m.NetworkTxMbps / c.networkCapacityMbps) * 100
			}
		}
	}

	// Update previous values
	c.prevNetworkRx = totalRx
	c.prevNetworkTx = totalTx
	c.prevCollectedAt = m.CollectedAt
}

// FormatBytes formats bytes into a human-readable string.
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
