// Package logs provides log collection functionality.
package logs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// LogSourceType defines how to fetch logs.
type LogSourceType string

const (
	// SourceTypeUnit fetches logs from a systemd unit.
	SourceTypeUnit LogSourceType = "unit"
	// SourceTypeJournalKernel fetches kernel logs from journalctl.
	SourceTypeJournalKernel LogSourceType = "kernel"
	// SourceTypeJournalPriority fetches logs filtered by priority.
	SourceTypeJournalPriority LogSourceType = "priority"
	// SourceTypeJournalBoot fetches logs from current boot.
	SourceTypeJournalBoot LogSourceType = "boot"
	// SourceTypeJournalSSH fetches SSH-related logs.
	SourceTypeJournalSSH LogSourceType = "ssh"
	// SourceTypeFile reads from a log file directly.
	SourceTypeFile LogSourceType = "file"
)

// LogSource represents a configured log source.
type LogSource struct {
	Name     string        `json:"name"`
	Unit     string        `json:"unit,omitempty"`      // Systemd unit name (e.g., "haproxy")
	Type     LogSourceType `json:"type,omitempty"`      // Source type for special handling
	FilePath string        `json:"file_path,omitempty"` // File path for file-based sources
	Priority string        `json:"priority,omitempty"`  // Priority filter (e.g., "warning..emerg")
	Filter   string        `json:"filter,omitempty"`    // Regex filter pattern for lines
}

// DefaultSources returns the default log sources for HAProxy servers.
// This list should include all services from the curated services list in
// gearbox/internal/handler/integrations.go getCuratedServicesList()
// so that any monitored service can have its logs viewed.
func DefaultSources() []LogSource {
	return []LogSource{
		// Core services
		{Name: "haproxy", Unit: "haproxy"},
		{Name: "gearbox-agent", Unit: "gearbox-agent"},

		// Security services
		{Name: "fail2ban", Unit: "fail2ban"},
		{Name: "fail2ban-log", Type: SourceTypeFile, FilePath: "/var/log/fail2ban.log"},
		{Name: "ufw", Unit: "ufw"},

		// Firewall
		{Name: "nftables", Unit: "nftables"},
		{Name: "firewall", Type: SourceTypeJournalKernel, Filter: "(?i)(nft|DROP|REJECT|firewall|IN=.*OUT=)"},

		// Authentication and SSH
		{Name: "auth", Type: SourceTypeJournalSSH},
		{Name: "ssh", Unit: "ssh"},

		// Certificate management
		{Name: "acme.sh", Type: SourceTypeFile, FilePath: "/root/.acme.sh/acme.sh.log"},

		// System services (commonly monitored)
		{Name: "cron", Unit: "cron"},
		{Name: "rsyslog", Unit: "rsyslog"},
		{Name: "systemd-resolved", Unit: "systemd-resolved"},

		// System logs
		{Name: "system", Type: SourceTypeJournalPriority, Priority: "warning..emerg"},
		{Name: "kernel", Type: SourceTypeJournalKernel},
		{Name: "boot", Type: SourceTypeJournalBoot, Priority: "notice..emerg"},
	}
}

// Collector fetches logs from configured sources.
type Collector struct {
	sources map[string]LogSource
}

// NewCollector creates a new log collector with the given sources.
func NewCollector(sources []LogSource) *Collector {
	sourceMap := make(map[string]LogSource)
	for _, s := range sources {
		sourceMap[s.Name] = s
	}
	return &Collector{sources: sourceMap}
}

// GetSources returns the list of available log sources.
func (c *Collector) GetSources() []LogSource {
	sources := make([]LogSource, 0, len(c.sources))
	for _, s := range c.sources {
		sources = append(sources, s)
	}
	return sources
}

// HasSource checks if a log source exists.
func (c *Collector) HasSource(name string) bool {
	_, ok := c.sources[name]
	return ok
}

// FetchLogs fetches log content from the named source.
func (c *Collector) FetchLogs(name string, lines int) (string, error) {
	source, ok := c.sources[name]
	if !ok {
		return "", fmt.Errorf("unknown log source: %s", name)
	}

	// Validate and cap lines
	if lines <= 0 {
		lines = 500
	}
	if lines > 10000 {
		lines = 10000 // Cap at 10k lines
	}

	// Handle based on source type
	if source.Unit != "" {
		return c.fetchFromUnit(source.Unit, lines)
	}

	switch source.Type {
	case SourceTypeFile:
		return c.fetchFromFile(source.FilePath, lines)
	case SourceTypeJournalKernel:
		return c.fetchKernelLogs(lines, source.Filter)
	case SourceTypeJournalPriority:
		return c.fetchPriorityLogs(lines, source.Priority)
	case SourceTypeJournalBoot:
		return c.fetchBootLogs(lines, source.Priority)
	case SourceTypeJournalSSH:
		return c.fetchSSHLogs(lines)
	default:
		return "", fmt.Errorf("log source %s has no valid configuration", name)
	}
}

// fetchFromUnit fetches logs from a systemd unit.
func (c *Collector) fetchFromUnit(unit string, lines int) (string, error) {
	cmd := exec.Command("journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch unit logs: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// fetchFromFile reads the last N lines from a file.
func (c *Collector) fetchFromFile(filePath string, lines int) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("%s not found", filePath), nil
		}
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read all lines and take the last N
	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Take last N lines
	start := 0
	if len(allLines) > lines {
		start = len(allLines) - lines
	}
	return strings.Join(allLines[start:], "\n"), nil
}

// fetchKernelLogs fetches kernel logs, optionally filtered.
func (c *Collector) fetchKernelLogs(lines int, filter string) (string, error) {
	cmd := exec.Command("journalctl", "-k", "-n", strconv.Itoa(lines), "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch kernel logs: %w", err)
	}

	content := string(output)

	// Apply filter if specified
	if filter != "" {
		content = filterLines(content, filter)
		if content == "" {
			return "No matching log entries found", nil
		}
	}

	return strings.TrimSpace(content), nil
}

// fetchPriorityLogs fetches logs filtered by priority.
func (c *Collector) fetchPriorityLogs(lines int, priority string) (string, error) {
	args := []string{"-n", strconv.Itoa(lines), "--no-pager"}
	if priority != "" {
		args = append(args, "-p", priority)
	}
	cmd := exec.Command("journalctl", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch priority logs: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// fetchBootLogs fetches logs from current boot with optional priority filter.
func (c *Collector) fetchBootLogs(lines int, priority string) (string, error) {
	args := []string{"-b", "-n", strconv.Itoa(lines), "--no-pager"}
	if priority != "" {
		args = append(args, "-p", priority)
	}
	cmd := exec.Command("journalctl", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch boot logs: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// fetchSSHLogs fetches SSH authentication logs.
func (c *Collector) fetchSSHLogs(lines int) (string, error) {
	// Try _COMM=sshd first (more reliable)
	cmd := exec.Command("journalctl", "-n", strconv.Itoa(lines), "--no-pager", "_COMM=sshd", "-o", "short-iso")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output)), nil
	}

	// Fall back to -t sshd
	cmd = exec.Command("journalctl", "-n", strconv.Itoa(lines), "--no-pager", "-t", "sshd")
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch SSH logs: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// filterLines filters log lines using a regex pattern.
func filterLines(content, pattern string) string {
	var filtered []string
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) ||
			matchesPattern(line, pattern) {
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
}

// matchesPattern checks if a line matches a regex-like pattern.
// For simplicity, we check for common patterns without full regex.
func matchesPattern(line, pattern string) bool {
	// Handle case-insensitive pattern: (?i)
	checkLine := line
	checkPattern := pattern
	if strings.HasPrefix(pattern, "(?i)") {
		checkLine = strings.ToLower(line)
		checkPattern = strings.ToLower(pattern[4:])
	}

	// Handle OR patterns: (a|b|c)
	if strings.HasPrefix(checkPattern, "(") && strings.Contains(checkPattern, "|") {
		inner := strings.TrimPrefix(checkPattern, "(")
		inner = strings.TrimSuffix(inner, ")")
		parts := strings.Split(inner, "|")
		for _, part := range parts {
			if strings.Contains(checkLine, part) {
				return true
			}
		}
		return false
	}

	return strings.Contains(checkLine, checkPattern)
}

// LogResponse represents the response for a log fetch request.
type LogResponse struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Content string `json:"content"`
}

// SourcesResponse lists available log sources.
type SourcesResponse struct {
	Sources []LogSource `json:"sources"`
}
